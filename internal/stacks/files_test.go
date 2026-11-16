// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package stacks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/errdefs"
)

func TestStackFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("v1"), 0o640); err != nil {
		t.Fatal(err)
	}
	def := Definition{Name: "app", Dir: dir, File: "compose.yaml", Mode: ModeManaged}

	f, err := ReadFile(def, FileCompose)
	if err != nil || f.Content != "v1" || !f.Exists || f.Version != Version([]byte("v1")) || f.Name != "compose.yaml" {
		t.Fatalf("read: %+v %v", f, err)
	}
	env, err := ReadFile(def, FileEnv)
	if err != nil || env.Exists || env.Version != "" || env.Name != ".env" {
		t.Fatalf("missing .env: %+v %v", env, err)
	}
	if _, err := ReadFile(def, "passwd"); !errdefs.IsNotFound(err) {
		t.Errorf("unknown file: %v", err)
	}

	// Saving keeps the previous content as .bak and the file's permissions.
	saved, err := WriteFile(def, FileCompose, []byte("v2"), f.Version)
	if err != nil || saved.Version != Version([]byte("v2")) {
		t.Fatalf("write: %+v %v", saved, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "compose.yaml.bak")); string(b) != "v1" {
		t.Errorf("backup %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "compose.yaml")); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode())
	}
	// A stale version is a conflict and changes nothing.
	if _, err := WriteFile(def, FileCompose, []byte("v3"), f.Version); !errdefs.IsConflict(err) {
		t.Errorf("stale version: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "compose.yaml")); string(b) != "v2" {
		t.Errorf("content after conflict %q", b)
	}
	// A new .env is created only when it is expected to be missing, with 0600.
	if _, err := WriteFile(def, FileEnv, []byte("A=1\n"), "stale"); !errdefs.IsConflict(err) {
		t.Errorf("new .env with a version: %v", err)
	}
	if _, err := WriteFile(def, FileEnv, []byte("A=1\n"), ""); err != nil {
		t.Fatalf("new .env: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, ".env")); fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode %v", fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(dir, ".env.bak")); err == nil {
		t.Error("no backup for a new file")
	}
	if _, err := WriteFile(def, FileCompose, make([]byte, MaxFileSize+1), saved.Version); !errdefs.IsInvalidArgument(err) {
		t.Errorf("too large: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 { // compose.yaml, compose.yaml.bak, .env
		t.Errorf("leftovers: %v", entries)
	}
}

func TestCreateStack(t *testing.T) {
	stacksDir := filepath.Join(t.TempDir(), "stacks")
	s := &Service{Dir: stacksDir}
	ctx := context.Background()
	def, err := s.Create(ctx, "blog", []byte(DefaultCompose))
	if err != nil || def.Dir != filepath.Join(stacksDir, "blog") || def.File != "compose.yaml" || def.Mode != ModeManaged {
		t.Fatalf("create: %+v %v", def, err)
	}
	if b, _ := os.ReadFile(filepath.Join(def.Dir, "compose.yaml")); string(b) != DefaultCompose {
		t.Errorf("content %q", b)
	}
	if _, err := s.Create(ctx, "blog", nil); !errdefs.IsAlreadyExists(err) {
		t.Errorf("twice: %v", err)
	}
	if _, err := s.Create(ctx, "../evil", nil); !errdefs.IsInvalidArgument(err) {
		t.Errorf("bad name: %v", err)
	}
	if d, err := s.Find(ctx, "blog"); err != nil || d.Name != "blog" {
		t.Errorf("find: %+v %v", d, err)
	}
	if _, err := s.Find(ctx, "nope"); !errdefs.IsNotFound(err) {
		t.Errorf("find missing: %v", err)
	}
}
