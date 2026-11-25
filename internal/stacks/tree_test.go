// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package stacks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
)

func TestTree(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	for name, content := range map[string]string{
		"compose.yaml":           "services: {}\n",
		".env":                   "A=1\n",
		"config/nginx/site.conf": "server {}\n",
		"config/app.ini":         "x=1\n",
		".git/HEAD":              "ref\n",
		"logo.png":               "\x89PNG\x00\x01",
		filepath.Join("..", filepath.Base(outside), "secret"): "s",
	} {
		p := filepath.Join(dir, name)
		if strings.HasPrefix(name, "..") {
			p = filepath.Join(outside, "secret")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	def := Definition{Name: "web", Dir: dir, File: "compose.yaml", Mode: ModeManaged}

	tree, err := ReadTree(def)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range tree.Entries {
		s := e.Path
		if e.Dir {
			s += "/"
		}
		if e.Protected {
			s += "!"
		}
		got = append(got, s)
	}
	want := "config/ config/nginx/ config/nginx/site.conf config/app.ini .env compose.yaml! logo.png"
	if strings.Join(got, " ") != want || tree.ReadOnly || tree.Truncated {
		t.Errorf("tree %v", got)
	}

	f, err := ReadPath(def, "config/app.ini")
	if err != nil || f.Content != "x=1\n" || !f.Exists || f.Version == "" {
		t.Fatalf("read %+v %v", f, err)
	}
	if f, err := ReadPath(def, "logo.png"); err != nil || !f.Binary || f.Content != "" {
		t.Errorf("binary %+v %v", f, err)
	}
	if f, err := ReadPath(def, "new.txt"); err != nil || f.Exists {
		t.Errorf("missing %+v %v", f, err)
	}
	for _, p := range []string{"../x", "/etc/passwd", "a/../../x", ".git/HEAD", "", "escape/secret"} {
		if _, err := ReadPath(def, p); err == nil {
			t.Errorf("%q read", p)
		}
	}

	// Write: new file with directories, conflict, replace.
	n, err := WritePath(def, "config/redis/redis.conf", []byte("maxmemory 1mb\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "config/redis/redis.conf")); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("new file %v %v", fi, err)
	}
	if _, err := WritePath(def, "config/redis/redis.conf", []byte("x"), ""); !errors.Is(err, errdefs.ErrAlreadyExists) {
		t.Errorf("create over existing: %v", err)
	}
	if _, err := WritePath(def, "config/app.ini", []byte("x"), "stale"); !errors.Is(err, errdefs.ErrConflict) {
		t.Errorf("stale write: %v", err)
	}
	if _, err := WritePath(def, "config/app.ini", []byte("x=2\n"), f.Version); err != nil {
		t.Errorf("replace: %v", err)
	}
	if _, err := WritePath(def, "logo.png", []byte("x"), ""); !errors.Is(err, errdefs.ErrFailedPrecondition) {
		t.Errorf("binary write: %v", err)
	}
	if _, err := WritePath(def, "escape/x", []byte("x"), ""); err == nil {
		t.Error("write through a symbolic link")
	}
	if _, err := os.Stat(filepath.Join(outside, "x")); !os.IsNotExist(err) {
		t.Error("file written outside")
	}

	// Delete: compose protected, version checked, empty directories only.
	if err := DeletePath(def, "compose.yaml", ""); !errors.Is(err, errdefs.ErrFailedPrecondition) {
		t.Errorf("delete compose: %v", err)
	}
	if err := DeletePath(def, "config/redis/redis.conf", "stale"); !errors.Is(err, errdefs.ErrConflict) {
		t.Errorf("stale delete: %v", err)
	}
	if err := DeletePath(def, "config/redis", ""); !errors.Is(err, errdefs.ErrFailedPrecondition) {
		t.Errorf("delete non-empty directory: %v", err)
	}
	if err := DeletePath(def, "config/redis/redis.conf", n.Version); err != nil {
		t.Errorf("delete: %v", err)
	}
	if err := DeletePath(def, "config/redis", ""); err != nil {
		t.Errorf("delete empty directory: %v", err)
	}
	if err := DeletePath(def, "nothing", ""); !errors.Is(err, errdefs.ErrNotFound) {
		t.Errorf("delete missing: %v", err)
	}

	// Git stacks are read-only.
	git := def
	git.Mode = ModeGit
	if tree, _ := ReadTree(git); !tree.ReadOnly {
		t.Error("Git tree not read-only")
	}
	if _, err := WritePath(git, "x.txt", []byte("x"), ""); !errors.Is(err, errdefs.ErrFailedPrecondition) {
		t.Errorf("Git write: %v", err)
	}
	if err := DeletePath(git, "config/app.ini", ""); !errors.Is(err, errdefs.ErrFailedPrecondition) {
		t.Errorf("Git delete: %v", err)
	}
}
