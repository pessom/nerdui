// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/containerd/errdefs"
)

// MaxTreeEntries bounds the file tree of a stack; a larger tree is cut off.
const MaxTreeEntries = 2000

// TreeEntry is a file or directory of a stack's directory (#27). Path is relative to the
// directory, with slashes. Protected files cannot be deleted: the stack's compose file. Keep in
// sync with api/openapi.yaml.
type TreeEntry struct {
	Path      string `json:"path"`
	Dir       bool   `json:"dir"`
	Size      int64  `json:"size"`
	Protected bool   `json:"protected,omitempty"`
}

// Tree is the file tree of a stack. ReadOnly is set for Git stacks, whose files come from the
// repository. Keep in sync with api/openapi.yaml.
type Tree struct {
	Entries   []TreeEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
	ReadOnly  bool        `json:"readOnly"`
}

// PathFile is the content of a file of a stack's directory, by its path (#27). Binary files and
// files larger than MaxFileSize have no content and cannot be edited. Keep in sync with
// api/openapi.yaml.
type PathFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Version string `json:"version"`
	Exists  bool   `json:"exists"`
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary,omitempty"`
	TooBig  bool   `json:"tooBig,omitempty"`
}

// skippedDirs are not shown in the tree: the Git metadata of a clone.
var skippedDirs = map[string]bool{".git": true}

// CleanPath checks a path relative to a stack's directory: slash-separated, no "." or ".."
// elements, not into skipped directories.
func CleanPath(p string) (string, error) {
	p = strings.TrimPrefix(p, "./")
	if p == "" || !fs.ValidPath(p) || p == "." {
		return "", fmt.Errorf("%w: %q is not a path in the stack's directory", errdefs.ErrInvalidArgument, p)
	}
	if first, _, _ := strings.Cut(p, "/"); skippedDirs[first] {
		return "", fmt.Errorf("%w: %s cannot be changed here", errdefs.ErrInvalidArgument, first)
	}
	return p, nil
}

// openRoot opens the directory of a stack. Every access goes through the root, so a path or
// a symbolic link cannot leave the directory.
func openRoot(def Definition) (*os.Root, error) {
	if def.Dir == "" {
		return nil, fmt.Errorf("%w: the directory of %q is unknown; adopt it first", errdefs.ErrFailedPrecondition, def.Name)
	}
	r, err := os.OpenRoot(def.Dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", def.Dir, err)
	}
	return r, nil
}

// ReadTree lists the files and directories of a stack's directory, directories first, up to
// MaxTreeEntries. Symbolic links and other special files are left out.
func ReadTree(def Definition) (Tree, error) {
	root, err := openRoot(def)
	if err != nil {
		return Tree{}, err
	}
	defer root.Close() //nolint:errcheck // read only
	t := Tree{Entries: []TreeEntry{}, ReadOnly: def.Mode == ModeGit}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil // unreadable: left out
		}
		if p == "." {
			return nil
		}
		if d.IsDir() && skippedDirs[d.Name()] {
			return fs.SkipDir
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil
		}
		if len(t.Entries) >= MaxTreeEntries {
			t.Truncated = true
			return fs.SkipAll
		}
		e := TreeEntry{Path: p, Dir: d.IsDir()}
		if !e.Dir {
			if fi, err := d.Info(); err == nil {
				e.Size = fi.Size()
			}
			e.Protected = p == def.File
		}
		t.Entries = append(t.Entries, e)
		return nil
	})
	if err != nil {
		return Tree{}, fmt.Errorf("read %s: %w", def.Dir, err)
	}
	sort.SliceStable(t.Entries, func(i, j int) bool { return treeLess(t.Entries[i], t.Entries[j]) })
	return t, nil
}

// treeLess orders a tree as a file manager does: within a directory, directories first, then
// by name.
func treeLess(a, b TreeEntry) bool {
	ap, bp := strings.Split(a.Path, "/"), strings.Split(b.Path, "/")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		aDir, bDir := i < len(ap)-1 || a.Dir, i < len(bp)-1 || b.Dir
		if aDir != bDir {
			return aDir
		}
		return ap[i] < bp[i]
	}
	return len(ap) < len(bp)
}

// ReadPath reads a file of a stack's directory; a missing file is returned with Exists false.
func ReadPath(def Definition, p string) (PathFile, error) {
	p, err := CleanPath(p)
	if err != nil {
		return PathFile{}, err
	}
	root, err := openRoot(def)
	if err != nil {
		return PathFile{}, err
	}
	defer root.Close() //nolint:errcheck // read only
	out := PathFile{Path: p}
	f, err := root.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return PathFile{}, fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close() //nolint:errcheck // read only
	fi, err := f.Stat()
	if err != nil {
		return PathFile{}, err
	}
	if !fi.Mode().IsRegular() {
		return PathFile{}, fmt.Errorf("%w: %s is not a file", errdefs.ErrInvalidArgument, p)
	}
	out.Exists, out.Size = true, fi.Size()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return PathFile{}, fmt.Errorf("read %s: %w", p, err)
	}
	out.Version = Version(b)
	switch {
	case len(b) > MaxFileSize:
		out.TooBig, out.Version = true, ""
	case bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b):
		out.Binary = true
	default:
		out.Content = string(b)
	}
	return out, nil
}

// checkWritable refuses changes to the files of Git stacks: they come from the repository.
func checkWritable(def Definition) error {
	if def.Mode == ModeGit {
		return fmt.Errorf("%w: the files of a Git stack come from its repository; change them there and pull", errdefs.ErrFailedPrecondition)
	}
	return nil
}

// WritePath creates or replaces a text file of a stack's directory if its version is still
// version (empty: the file must not exist), creating parent directories. The compose file and
// .env go through WriteFile instead, which keeps a backup. New files are readable by the
// containers they are mounted into, except .env files.
func WritePath(def Definition, p string, content []byte, version string) (PathFile, error) {
	if err := checkWritable(def); err != nil {
		return PathFile{}, err
	}
	if len(content) > MaxFileSize {
		return PathFile{}, fmt.Errorf("%w: the file is larger than %d KiB", errdefs.ErrInvalidArgument, MaxFileSize>>10)
	}
	cur, err := ReadPath(def, p)
	if err != nil {
		return PathFile{}, err
	}
	p = cur.Path
	if cur.Exists && (cur.Binary || cur.TooBig) {
		return PathFile{}, fmt.Errorf("%w: %s is not a text file of up to %d KiB", errdefs.ErrFailedPrecondition, p, MaxFileSize>>10)
	}
	if cur.Version != version {
		if version == "" {
			return PathFile{}, fmt.Errorf("%w: %s exists already", errdefs.ErrAlreadyExists, p)
		}
		return PathFile{}, fmt.Errorf("%w: %s changed since it was loaded; reload it to see the current content", errdefs.ErrConflict, p)
	}
	root, err := openRoot(def)
	if err != nil {
		return PathFile{}, err
	}
	defer root.Close() //nolint:errcheck // closed after the write
	mode := fs.FileMode(0o644)
	if strings.HasPrefix(path.Base(p), ".env") {
		mode = 0o600
	}
	if fi, err := root.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	if dir := path.Dir(p); dir != "." {
		if err := root.MkdirAll(dir, 0o750); err != nil {
			return PathFile{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := writeAtomicRoot(root, p, content, mode); err != nil {
		return PathFile{}, fmt.Errorf("write %s: %w", p, err)
	}
	return PathFile{Path: p, Content: string(content), Version: Version(content), Exists: true, Size: int64(len(content))}, nil
}

// writeAtomicRoot writes a temporary file next to p and renames it over p.
func writeAtomicRoot(root *os.Root, p string, content []byte, mode fs.FileMode) error {
	tmp := path.Join(path.Dir(p), "."+path.Base(p)+".tmp-"+Version(content)[:12])
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }() // a no-op once renamed
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
	if err := root.Chmod(tmp, mode); err != nil {
		return err
	}
	return root.Rename(tmp, p)
}

// DeletePath removes a file, or an empty directory, of a stack's directory. A file is removed
// only if its version is still version; the compose file cannot be removed.
func DeletePath(def Definition, p, version string) error {
	if err := checkWritable(def); err != nil {
		return err
	}
	p, err := CleanPath(p)
	if err != nil {
		return err
	}
	if p == def.File {
		return fmt.Errorf("%w: %s is the stack's compose file; delete the stack instead", errdefs.ErrFailedPrecondition, p)
	}
	root, err := openRoot(def)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // closed after the removal
	fi, err := root.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", errdefs.ErrNotFound, p)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		cur, err := ReadPath(def, p)
		if err != nil {
			return err
		}
		if cur.Version != version && (!cur.TooBig || version != "") {
			return fmt.Errorf("%w: %s changed since it was loaded; reload it to see the current content", errdefs.ErrConflict, p)
		}
	}
	if err := root.Remove(p); err != nil {
		if fi.IsDir() {
			return fmt.Errorf("%w: %s is not empty", errdefs.ErrFailedPrecondition, p)
		}
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}
