// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package backup writes and restores password-encrypted archives of nerdui's own data: the
// database, the managed stacks and the Git stacks (#89, ADR-0047).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"filippo.io/age"
	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/version"
)

const (
	// Format is the archive layout version.
	Format = 1
	// MinPasswordLen of a backup password; the archive holds credentials.
	MinPasswordLen = 12
	// workFactor of age's scrypt: 2^16 × 1 KiB = 64 MiB of memory, about a quarter second.
	workFactor = 16

	manifestName = "manifest.json"
	dbName       = "nerdui.db"
	stacksName   = "stacks"
	gitName      = "git"
	keyName      = "secret.key"
)

// Manifest describes an archive.
type Manifest struct {
	Format  int       `json:"format"`
	Created time.Time `json:"created"`
	Nerdui  string    `json:"nerdui"`
	Schema  int64     `json:"schema"`
	// DataDir and StacksDir of the source host: stack paths below them are moved to the
	// directories of the host that restores the archive.
	DataDir   string `json:"dataDir"`
	StacksDir string `json:"stacksDir"`
}

// Source is what a backup contains.
type Source struct {
	Store     *store.Store
	DataDir   string
	StacksDir string
	// KeyFile encrypts secrets in the database (#35); it is included when it exists.
	KeyFile string
}

// GitDir is where Git stacks are cloned.
func GitDir(dataDir string) string { return filepath.Join(dataDir, gitName) }

// ValidatePassword checks a backup password.
func ValidatePassword(p string) error {
	if utf8.RuneCountInString(p) < MinPasswordLen || len(p) > 1024 {
		return fmt.Errorf("%w: the backup password must have %d to 1024 characters", errdefs.ErrInvalidArgument, MinPasswordLen)
	}
	return nil
}

// FileName is the suggested name of an archive created at t.
func FileName(t time.Time) string {
	return "nerdui-backup-" + t.UTC().Format("20060102-150405") + ".tar.gz.age"
}

// Write writes an encrypted archive of src to w.
func Write(ctx context.Context, src Source, password string, w io.Writer) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(src.DataDir, ".backup-")
	if err != nil {
		return fmt.Errorf("temporary directory: %w", err)
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best effort
	snapshot := filepath.Join(tmp, dbName)
	if err := src.Store.Snapshot(ctx, snapshot); err != nil {
		return err
	}
	schema, err := src.Store.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("schema version: %w", err)
	}
	m := Manifest{
		Format: Format, Created: time.Now().UTC(), Nerdui: version.Get().Version, Schema: schema,
		DataDir: filepath.Clean(src.DataDir), StacksDir: filepath.Clean(src.StacksDir),
	}

	r, err := age.NewScryptRecipient(password)
	if err != nil {
		return err
	}
	r.SetWorkFactor(workFactor)
	enc, err := age.Encrypt(w, r)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(mb)), ModTime: m.Created}); err != nil {
		return err
	}
	if _, err := tw.Write(mb); err != nil {
		return err
	}
	if err := addFile(tw, snapshot, dbName); err != nil {
		return err
	}
	if src.KeyFile != "" {
		if err := addFile(tw, src.KeyFile, keyName); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("secret key: %w", err)
		}
	}
	if err := addTree(ctx, tw, src.StacksDir, stacksName); err != nil {
		return fmt.Errorf("stacks: %w", err)
	}
	if err := addTree(ctx, tw, GitDir(src.DataDir), gitName); err != nil {
		return fmt.Errorf("git stacks: %w", err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return enc.Close()
}

func addFile(tw *tar.Writer, file, name string) error {
	f, err := os.Open(file) //nolint:gosec // G304: a snapshot nerdui wrote
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(fi, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, hdr.Size)
	return err
}

// addTree adds dir (if it exists) under prefix/. Symlinks are kept as links; sockets and
// devices, and Git clones in progress (.clone-*), are skipped.
func addTree(ctx context.Context, tw *tar.Writer, dir, prefix string) error {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // read-only
	return fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".clone-") {
			return fs.SkipDir
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
		hdr.Name = path.Join(prefix, p)
		if fi.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uname, hdr.Gname = "", ""
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
		defer f.Close() //nolint:errcheck // read-only
		_, err = io.CopyN(tw, f, hdr.Size)
		return err
	})
}
