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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/containerd/errdefs"
	nvolume "github.com/containerd/nerdctl/v2/pkg/cmd/volume"
)

// VolumeEntry is a file or directory in a volume (#58). Keep in sync with api/openapi.yaml.
type VolumeEntry struct {
	Name string `json:"name"`
	// Type is dir, file, symlink or other (devices, sockets, pipes).
	Type    string    `json:"type"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
	// Target of a symlink, and its type (dir or file) when it stays inside the volume.
	Target     string `json:"target,omitempty"`
	TargetType string `json:"targetType,omitempty"`
}

// VolumeFile is the beginning of a file in a volume, for a preview.
type VolumeFile struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	// Content is the text of the file up to MaxPreviewBytes; empty for binary files.
	Content   string `json:"content"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

// MaxPreviewBytes limits a file preview.
const MaxPreviewBytes = 256 << 10

// MaxVolumeEntries limits a directory listing.
const MaxVolumeEntries = 5000

// volumePath turns an API path ("/", "/etc/app.conf") into one relative to the volume root.
// os.Root rejects paths that leave the volume, also through symlinks.
func volumePath(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: invalid path", errdefs.ErrInvalidArgument)
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		return ".", nil
	}
	return p, nil
}

// volumeRoot opens the data directory of a volume.
func (cv clientVolumes) volumeRoot(namespace, name string) (*os.Root, error) {
	if err := validVolumeName(name); err != nil {
		return nil, err
	}
	cfg := cv.c.nerdctlFor(namespace)
	vs, err := nvolume.Store(namespace, cfg.DataRoot, cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("volume store: %w", err)
	}
	vol, err := vs.Get(name, false)
	if err != nil {
		return nil, fmt.Errorf("volume %s: %w", name, classify(err))
	}
	root, err := os.OpenRoot(vol.Mountpoint)
	if err != nil {
		return nil, fmt.Errorf("open volume %s: %w", name, err)
	}
	return root, nil
}

func fileErr(p string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %s does not exist", errdefs.ErrNotFound, p)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %s: permission denied", errdefs.ErrPermissionDenied, p)
	case strings.Contains(err.Error(), "escapes from parent") || strings.Contains(err.Error(), "path escapes"):
		return fmt.Errorf("%w: %s points outside the volume", errdefs.ErrInvalidArgument, p)
	}
	return err
}

func entryOf(root *os.Root, dir string, fi fs.FileInfo) VolumeEntry {
	e := VolumeEntry{Name: fi.Name(), Size: fi.Size(), Mode: fi.Mode().String(), ModTime: fi.ModTime().UTC()}
	switch {
	case fi.IsDir():
		e.Type, e.Size = "dir", 0
	case fi.Mode().IsRegular():
		e.Type = "file"
	case fi.Mode()&fs.ModeSymlink != 0:
		e.Type = "symlink"
		e.Target, _ = root.Readlink(path.Join(dir, fi.Name()))
		if ti, err := root.Stat(path.Join(dir, fi.Name())); err == nil {
			switch {
			case ti.IsDir():
				e.TargetType = "dir"
			case ti.Mode().IsRegular():
				e.TargetType = "file"
			}
		}
	default:
		e.Type = "other"
	}
	return e
}

// ListFiles lists a directory of a volume, directories first; truncated is set when it holds
// more than MaxVolumeEntries entries.
func (cv clientVolumes) ListFiles(namespace, name, dir string) (entries []VolumeEntry, truncated bool, err error) {
	p, err := volumePath(dir)
	if err != nil {
		return nil, false, err
	}
	root, err := cv.volumeRoot(namespace, name)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	f, err := root.Open(p)
	if err != nil {
		return nil, false, fileErr(dir, err)
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil {
		return nil, false, fileErr(dir, err)
	} else if !fi.IsDir() {
		return nil, false, fmt.Errorf("%w: %s is not a directory", errdefs.ErrInvalidArgument, dir)
	}
	dirents, err := f.ReadDir(MaxVolumeEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, fileErr(dir, err)
	}
	if len(dirents) > MaxVolumeEntries {
		dirents, truncated = dirents[:MaxVolumeEntries], true
	}
	entries = make([]VolumeEntry, 0, len(dirents))
	for _, d := range dirents {
		fi, err := d.Info()
		if err != nil {
			continue // removed meanwhile
		}
		entries = append(entries, entryOf(root, p, fi))
	}
	sort.Slice(entries, func(i, j int) bool {
		if (entries[i].Type == "dir") != (entries[j].Type == "dir") {
			return entries[i].Type == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, truncated, nil
}

// PreviewFile reads the beginning of a regular file of a volume.
func (cv clientVolumes) PreviewFile(namespace, name, file string) (VolumeFile, error) {
	p, err := volumePath(file)
	if err != nil {
		return VolumeFile{}, err
	}
	root, err := cv.volumeRoot(namespace, name)
	if err != nil {
		return VolumeFile{}, err
	}
	defer root.Close()
	f, fi, err := openRegular(root, p, file)
	if err != nil {
		return VolumeFile{}, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, MaxPreviewBytes))
	if err != nil {
		return VolumeFile{}, fileErr(file, err)
	}
	out := VolumeFile{Size: fi.Size(), ModTime: fi.ModTime().UTC(), Truncated: fi.Size() > int64(len(buf))}
	text := buf
	if out.Truncated {
		// Do not count a rune cut at the limit as binary.
		for i := 0; i < utf8.UTFMax && len(text) > 0 && !utf8.Valid(text); i++ {
			text = text[:len(text)-1]
		}
	}
	if !utf8.Valid(text) || strings.ContainsRune(string(text), 0) {
		out.Binary = true
	} else {
		out.Content = string(text)
	}
	return out, nil
}

func openRegular(root *os.Root, p, shown string) (*os.File, fs.FileInfo, error) {
	f, err := root.Open(p)
	if err != nil {
		return nil, nil, fileErr(shown, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, fileErr(shown, err)
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("%w: %s is not a regular file", errdefs.ErrInvalidArgument, shown)
	}
	return f, fi, nil
}

// VolumeDownload is a file or directory of a volume, ready to be written to a client.
type VolumeDownload struct {
	// Name is the base name of the download: the file's name, or <dir>.tar.gz.
	Name string
	// Size is the file size; -1 for an archive.
	Size  int64
	Write func(w io.Writer) error
	Close func() error
}

// Download opens a regular file of a volume, or a directory as a gzip-compressed tar archive
// (the whole volume for "/"). Write streams it; Close must be called.
func (cv clientVolumes) Download(namespace, name, target string) (VolumeDownload, error) {
	p, err := volumePath(target)
	if err != nil {
		return VolumeDownload{}, err
	}
	root, err := cv.volumeRoot(namespace, name)
	if err != nil {
		return VolumeDownload{}, err
	}
	fi, err := root.Stat(p)
	if err != nil {
		_ = root.Close()
		return VolumeDownload{}, fileErr(target, err)
	}
	if fi.Mode().IsRegular() {
		f, fi, err := openRegular(root, p, target)
		if err != nil {
			_ = root.Close()
			return VolumeDownload{}, err
		}
		return VolumeDownload{
			Name: fi.Name(), Size: fi.Size(),
			Write: func(w io.Writer) error {
				_, err := io.Copy(w, f)
				return err
			},
			Close: func() error { _ = f.Close(); return root.Close() },
		}, nil
	}
	if !fi.IsDir() {
		_ = root.Close()
		return VolumeDownload{}, fmt.Errorf("%w: %s is neither a file nor a directory", errdefs.ErrInvalidArgument, target)
	}
	base := path.Base(p)
	if p == "." {
		base = name
	}
	return VolumeDownload{
		Name: base + ".tar.gz", Size: -1,
		Write: func(w io.Writer) error { return writeTarGz(root, p, base, w) },
		Close: root.Close,
	}, nil
}

// writeTarGz writes the tree under dir as a gzip-compressed tar archive whose entries start
// with prefix/. Symlinks are stored as links; sockets and devices are skipped.
func writeTarGz(root *os.Root, dir, prefix string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	err := fs.WalkDir(root.FS(), dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := root.Lstat(p)
		if err != nil {
			return err
		}
		var link string
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			if link, err = root.Readlink(p); err != nil {
				return err
			}
		case !fi.IsDir() && !fi.Mode().IsRegular():
			return nil
		}
		hdr, err := tar.FileInfoHeader(fi, link)
		if err != nil {
			return err
		}
		rel := p
		if dir != "." {
			rel = strings.TrimPrefix(strings.TrimPrefix(p, dir), "/")
		} else if p == "." {
			rel = ""
		}
		hdr.Name = path.Join(prefix, rel)
		if fi.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		f, err := root.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.CopyN(tw, f, hdr.Size)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
