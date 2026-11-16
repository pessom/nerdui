// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package stacks finds compose stacks and computes their status (#24, ADR-0008). A stack is a
// compose project: a directory with a compose file, either inside --stacks-dir ("managed") or
// registered in place from elsewhere ("external", #25). Its containers carry the
// com.docker.compose.project label.
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

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store/db"
)

// Modes of a stack.
const (
	ModeManaged  = "managed"
	ModeExternal = "external"
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

// ComposeFileNames are looked up in this order, as compose-go does.
var ComposeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Registry stores external stacks; *db.Queries implements it.
type Registry interface {
	ListStacks(ctx context.Context) ([]db.Stack, error)
}

// Definition is a stack nerdui knows the files of.
type Definition struct {
	Name string
	Dir  string
	// File is the compose file inside Dir ("" when none is found).
	File string
	Mode string
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Stack is a stack with its status in a namespace. Keep in sync with api/openapi.yaml.
type Stack struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	// Path is the stack directory and ComposeFile the file name in it (empty when unmanaged).
	Path        string `json:"path,omitempty"`
	ComposeFile string `json:"composeFile,omitempty"`
	Status      string `json:"status"`
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
	st := Stack{Name: def.Name, Mode: def.Mode, Path: def.Dir, ComposeFile: def.File, Services: []StackService{}}
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
