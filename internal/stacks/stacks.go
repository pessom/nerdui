// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package stacks finds compose stacks and computes their status (#24, ADR-0008). A stack is a
// compose project: a directory with a compose file, either inside --stacks-dir ("managed"),
// registered in place from elsewhere ("external", #25) or cloned from a Git repository ("git",
// #37). Its containers carry the com.docker.compose.project label.
package stacks

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pessom/nerdui/internal/gitrepo"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store/db"
)

// Modes of a stack.
const (
	ModeManaged  = "managed"
	ModeExternal = "external"
	// ModeGit is a stack cloned from a Git repository (#37, ADR-0039): its compose file comes
	// from the repository and is read-only; .env is local.
	ModeGit = "git"
	// ModeUnmanaged is a compose project seen only through container labels: nerdui does not
	// know its files.
	ModeUnmanaged = "unmanaged"
)

// Statuses of a stack.
const (
	StatusRunning = "running" // every service runs
	StatusPartial = "partial" // some services run
	StatusStopped = "stopped" // nothing runs
	StatusUnknown = "unknown" // no readable compose file
)

// ComposeFileNames are looked up in this order, as compose-go does (cli.DefaultFileNames).
var ComposeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// Registry stores external stacks; *db.Queries implements it.
type Registry interface {
	ListStacks(ctx context.Context) ([]db.Stack, error)
}

// GitRegistry lists Git stacks; *db.Queries implements it.
type GitRegistry interface {
	ListGitStacks(ctx context.Context) ([]db.GitStack, error)
}

// Definition is a stack nerdui knows the files of.
type Definition struct {
	Name string
	Dir  string
	// File is the compose file inside Dir ("" when none is found).
	File string
	Mode string
	// Git describes the repository of a Git stack.
	Git *GitInfo
}

// GitInfo is the source of a Git stack. Keep in sync with api/openapi.yaml.
type GitInfo struct {
	URL string `json:"url"`
	// Ref as entered (empty: the default branch) and FullRef as resolved.
	Ref          string         `json:"ref"`
	FullRef      string         `json:"fullRef"`
	ComposePath  string         `json:"composePath"`
	CredentialID *int64         `json:"credentialId,omitempty"`
	Commit       gitrepo.Commit `json:"commit"`
	PulledAt     time.Time      `json:"pulledAt"`
	// LastError of the newest failed pull or check; the checked-out commit stays.
	LastError string `json:"lastError,omitempty"`
	// AutoUpdate says what happens on a new commit found by polling or the webhook (#38, #39):
	// off, notify or deploy. PollInterval is in seconds, 0 without polling.
	AutoUpdate   string `json:"autoUpdate"`
	PollInterval int64  `json:"pollInterval"`
	// Webhook is true when the stack has a webhook token.
	Webhook bool `json:"webhook"`
	// CheckedAt is the last check of the remote; RemoteCommit the newest remote commit when it
	// is not checked out.
	CheckedAt    *time.Time `json:"checkedAt,omitempty"`
	RemoteCommit string     `json:"remoteCommit,omitempty"`
}

// GitInfoFrom converts a stored Git stack.
func GitInfoFrom(r db.GitStack) *GitInfo {
	g := &GitInfo{
		URL: r.Url, Ref: r.Ref, FullRef: r.FullRef, ComposePath: r.ComposePath,
		Commit: gitrepo.Commit{
			Hash: r.CommitHash, Message: r.CommitMessage, Author: r.CommitAuthor,
			Date: time.UnixMilli(r.CommitAt).UTC(),
		},
		PulledAt: time.UnixMilli(r.PulledAt).UTC(), LastError: r.LastError,
		AutoUpdate: r.AutoUpdate, PollInterval: r.PollInterval, Webhook: r.WebhookHash != "",
		RemoteCommit: r.RemoteHash,
	}
	if r.CheckedAt > 0 {
		t := time.UnixMilli(r.CheckedAt).UTC()
		g.CheckedAt = &t
	}
	if r.CredentialID.Valid {
		id := r.CredentialID.Int64
		g.CredentialID = &id
	}
	return g
}

// GitDefinition is the definition of a stored Git stack cloned into gitDir/<name>.
func GitDefinition(gitDir string, r db.GitStack) Definition {
	clone := filepath.Join(gitDir, r.Name)
	dir := filepath.Join(clone, filepath.Dir(filepath.FromSlash(r.ComposePath)))
	file := filepath.Base(r.ComposePath)
	if fi, err := os.Lstat(filepath.Join(dir, file)); err != nil || !fi.Mode().IsRegular() {
		file = "" // removed from the repository
	}
	return Definition{Name: r.Name, Dir: dir, File: file, Mode: ModeGit, Git: GitInfoFrom(r)}
}

// Service finds the stack definitions of a host.
type Service struct {
	// Dir is --stacks-dir; each subdirectory with a compose file is a managed stack.
	Dir      string
	Registry Registry
	// Writer registers external stacks (#25); nil makes adoption unavailable.
	Writer Writer
	// ScanDirs are searched for compose projects to adopt (--stacks-scan-dirs, #25).
	ScanDirs []string
	// Git lists Git stacks cloned into GitDir (#37); nil disables them.
	Git    GitRegistry
	GitDir string
}

// ComposeFile returns the compose file of dir, or "" when it has none.
func ComposeFile(dir string) string {
	for _, n := range ComposeFileNames {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && fi.Mode().IsRegular() {
			return n
		}
	}
	return ""
}

// Definitions returns the managed and external stacks sorted by name. A missing stacks
// directory has no stacks.
func (s *Service) Definitions(ctx context.Context) ([]Definition, error) {
	var out []Definition
	seen := map[string]bool{}
	if s.Dir != "" {
		entries, err := os.ReadDir(s.Dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read stacks directory: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(s.Dir, e.Name())
			file := ComposeFile(dir)
			if file == "" {
				continue
			}
			out = append(out, Definition{Name: e.Name(), Dir: dir, File: file, Mode: ModeManaged})
			seen[e.Name()] = true
		}
	}
	if s.Registry != nil {
		rows, err := s.Registry.ListStacks(ctx)
		if err != nil {
			return nil, fmt.Errorf("list registered stacks: %w", err)
		}
		for _, r := range rows {
			if r.Mode != ModeExternal || seen[r.Name] {
				continue
			}
			out = append(out, Definition{Name: r.Name, Dir: r.Path, File: ComposeFile(r.Path), Mode: ModeExternal})
			seen[r.Name] = true
		}
	}
	if s.Git != nil && s.GitDir != "" {
		rows, err := s.Git.ListGitStacks(ctx)
		if err != nil {
			return nil, fmt.Errorf("list Git stacks: %w", err)
		}
		for _, r := range rows {
			if seen[r.Name] {
				continue
			}
			out = append(out, GitDefinition(s.GitDir, r))
			seen[r.Name] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Stack is a stack with its status in a namespace. Keep in sync with api/openapi.yaml.
type Stack struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	// Namespace is the containerd namespace of the stack.
	Namespace string `json:"namespace"`
	// Path is the stack directory and ComposeFile the file name in it (empty when unmanaged).
	Path        string `json:"path,omitempty"`
	ComposeFile string `json:"composeFile,omitempty"`
	// Git is set for a stack cloned from a Git repository.
	Git    *GitInfo `json:"git,omitempty"`
	Status string   `json:"status"`
	// Error explains why the compose file could not be read.
	Error    string         `json:"error,omitempty"`
	Services []StackService `json:"services"`
	// Containers and Running count all containers of the project, including ones of services
	// no longer in the compose file.
	Containers int `json:"containers"`
	Running    int `json:"running"`
}

// StackService is a service of a stack. Defined is false for a service that has containers
// but is not in the compose file (removed or behind a profile).
type StackService struct {
	Name       string           `json:"name"`
	Defined    bool             `json:"defined"`
	Running    int              `json:"running"`
	Containers []StackContainer `json:"containers"`
}

// StackContainer is a container of a service.
type StackContainer struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	State string `json:"state"`
}

// Summarize computes a stack's status from its compose services (nil with loadErr when the file
// could not be read) and the containers of its project.
func Summarize(def Definition, services []string, loadErr error, containers []runtime.Container) Stack {
	st := Stack{Name: def.Name, Mode: def.Mode, Path: def.Dir, ComposeFile: def.File, Git: def.Git, Services: []StackService{}}
	byName := map[string]*StackService{}
	for _, name := range services {
		st.Services = append(st.Services, StackService{Name: name, Defined: true, Containers: []StackContainer{}})
	}
	for i := range st.Services {
		byName[st.Services[i].Name] = &st.Services[i]
	}
	var extra []*StackService // services with containers but not in the compose file
	for _, c := range containers {
		if c.Compose == nil || c.Compose.Project != def.Name {
			continue
		}
		st.Containers++
		running := c.State == runtime.StateRunning
		if running {
			st.Running++
		}
		svc, ok := byName[c.Compose.Service]
		if !ok {
			svc = &StackService{Name: c.Compose.Service, Containers: []StackContainer{}}
			extra = append(extra, svc)
			byName[c.Compose.Service] = svc
		}
		svc.Containers = append(svc.Containers, StackContainer{ID: c.ID, Name: c.Name, State: c.State})
		if running {
			svc.Running++
		}
	}
	for _, e := range extra {
		st.Services = append(st.Services, *e)
	}

	switch {
	case def.File == "" || loadErr != nil:
		st.Status = StatusUnknown
		if loadErr != nil {
			st.Error = loadErr.Error()
		} else if def.Mode != ModeUnmanaged {
			st.Error = "no compose file in " + def.Dir
		}
	default:
		up := 0
		for _, s := range st.Services {
			if s.Defined && s.Running > 0 {
				up++
			}
		}
		switch {
		case len(services) > 0 && up == len(services):
			st.Status = StatusRunning
		case up > 0:
			st.Status = StatusPartial
		default:
			st.Status = StatusStopped
		}
	}
	return st
}

// Unmanaged returns the compose projects of containers that match no definition, sorted.
func Unmanaged(defs []Definition, containers []runtime.Container) []string {
	known := map[string]bool{}
	for _, d := range defs {
		known[d.Name] = true
	}
	set := map[string]bool{}
	for _, c := range containers {
		if c.Compose != nil && c.Compose.Project != "" && !known[c.Compose.Project] {
			set[c.Compose.Project] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
