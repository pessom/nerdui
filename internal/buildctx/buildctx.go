// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package buildctx prepares the context directory of an image build (#45, ADR-0061): a
// Dockerfile given as text, or an uploaded tar archive extracted safely.
package buildctx

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containerd/errdefs"
)

// Limits of an extracted archive.
const (
	// MaxSize bounds the extracted bytes of an archive.
	MaxSize = 2 << 30
	// maxEntries bounds the number of files and directories.
	maxEntries = 100_000
	// MaxDockerfile bounds a Dockerfile given as text.
	MaxDockerfile = 1 << 20
)

// WriteDockerfile writes a Dockerfile given as text into the empty context dir.
func WriteDockerfile(dir, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: the Dockerfile is empty", errdefs.ErrInvalidArgument)
	}
	if len(content) > MaxDockerfile {
		return fmt.Errorf("%w: the Dockerfile is larger than %d bytes", errdefs.ErrInvalidArgument, MaxDockerfile)
	}
	return os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o600)
}

// Extract extracts a tar archive, optionally gzip-compressed, into dir. Every file is created
// through an os.Root of dir, so neither "..", absolute names nor symbolic links can write
// outside it. Device files and other special entries are skipped. The archive may hold at
// most maxSize bytes (MaxSize when 0).
func Extract(dir string, r io.Reader, maxSize int64) error {
	if maxSize <= 0 {
		maxSize = MaxSize
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	br := bufio.NewReader(r)
	var in io.Reader = br
	if magic, _ := br.Peek(2); bytes.Equal(magic, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return fmt.Errorf("%w: gzip: %w", errdefs.ErrInvalidArgument, err)
		}
		defer func() { _ = gz.Close() }()
		in = gz
	}
	tr := tar.NewReader(in)
	var total int64
	for n := 0; ; n++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if n == 0 {
				return fmt.Errorf("%w: the archive is empty", errdefs.ErrInvalidArgument)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: not a tar archive: %w", errdefs.ErrInvalidArgument, err)
		}
		if n >= maxEntries {
			return fmt.Errorf("%w: the archive has more than %d entries", errdefs.ErrInvalidArgument, maxEntries)
		}
		name, ok := localName(h.Name)
		if !ok {
			return fmt.Errorf("%w: archive entry %q is outside the context", errdefs.ErrInvalidArgument, h.Name)
		}
		if name == "." {
			continue
		}
		if err := mkParents(root, name); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return entryErr(h.Name, err)
			}
		case tar.TypeReg:
			total += h.Size
			if total > maxSize {
				return fmt.Errorf("%w: the archive is larger than %d bytes", errdefs.ErrInvalidArgument, maxSize)
			}
			if err := writeFile(root, name, h, tr); err != nil {
				return entryErr(h.Name, err)
			}
		case tar.TypeSymlink:
			// The link is created as it is; the build reads it as a link, and later entries
			// cannot write through it, since os.Root refuses to leave dir.
			if err := root.Symlink(h.Linkname, name); err != nil {
				return entryErr(h.Name, err)
			}
		case tar.TypeLink:
			target, ok := localName(h.Linkname)
			if !ok {
				return fmt.Errorf("%w: archive link %q points outside the context", errdefs.ErrInvalidArgument, h.Name)
			}
			if err := root.Link(target, name); err != nil {
				return entryErr(h.Name, err)
			}
		default:
			// Devices, FIFOs and extended headers do not belong in a build context.
		}
	}
}

// localName cleans an archive name and tells whether it stays inside the archive root.
func localName(name string) (string, bool) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." {
		return ".", true
	}
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// mkParents creates the parent directories of name, which archives may omit.
func mkParents(root *os.Root, name string) error {
	if dir := path.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return entryErr(name, err)
		}
	}
	return nil
}

func writeFile(root *os.Root, name string, h *tar.Header, r io.Reader) error {
	mode := fs.FileMode(0o644)
	if h.FileInfo().Mode()&0o111 != 0 {
		mode = 0o755
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, r, h.Size); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func entryErr(name string, err error) error {
	return fmt.Errorf("%w: archive entry %q: %w", errdefs.ErrInvalidArgument, name, err)
}
