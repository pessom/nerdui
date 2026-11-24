// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	nvolume "github.com/containerd/nerdctl/v2/pkg/cmd/volume"
)

// Restore replaces the contents of a volume with a gzip-compressed tar archive as written by
// Download of the whole volume (#59): entries start with one top directory, which becomes the
// volume's root. The archive is unpacked next to the volume's data and swapped in, so a failed
// restore leaves the volume as it was. Owners, modes and times are kept. Containers using the
// volume must be stopped by the caller.
func (cv clientVolumes) Restore(namespace, name string, r io.Reader) error {
	if err := validVolumeName(name); err != nil {
		return err
	}
	cfg := cv.c.nerdctlFor(namespace)
	vs, err := nvolume.Store(namespace, cfg.DataRoot, cfg.Address)
	if err != nil {
		return fmt.Errorf("volume store: %w", err)
	}
	vol, err := vs.Get(name, false)
	if err != nil {
		return fmt.Errorf("volume %s: %w", name, classify(err))
	}
	return restoreInto(vol.Mountpoint, r)
}

// restoreInto unpacks the archive into a directory next to dir and swaps it in.
func restoreInto(dir string, r io.Reader) error {
	dir = filepath.Clean(dir)
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // renamed away on success
	if err := extractVolume(r, tmp); err != nil {
		return err
	}
	old := dir + ".before-restore-" + time.Now().UTC().Format("20060102-150405.000")
	if err := os.Rename(dir, old); err != nil {
		return fmt.Errorf("move the old data: %w", err)
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.Rename(old, dir)
		return fmt.Errorf("put the restored data in place: %w", err)
	}
	return os.RemoveAll(old)
}

// dirMeta is applied to directories after their contents are written.
type dirMeta struct {
	name string
	hdr  *tar.Header
}

func extractVolume(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("%w: not a gzip-compressed archive: %w", errdefs.ErrInvalidArgument, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // nothing to flush
	chown := os.Geteuid() == 0
	tr := tar.NewReader(gz)
	var dirs []dirMeta
	top := ""
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: read the archive: %w", errdefs.ErrInvalidArgument, err)
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "/"))
		first, rest, _ := strings.Cut(name, "/")
		if top == "" {
			top = first
		}
		if first != top || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("%w: entry %q is outside the archive's top directory %q", errdefs.ErrInvalidArgument, hdr.Name, top)
		}
		if rest == "" {
			rest = "."
		}
		mode := fs.FileMode(hdr.Mode) & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky) //nolint:gosec // G115: masked
		switch hdr.Typeflag {
		case tar.TypeDir:
			if rest != "." {
				if err := root.MkdirAll(rest, 0o700); err != nil {
					return err
				}
			}
			dirs = append(dirs, dirMeta{rest, hdr})
			continue
		case tar.TypeReg:
			if err := root.MkdirAll(path.Dir(rest), 0o700); err != nil {
				return err
			}
			f, err := root.OpenFile(rest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr) //nolint:gosec // G110: the operator's own backup
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := root.MkdirAll(path.Dir(rest), 0o700); err != nil {
				return err
			}
			if err := root.Symlink(hdr.Linkname, rest); err != nil {
				return err
			}
			if chown {
				_ = root.Lchown(rest, hdr.Uid, hdr.Gid)
			}
			continue
		default:
			continue // hard links, devices and others are not written by backups
		}
		if err := setMeta(root, rest, hdr, mode, chown); err != nil {
			return err
		}
	}
	if top == "" {
		return fmt.Errorf("%w: the archive is empty", errdefs.ErrInvalidArgument)
	}
	// Deepest first, so setting times of a parent is not undone by its children.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		mode := fs.FileMode(d.hdr.Mode) & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky) //nolint:gosec // G115: masked
		if err := setMeta(root, d.name, d.hdr, mode, chown); err != nil {
			return err
		}
	}
	return nil
}

func setMeta(root *os.Root, name string, hdr *tar.Header, mode fs.FileMode, chown bool) error {
	if chown {
		if err := root.Lchown(name, hdr.Uid, hdr.Gid); err != nil {
			return err
		}
	}
	// After chown, which clears setuid and setgid.
	if err := root.Chmod(name, mode); err != nil {
		return err
	}
	return root.Chtimes(name, hdr.ModTime, hdr.ModTime)
}
