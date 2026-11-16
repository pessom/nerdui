// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package stacks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"

	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/store/db"
)

// Adoption modes (#25, ADR-0008).
const (
	// AdoptInPlace registers the directory where it is; files stay there.
	AdoptInPlace = "in-place"
	// AdoptCopy copies the compose and .env files into the stacks directory.
	AdoptCopy = "copy"
)

// Writer registers and forgets external stacks; *db.Queries implements it.
type Writer interface {
	RegisterStack(ctx context.Context, arg db.RegisterStackParams) error
	DeleteStack(ctx context.Context, name string) (int64, error)
}

// Describer loads a compose project from disk; runtime.Compose implements it.
type Describer interface {
	Describe(ctx context.Context, dir, file string) (runtime.ComposeProject, error)
}

// Candidate is a compose project found in a scanned directory and not yet a stack.
type Candidate struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	ComposeFile string   `json:"composeFile"`
	Services    []string `json:"services"`
	// LocalPaths are files of the directory the project uses: it can only be adopted in place.
	LocalPaths []string `json:"localPaths,omitempty"`
	// Error explains why the compose file could not be read.
	Error string `json:"error,omitempty"`
	// Taken is true when a stack of that name exists already.
	Taken bool `json:"taken"`
}

// scanDepth is how deep below a scan directory compose projects are looked for.
const scanDepth = 2

// Discover scans the scan directories for compose projects that are not stacks yet (#25).
func (s *Service) Discover(ctx context.Context, d Describer) ([]Candidate, error) {
	defs, err := s.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	knownDir, knownName := map[string]bool{}, map[string]bool{}
	for _, def := range defs {
		knownDir[filepath.Clean(def.Dir)] = true
		knownName[def.Name] = true
	}
	var dirs []string
	for _, root := range s.ScanDirs {
		dirs = append(dirs, scan(filepath.Clean(root), scanDepth)...)
	}
	out := []Candidate{}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] || knownDir[dir] || s.inStacksDir(dir) {
			continue
		}
		seen[dir] = true
		file := ComposeFile(dir)
		c := Candidate{Path: dir, ComposeFile: file, Services: []string{}}
		p, err := d.Describe(ctx, dir, filepath.Join(dir, file))
		if err != nil {
			c.Name, c.Error = filepath.Base(dir), err.Error()
		} else {
			c.Name, c.Services, c.LocalPaths = p.Name, p.Services, p.LocalPaths
		}
		c.Taken = knownName[c.Name]
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// scan returns root and its subdirectories down to depth that contain a compose file,
// skipping hidden directories. Unreadable directories are skipped.
func scan(root string, depth int) []string {
	var out []string
	if ComposeFile(root) != "" {
		out = append(out, root)
	}
	if depth == 0 {
		return out
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, scan(filepath.Join(root, e.Name()), depth-1)...)
		}
	}
	return out
}

func (s *Service) inStacksDir(dir string) bool {
	if s.Dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(s.Dir), dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// AdoptRequest adopts an existing compose project as a stack.
type AdoptRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// Adopt makes an existing compose project a stack (#25): in place (the directory is registered)
// or by copying its compose and .env files into the stacks directory. Copying is refused when
// the project uses other files of its directory, which would not follow it.
func (s *Service) Adopt(ctx context.Context, d Describer, req AdoptRequest) (Definition, error) {
	if s.Writer == nil {
		return Definition{}, fmt.Errorf("%w: adopting stacks is not available", errdefs.ErrNotImplemented)
	}
	if err := identifiers.ValidateDockerCompat(req.Name); err != nil {
		return Definition{}, fmt.Errorf("%w: stack name: %w", errdefs.ErrInvalidArgument, err)
	}
	if !filepath.IsAbs(req.Path) {
		return Definition{}, fmt.Errorf("%w: path must be absolute", errdefs.ErrInvalidArgument)
	}
	dir := filepath.Clean(req.Path)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Definition{}, fmt.Errorf("%w: %s is not a directory", errdefs.ErrInvalidArgument, dir)
	}
	file := ComposeFile(dir)
	if file == "" {
		return Definition{}, fmt.Errorf("%w: no compose file (%s) in %s", errdefs.ErrInvalidArgument,
			strings.Join(ComposeFileNames, ", "), dir)
	}
	if s.inStacksDir(dir) {
		return Definition{}, fmt.Errorf("%w: %s is inside the stacks directory: it is a stack already", errdefs.ErrInvalidArgument, dir)
	}
	defs, err := s.Definitions(ctx)
	if err != nil {
		return Definition{}, err
	}
	for _, def := range defs {
		if def.Name == req.Name {
			return Definition{}, fmt.Errorf("%w: stack %q exists already", errdefs.ErrAlreadyExists, req.Name)
		}
		if filepath.Clean(def.Dir) == dir {
			return Definition{}, fmt.Errorf("%w: %s is stack %q already", errdefs.ErrAlreadyExists, dir, def.Name)
		}
	}
	p, err := d.Describe(ctx, dir, filepath.Join(dir, file))
	if err != nil {
		return Definition{}, err
	}

	switch req.Mode {
	case AdoptInPlace:
		if err := s.Writer.RegisterStack(ctx, db.RegisterStackParams{
			Name: req.Name, Path: dir, Mode: ModeExternal, Now: time.Now().UnixMilli(),
		}); err != nil {
			return Definition{}, fmt.Errorf("register stack: %w", err)
		}
		return Definition{Name: req.Name, Dir: dir, File: file, Mode: ModeExternal}, nil
	case AdoptCopy:
		if len(p.LocalPaths) > 0 {
			return Definition{}, fmt.Errorf("%w: the project uses files of its directory (%s); adopt it in place",
				errdefs.ErrFailedPrecondition, strings.Join(firstN(p.LocalPaths, 3), ", "))
		}
		target, err := s.copyProject(dir, req.Name, p.Files)
		if err != nil {
			return Definition{}, err
		}
		return Definition{Name: req.Name, Dir: target, File: ComposeFile(target), Mode: ModeManaged}, nil
	default:
		return Definition{}, fmt.Errorf("%w: mode must be %s or %s", errdefs.ErrInvalidArgument, AdoptInPlace, AdoptCopy)
	}
}

// copyProject copies the compose files and .env of dir into <stacks-dir>/<name> through a
// temporary directory renamed at the end.
func (s *Service) copyProject(dir, name string, files []string) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("%w: no stacks directory", errdefs.ErrFailedPrecondition)
	}
	target := filepath.Join(s.Dir, name)
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("%w: %s exists already", errdefs.ErrAlreadyExists, target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", fmt.Errorf("create stacks directory: %w", err)
	}
	var rels []string
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", fmt.Errorf("%w: compose file %s is outside %s; adopt the project in place", errdefs.ErrFailedPrecondition, f, dir)
		}
		rels = append(rels, rel)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		rels = append(rels, ".env")
	}
	tmp, err := os.MkdirTemp(s.Dir, ".adopt-"+name+"-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // a no-op once renamed
	for _, rel := range rels {
		if err := copyFile(filepath.Join(dir, rel), filepath.Join(tmp, rel)); err != nil {
			return "", fmt.Errorf("copy %s: %w", rel, err)
		}
	}
	if err := os.Chmod(tmp, 0o750); err != nil { //nolint:gosec // G302: a directory, like the other stack directories
		return "", err
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", fmt.Errorf("move into %s: %w", target, err)
	}
	return target, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // G304: files of a project the administrator adopts
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: inside a fresh temp dir
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
}

// Forget removes the registration of an external stack; its files stay where they are.
func (s *Service) Forget(ctx context.Context, name string) error {
	if s.Writer == nil {
		return fmt.Errorf("%w: stacks registry is not available", errdefs.ErrNotImplemented)
	}
	defs, err := s.Definitions(ctx)
	if err != nil {
		return err
	}
	for _, def := range defs {
		if def.Name != name {
			continue
		}
		if def.Mode != ModeExternal {
			return fmt.Errorf("%w: %q is in the stacks directory, not a registered directory", errdefs.ErrFailedPrecondition, name)
		}
		if _, err := s.Writer.DeleteStack(ctx, name); err != nil {
			return fmt.Errorf("forget stack: %w", err)
		}
		return nil
	}
	return fmt.Errorf("%w: stack %q not found", errdefs.ErrNotFound, name)
}
