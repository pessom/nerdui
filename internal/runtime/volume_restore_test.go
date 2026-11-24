// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/errdefs"
)

func TestRestoreVolume(t *testing.T) {
	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "db", "base"), 0o750))
	must(os.WriteFile(filepath.Join(src, "db", "base", "1"), []byte("rows"), 0o640))
	must(os.WriteFile(filepath.Join(src, "conf"), []byte("x=1"), 0o600))
	must(os.Symlink("db/base/1", filepath.Join(src, "link")))
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	must(os.Chtimes(filepath.Join(src, "conf"), old, old))
	must(os.Chtimes(filepath.Join(src, "db"), old, old))

	root, err := os.OpenRoot(src)
	must(err)
	defer root.Close() //nolint:errcheck // test
	var archive bytes.Buffer
	must(writeTarGz(root, ".", "pgdata", &archive))

	vol := filepath.Join(t.TempDir(), "pgdata", "_data")
	must(os.MkdirAll(vol, 0o755))
	must(os.WriteFile(filepath.Join(vol, "stale"), []byte("old"), 0o600))
	must(restoreInto(vol, bytes.NewReader(archive.Bytes())))

	if b, err := os.ReadFile(filepath.Join(vol, "db", "base", "1")); err != nil || string(b) != "rows" {
		t.Errorf("file: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(vol, "stale")); !os.IsNotExist(err) {
		t.Errorf("old contents kept: %v", err)
	}
	if l, err := os.Readlink(filepath.Join(vol, "link")); err != nil || l != "db/base/1" {
		t.Errorf("link %q %v", l, err)
	}
	fi, err := os.Stat(filepath.Join(vol, "conf"))
	must(err)
	if fi.Mode().Perm() != 0o600 || !fi.ModTime().Equal(old) {
		t.Errorf("conf %v %v", fi.Mode(), fi.ModTime())
	}
	if fi, _ := os.Stat(filepath.Join(vol, "db")); fi.Mode().Perm() != 0o750 || !fi.ModTime().Equal(old) {
		t.Errorf("db dir %v %v", fi.Mode(), fi.ModTime())
	}
	if entries, _ := os.ReadDir(filepath.Dir(vol)); len(entries) != 1 {
		t.Errorf("leftovers next to the volume: %v", entries)
	}
}

func TestRestoreVolumeRefusesBadArchives(t *testing.T) {
	archive := func(names ...string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, n := range names {
			_ = tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
			_, _ = tw.Write([]byte("x"))
		}
		_ = tw.Close()
		_ = gz.Close()
		return buf.Bytes()
	}
	for name, data := range map[string][]byte{
		"not gzip":   []byte("plain"),
		"empty":      archive(),
		"two tops":   archive("a/x", "b/y"),
		"escape":     archive("a/../../x"),
		"top escape": archive("../x"),
	} {
		vol := filepath.Join(t.TempDir(), "_data")
		if err := os.MkdirAll(vol, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vol, "keep"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := restoreInto(vol, bytes.NewReader(data)); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(vol, "keep")); err != nil {
			t.Errorf("%s: the volume changed: %v", name, err)
		}
	}
}
