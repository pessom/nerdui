// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package stacks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"
)

// Editable files of a stack (#26).
const (
	FileCompose = "compose"
	FileEnv     = "env"
)

// MaxFileSize bounds an edited file.
const MaxFileSize = 1 << 20

// StackFile is the content of an editable file. Version identifies the content for optimistic
// concurrency; it is empty when the file does not exist. Keep in sync with api/openapi.yaml.
type StackFile struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Version string `json:"version"`
	Exists  bool   `json:"exists"`
}

// Version returns the version of content.
func Version(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// within returns base/name, cleaned, when it is a direct child of base; name must be a single
// path element. It guards every path built for a stack (defense in depth: names are validated
// and definitions come from the stacks directory and the database).
func within(base, name string) (string, error) {
	base = filepath.Clean(base)
	p := filepath.Clean(filepath.Join(base, name))
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(p, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q is not a file of %s", errdefs.ErrInvalidArgument, name, base)
	}
	return p, nil
}

// FilePath returns the name and path of an editable file of a stack.
func FilePath(def Definition, kind string) (name, path string, err error) {
	switch kind {
	case FileCompose:
		if def.File == "" {
			return "", "", fmt.Errorf("%w: no compose file in %s", errdefs.ErrFailedPrecondition, def.Dir)
		}
		name = def.File
	case FileEnv:
		name = ".env"
	default:
		return "", "", fmt.Errorf("%w: file must be %s or %s", errdefs.ErrNotFound, FileCompose, FileEnv)
	}
	if path, err = within(def.Dir, name); err != nil {
		return "", "", err
	}
	return name, path, nil
}

// ReadFile reads an editable file; a missing .env is empty.
func ReadFile(def Definition, kind string) (StackFile, error) {
	name, path, err := FilePath(def, kind)
	if err != nil {
		return StackFile{}, err
	}
	out := StackFile{Kind: kind, Name: name}
	b, err := os.ReadFile(path) //nolint:gosec // G304: a stack's own file
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return StackFile{}, fmt.Errorf("read %s: %w", name, err)
	}
	out.Content, out.Version, out.Exists = string(b), Version(b), true
	return out, nil
}

// WriteFile replaces an editable file if its version is still version (empty: the file must
// not exist), keeping the previous content as <name>.bak. The write is atomic: a temporary
// file in the same directory is renamed over the file, with the file's permissions.
func WriteFile(def Definition, kind string, content []byte, version string) (StackFile, error) {
	if len(content) > MaxFileSize {
		return StackFile{}, fmt.Errorf("%w: the file is larger than %d KiB", errdefs.ErrInvalidArgument, MaxFileSize>>10)
	}
	if def.Mode == ModeGit && kind == FileCompose {
		return StackFile{}, fmt.Errorf("%w: the compose file of a Git stack comes from its repository; change it there and pull",
			errdefs.ErrFailedPrecondition)
	}
	cur, err := ReadFile(def, kind)
	if err != nil {
		return StackFile{}, err
	}
	if cur.Version != version {
		return StackFile{}, fmt.Errorf("%w: %s changed since it was loaded; reload it to see the current content",
			errdefs.ErrConflict, cur.Name)
	}
	_, path, _ := FilePath(def, kind)
	mode := fs.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if cur.Exists {
		if err := writeAtomic(path+".bak", []byte(cur.Content), mode); err != nil {
			return StackFile{}, fmt.Errorf("back up %s: %w", cur.Name, err)
		}
	}
	if err := writeAtomic(path, content, mode); err != nil {
		return StackFile{}, fmt.Errorf("write %s: %w", cur.Name, err)
	}
	return StackFile{Kind: kind, Name: cur.Name, Content: string(content), Version: Version(content), Exists: true}, nil
}

func writeAtomic(path string, content []byte, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // a no-op once renamed
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DefaultCompose is the compose file of a new stack.
const DefaultCompose = `services:
  app:
    image: docker.io/library/nginx:1.27
    ports:
      - "8080:80"
    restart: unless-stopped
`

// Create makes a new managed stack: <stacks-dir>/<name>/compose.yaml with compose (#26).
// The content must have been validated by the caller.
func (s *Service) Create(ctx context.Context, name string, compose []byte) (Definition, error) {
	if err := identifiers.ValidateDockerCompat(name); err != nil {
		return Definition{}, fmt.Errorf("%w: stack name: %w", errdefs.ErrInvalidArgument, err)
	}
	if s.Dir == "" {
		return Definition{}, fmt.Errorf("%w: no stacks directory", errdefs.ErrFailedPrecondition)
	}
	if len(compose) > MaxFileSize {
		return Definition{}, fmt.Errorf("%w: the file is larger than %d KiB", errdefs.ErrInvalidArgument, MaxFileSize>>10)
	}
	defs, err := s.Definitions(ctx)
	if err != nil {
		return Definition{}, err
	}
	for _, d := range defs {
		if d.Name == name {
			return Definition{}, fmt.Errorf("%w: stack %q exists already", errdefs.ErrAlreadyExists, name)
		}
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Definition{}, fmt.Errorf("create stacks directory: %w", err)
	}
	dir, err := within(s.Dir, name)
	if err != nil {
		return Definition{}, err
	}
	if err := os.Mkdir(dir, 0o750); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Definition{}, fmt.Errorf("%w: %s exists already", errdefs.ErrAlreadyExists, dir)
		}
		return Definition{}, fmt.Errorf("create %s: %w", dir, err)
	}
	if err := writeAtomic(filepath.Join(dir, "compose.yaml"), compose, 0o640); err != nil {
		_ = os.RemoveAll(dir)
		return Definition{}, fmt.Errorf("write compose.yaml: %w", err)
	}
	return Definition{Name: name, Dir: dir, File: "compose.yaml", Mode: ModeManaged}, nil
}

// Find returns the definition of a stack nerdui knows the files of.
func (s *Service) Find(ctx context.Context, name string) (Definition, error) {
	defs, err := s.Definitions(ctx)
	if err != nil {
		return Definition{}, err
	}
	for _, d := range defs {
		if d.Name == name {
			return d, nil
		}
	}
	return Definition{}, fmt.Errorf("%w: stack %q has no known compose file; adopt it first", errdefs.ErrNotFound, name)
}
