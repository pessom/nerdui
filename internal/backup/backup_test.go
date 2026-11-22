// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

const password = "correct horse battery"

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// source creates a data directory with a user, a managed stack, an external stack and a Git
// stack, and returns its archive.
func source(t *testing.T) (Source, []byte) {
	t.Helper()
	ctx := context.Background()
	data := t.TempDir()
	stacks := filepath.Join(data, "stacks")
	st, err := store.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Write().CreateUser(ctx, db.CreateUserParams{Username: "admin", PasswordHash: "x", Role: "admin", Now: 1}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []db.RegisterStackParams{
		{Name: "shop", Path: filepath.Join(stacks, "shop"), Mode: "managed", Now: 1},
		{Name: "blog", Path: "/srv/blog", Mode: "external", Now: 1},
		{Name: "demo", Path: filepath.Join(data, "git", "demo", "deploy"), Mode: "managed", Now: 1},
	} {
		if err := st.Write().RegisterStack(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(stacks, "shop", "compose.yaml"), "services: {}\n")
	write(t, filepath.Join(stacks, "shop", ".env"), "TOKEN=secret\n")
	write(t, filepath.Join(data, "git", "demo", "deploy", "compose.yaml"), "services: {}\n")
	write(t, filepath.Join(data, "git", ".clone-123", "partial"), "x")
	if err := os.Symlink("compose.yaml", filepath.Join(stacks, "shop", "docker-compose.yml")); err != nil {
		t.Fatal(err)
	}
	src := Source{Store: st, DataDir: data, StacksDir: stacks}
	var buf bytes.Buffer
	if err := Write(ctx, src, password, &buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("TOKEN=secret")) || bytes.Contains(buf.Bytes(), []byte("services")) {
		t.Fatal("the archive is not encrypted")
	}
	return src, buf.Bytes()
}

func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	_, archive := source(t)

	// Restore on a "new server" with other directories.
	data := t.TempDir()
	stacks := filepath.Join(t.TempDir(), "my-stacks")
	m, err := Restore(ctx, bytes.NewReader(archive), password, Target{DataDir: data, StacksDir: stacks})
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != Format || m.Schema == 0 || time.Since(m.Created) > time.Minute {
		t.Errorf("manifest %+v", m)
	}
	if b, err := os.ReadFile(filepath.Join(stacks, "shop", ".env")); err != nil || string(b) != "TOKEN=secret\n" {
		t.Errorf(".env: %q %v", b, err)
	}
	if l, err := os.Readlink(filepath.Join(stacks, "shop", "docker-compose.yml")); err != nil || l != "compose.yaml" {
		t.Errorf("symlink: %q %v", l, err)
	}
	if _, err := os.Stat(filepath.Join(data, "git", "demo", "deploy", "compose.yaml")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(data, "git", ".clone-123")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("clone in progress restored: %v", err)
	}
	st, err := store.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck // test
	if _, err := st.Read().GetUserByUsername(ctx, "admin"); err != nil {
		t.Errorf("user: %v", err)
	}
	list, err := st.Read().ListStacks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, s := range list {
		paths[s.Name] = s.Path
	}
	want := map[string]string{
		"shop": filepath.Join(stacks, "shop"),
		"blog": "/srv/blog",
		"demo": filepath.Join(data, "git", "demo", "deploy"),
	}
	for n, p := range want {
		if paths[n] != p {
			t.Errorf("stack %s path %q, want %q", n, paths[n], p)
		}
	}
}

func TestRestoreRefusesAndReplaces(t *testing.T) {
	ctx := context.Background()
	_, archive := source(t)
	data := t.TempDir()
	stacks := filepath.Join(data, "stacks")
	write(t, filepath.Join(data, store.FileName), "old database")

	if _, err := Restore(ctx, bytes.NewReader(archive), "wrong password!!", Target{DataDir: data, StacksDir: stacks, Force: true}); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := Restore(ctx, bytes.NewReader(archive), password, Target{DataDir: data, StacksDir: stacks}); !errdefs.IsAlreadyExists(err) {
		t.Errorf("existing database without --force: %v", err)
	}
	if _, err := Restore(ctx, bytes.NewReader(archive), password, Target{DataDir: data, StacksDir: stacks, Force: true}); err != nil {
		t.Fatal(err)
	}
	kept, _ := filepath.Glob(filepath.Join(data, store.FileName+".before-restore-*"))
	if len(kept) != 1 {
		t.Fatalf("previous database kept as %v", kept)
	}
	if b, _ := os.ReadFile(kept[0]); string(b) != "old database" {
		t.Errorf("kept %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(data, ".restore-*")); len(left) != 0 {
		t.Errorf("temporary directories left: %v", left)
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); !errdefs.IsInvalidArgument(err) {
		t.Error(err)
	}
	if err := ValidatePassword(password); err != nil {
		t.Error(err)
	}
	if _, err := Restore(context.Background(), bytes.NewReader([]byte("garbage")), password, Target{DataDir: t.TempDir()}); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("garbage: %v", err)
	}
}
