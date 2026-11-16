// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"
)

// Compose reads compose projects like `nerdctl compose` (ADR-0008): compose-go, at the version
// the linked nerdctl uses, with the project options of nerdctl's composer.New — includes,
// extends, profiles and .env interpolation behave the same.
type Compose interface {
	// Services returns the services of a project that run by default (no profile), loading
	// file from dir under the project name.
	Services(ctx context.Context, namespace, project, dir, file string) ([]string, error)
	// Describe loads a project found on disk under its own name (`name:` or the directory
	// name) for discovery and adoption (#25).
	Describe(ctx context.Context, dir, file string) (ComposeProject, error)
}

// ComposeProject describes a compose project on disk.
type ComposeProject struct {
	Name     string
	Services []string
	// Files are the compose files loaded (main, override, includes).
	Files []string
	// LocalPaths are paths inside the project directory the project uses besides its compose
	// and .env files: bind mounts, build contexts, env files, config and secret files.
	LocalPaths []string
}

// Compose returns compose operations of the local containerd.
func (c *Client) Compose() Compose { return clientCompose{} }

type clientCompose struct{}

// The package composer is not imported: its test dependencies reference a nerdctl-internal
// module (mod/tigron) that `go mod tidy` cannot resolve outside nerdctl's repository.
func (clientCompose) Services(ctx context.Context, _, project, dir, file string) ([]string, error) {
	if err := identifiers.ValidateDockerCompat(project); err != nil {
		return nil, fmt.Errorf("%w: project name: %w", errdefs.ErrInvalidArgument, err)
	}
	p, err := loadProject(ctx, project, dir, file)
	if err != nil {
		return nil, err
	}
	return p.ServiceNames(), nil
}

func (clientCompose) Describe(ctx context.Context, dir, file string) (ComposeProject, error) {
	p, err := loadProject(ctx, "", dir, file)
	if err != nil {
		return ComposeProject{}, err
	}
	out := ComposeProject{Name: p.Name, Services: p.ServiceNames(), Files: p.ComposeFiles}
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		path = filepath.Clean(path)
		if rel, err := filepath.Rel(dir, path); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return // outside the project directory
		}
		if slices.Contains(p.ComposeFiles, path) || filepath.Base(path) == ".env" && filepath.Dir(path) == dir {
			return
		}
		if !seen[path] {
			seen[path] = true
			out.LocalPaths = append(out.LocalPaths, path)
		}
	}
	for _, svc := range p.Services {
		for _, v := range svc.Volumes {
			if v.Type == "bind" {
				add(v.Source)
			}
		}
		if svc.Build != nil {
			add(svc.Build.Context)
		}
		for _, e := range svc.EnvFiles {
			add(e.Path)
		}
	}
	for _, c := range p.Configs {
		add(c.File)
	}
	for _, sc := range p.Secrets {
		add(sc.File)
	}
	sort.Strings(out.LocalPaths)
	return out, nil
}

// loadProject loads a compose project like `nerdctl compose` run in dir without -f: the default
// files (compose.yaml and compose.override.yaml, …) are found there; file only has to exist.
// An empty name derives it from the file or the directory.
func loadProject(ctx context.Context, project, dir, file string) (*types.Project, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, fmt.Errorf("%w: compose file: %w", errdefs.ErrInvalidArgument, err)
	}
	// The options of nerdctl's composer.New, in its order.
	opts, err := composecli.NewProjectOptions(nil,
		composecli.WithOsEnv,
		composecli.WithWorkingDirectory(dir),
		composecli.WithConfigFileEnv,
		composecli.WithDefaultConfigPath,
		composecli.WithEnvFiles(),
		composecli.WithDotEnv,
		composecli.WithName(project),
		composecli.WithProfiles(nil),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	p, err := opts.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	return p, nil
}
