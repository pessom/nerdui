// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

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

	"filippo.io/age"
	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/store"
)

// ErrWrongPassword means the archive could not be decrypted with the password.
var ErrWrongPassword = errors.New("wrong password, or not a nerdui backup")

// Target is where an archive is restored.
type Target struct {
	DataDir   string
	StacksDir string
	// KeyFile is where the key of secrets in the database is restored (#35).
	KeyFile string
	// Force replaces an existing database, stacks and Git stacks; they are kept next to
	// their place as <name>.before-restore-<time>.
	Force bool
	// Logf reports progress.
	Logf func(format string, args ...any)
}

// Restore decrypts an archive and puts its database, stacks and Git stacks in place. nerdui
// must not be running.
func Restore(ctx context.Context, r io.Reader, password string, t Target) (Manifest, error) {
	logf := t.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	id, err := age.NewScryptIdentity(password)
	if err != nil {
		return Manifest{}, err
	}
	dec, err := age.Decrypt(r, id)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrWrongPassword, err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return Manifest{}, fmt.Errorf("not a nerdui backup: %w", err)
	}

	if err := os.MkdirAll(t.DataDir, 0o700); err != nil {
		return Manifest{}, err
	}
	dbPath := filepath.Join(t.DataDir, store.FileName)
	gitDir := GitDir(t.DataDir)
	if !t.Force {
		for _, p := range []string{dbPath, t.StacksDir, gitDir} {
			if nonEmpty(p) {
				return Manifest{}, fmt.Errorf("%w: %s already exists; stop nerdui and use --force to replace it", errdefs.ErrAlreadyExists, p)
			}
		}
	}

	tmp, err := os.MkdirTemp(t.DataDir, ".restore-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // moved away on success
	m, err := extract(tar.NewReader(gz), tmp)
	if err != nil {
		return Manifest{}, err
	}
	if m.Format != Format {
		return Manifest{}, fmt.Errorf("unsupported backup format %d", m.Format)
	}

	// Open the restored database: migrations bring an older backup up to date, a newer one is
	// refused. Then move stack paths to this host's directories.
	st, err := store.Open(ctx, tmp, store.WithLogf(logf))
	if err != nil {
		return Manifest{}, fmt.Errorf("restored database: %w", err)
	}
	moves := [][2]string{{m.StacksDir, t.StacksDir}, {GitDir(m.DataDir), gitDir}}
	for _, mv := range moves {
		if mv[0] == "" || mv[0] == "." {
			continue
		}
		n, err := st.RewriteStackPaths(ctx, mv[0], mv[1])
		if err != nil {
			_ = st.Close()
			return Manifest{}, err
		}
		if n > 0 {
			logf("moved %d stack paths from %s to %s", n, mv[0], mv[1])
		}
	}
	if err := st.Close(); err != nil {
		return Manifest{}, err
	}
	// The restored database was opened in WAL mode: everything is in the main file after
	// the clean close, but remove leftovers so they are not moved with it.
	_ = os.Remove(filepath.Join(tmp, store.FileName+"-wal"))
	_ = os.Remove(filepath.Join(tmp, store.FileName+"-shm"))

	suffix := ".before-restore-" + time.Now().UTC().Format("20060102-150405")
	for _, p := range []string{dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Manifest{}, err
		}
	}
	for _, mv := range []struct{ from, to string }{
		{filepath.Join(tmp, dbName), dbPath},
		{filepath.Join(tmp, stacksName), t.StacksDir},
		{filepath.Join(tmp, gitName), gitDir},
		{filepath.Join(tmp, keyName), t.KeyFile},
	} {
		if mv.to == "" {
			continue
		}
		if _, err := os.Lstat(mv.to); err == nil {
			if err := os.Rename(mv.to, mv.to+suffix); err != nil {
				return Manifest{}, fmt.Errorf("keep %s: %w", mv.to, err)
			}
			logf("kept the previous %s as %s", mv.to, mv.to+suffix)
		}
		if _, err := os.Lstat(mv.from); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(mv.to), 0o700); err != nil {
			return Manifest{}, err
		}
		if err := os.Rename(mv.from, mv.to); err != nil {
			return Manifest{}, fmt.Errorf("restore %s: %w", mv.to, err)
		}
	}
	return m, nil
}

func nonEmpty(p string) bool {
	fi, err := os.Stat(p)
	if err != nil {
		return false
	}
	if !fi.IsDir() {
		return fi.Size() > 0
	}
	entries, err := os.ReadDir(p)
	return err == nil && len(entries) > 0
}

// extract unpacks the archive into dir, which it cannot leave (os.Root).
func extract(tr *tar.Reader, dir string) (Manifest, error) {
	var m Manifest
	root, err := os.OpenRoot(dir)
	if err != nil {
		return m, err
	}
	defer root.Close() //nolint:errcheck // nothing to flush
	seenManifest := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return m, fmt.Errorf("read backup: %w", err)
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "/"))
		top, _, _ := strings.Cut(name, "/")
		switch {
		case name == manifestName:
			if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
				return m, fmt.Errorf("manifest: %w", err)
			}
			seenManifest = true
			continue
		case name == dbName, name == keyName, top == stacksName, top == gitName:
		default:
			return m, fmt.Errorf("unexpected entry %q in the backup", hdr.Name)
		}
		mode := fs.FileMode(hdr.Mode & 0o777) //nolint:gosec // G115: masked to permission bits
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, mode|0o700); err != nil {
				return m, err
			}
		case tar.TypeReg:
			if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
				return m, err
			}
			f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return m, err
			}
			_, err = io.Copy(f, tr) //nolint:gosec // G110: the archive is the operator's own backup
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return m, err
			}
		case tar.TypeSymlink:
			if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
				return m, err
			}
			if err := root.Symlink(hdr.Linkname, name); err != nil {
				return m, err
			}
		default:
			// Skip other entry types.
		}
	}
	if !seenManifest {
		return m, errors.New("not a nerdui backup: no manifest")
	}
	if _, err := root.Stat(dbName); err != nil {
		return m, errors.New("not a nerdui backup: no database")
	}
	return m, nil
}
