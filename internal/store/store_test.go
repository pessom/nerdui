// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/pessom/nerdui/internal/store/db"
)

const now = int64(1_800_000_000)

// latestVersion is the number of embedded migrations.
const latestVersion = 3

func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func migrationsV1(t *testing.T) fstest.MapFS {
	t.Helper()
	b, err := embedded.ReadFile("migrations/00001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"00001_initial.sql": {Data: b}}
}

func migrationsV2(t *testing.T) fstest.MapFS {
	m := migrationsV1(t)
	m["00002_extra.sql"] = &fstest.MapFile{Data: []byte(
		"-- +goose Up\nCREATE TABLE extra (id INTEGER PRIMARY KEY) STRICT;\n")}
	return m
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, FileName+".bak-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s := openTest(t, dir)
	if v, err := s.SchemaVersion(ctx); err != nil || v != latestVersion {
		t.Fatalf("version = %d, err = %v", v, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)
	if v, _ := s2.SchemaVersion(ctx); v != latestVersion {
		t.Fatalf("version after reopen = %d", v)
	}
	if b := backups(t, dir); len(b) != 0 {
		t.Errorf("unexpected backups without pending migrations: %v", b)
	}
}

// TestUserStateMigration upgrades a database with an existing admin (#74): the account
// stays active, needs no password change and has no sign-in time yet.
func TestUserStateMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := open(ctx, dir, migrationsV1(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.writer.ExecContext(ctx,
		"INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES ('admin', 'h', 'admin', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s = openTest(t, dir)
	u, err := s.Read().GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.DisabledAt.Valid || u.MustChangePassword != 0 || u.LastLoginAt.Valid {
		t.Errorf("migrated user = %+v", u)
	}
	if n, err := s.Read().CountActiveAdmins(ctx); err != nil || n != 1 {
		t.Errorf("active admins = %d, %v", n, err)
	}
	if _, err := s.Write().CreateUser(ctx, db.CreateUserParams{
		Username: "x", PasswordHash: "h", Role: "admin", MustChangePassword: 2, Now: now,
	}); err == nil {
		t.Error("must_change_password = 2 accepted")
	}
}

func TestFileMode(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != FileMode {
		t.Errorf("db mode = %v, want %v", info.Mode().Perm(), FileMode)
	}
}

func TestBackupBeforeMigration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := open(ctx, dir, migrationsV1(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write().SetSetting(ctx, db.SetSettingParams{Key: "k", Value: "v", Now: now}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s, err = open(ctx, dir, migrationsV2(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.SchemaVersion(ctx); v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}

	b := backups(t, dir)
	if len(b) != 1 {
		t.Fatalf("backups = %v, want exactly one", b)
	}
	info, err := os.Stat(b[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != FileMode {
		t.Errorf("backup mode = %v", info.Mode().Perm())
	}

	// The backup is a consistent copy of the v1 database.
	bs, err := open(ctx, t.TempDir(), migrationsV1(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = bs.Close()
	if err := os.Rename(b[0], filepath.Join(filepath.Dir(bs.Path()), FileName)); err != nil {
		t.Fatal(err)
	}
	restored, err := open(ctx, filepath.Dir(bs.Path()), migrationsV1(t))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if v, err := restored.Read().GetSetting(ctx, "k"); err != nil || v != "v" {
		t.Errorf("backup content: %q, %v", v, err)
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := open(ctx, dir, migrationsV2(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	_, err = open(ctx, dir, migrationsV1(t))
	if !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("err = %v, want ErrNewerSchema", err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	w, r := s.Write(), s.Read()

	u, err := w.CreateUser(ctx, db.CreateUserParams{Username: "Admin", PasswordHash: "h", Role: "admin", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.GetUserByUsername(ctx, "admin"); err != nil || got.ID != u.ID {
		t.Fatalf("case-insensitive lookup: %+v, %v", got, err)
	}
	_, err = w.CreateUser(ctx, db.CreateUserParams{Username: "ADMIN", PasswordHash: "h", Role: "admin", Now: now})
	if !IsUniqueViolation(err) {
		t.Fatalf("duplicate username: err = %v", err)
	}
	_, err = w.CreateUser(ctx, db.CreateUserParams{Username: "x", PasswordHash: "h", Role: "root", Now: now})
	if err == nil {
		t.Fatal("invalid role accepted")
	}
	if n, _ := r.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers = %d", n)
	}

	token := []byte("hashed-token")
	err = w.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: token, UserID: u.ID, CsrfToken: "c", Now: now, ExpiresAt: now + 3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := r.GetSession(ctx, token); err != nil || sess.UserID != u.ID {
		t.Fatalf("GetSession: %+v, %v", sess, err)
	}
	if n, _ := w.TouchSession(ctx, db.TouchSessionParams{TokenHash: token, Now: now + 10}); n != 1 {
		t.Errorf("TouchSession rows = %d", n)
	}

	// Expired by absolute lifetime.
	if n, _ := w.DeleteExpiredSessions(ctx, db.DeleteExpiredSessionsParams{Now: now + 7200, IdleBefore: 0}); n != 1 {
		t.Errorf("DeleteExpiredSessions rows = %d", n)
	}
	if _, err := r.GetSession(ctx, token); !IsNotFound(err) {
		t.Errorf("session still present: %v", err)
	}

	// Sessions are removed together with their user (foreign keys enabled).
	_ = w.CreateSession(ctx, db.CreateSessionParams{TokenHash: token, UserID: u.ID, CsrfToken: "c", Now: now, ExpiresAt: now + 3600})
	if n, err := w.DeleteUser(ctx, u.ID); err != nil || n != 1 {
		t.Fatalf("DeleteUser: %d, %v", n, err)
	}
	if _, err := r.GetSession(ctx, token); !IsNotFound(err) {
		t.Errorf("session not cascaded: %v", err)
	}
}

func TestSettingsAndStacks(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	w, r := s.Write(), s.Read()

	for _, v := range []string{"a", "b"} {
		if err := w.SetSetting(ctx, db.SetSettingParams{Key: "theme", Value: v, Now: now}); err != nil {
			t.Fatal(err)
		}
	}
	if v, _ := r.GetSetting(ctx, "theme"); v != "b" {
		t.Errorf("upsert: got %q", v)
	}

	if err := w.RegisterStack(ctx, db.RegisterStackParams{Name: "media", Path: "/srv/media", Mode: "external", Now: now}); err != nil {
		t.Fatal(err)
	}
	err := w.RegisterStack(ctx, db.RegisterStackParams{Name: "other", Path: "/srv/media", Mode: "managed", Now: now})
	if !IsUniqueViolation(err) {
		t.Errorf("duplicate path: %v", err)
	}
	if list, _ := r.ListStacks(ctx); len(list) != 1 || list[0].Name != "media" {
		t.Errorf("ListStacks = %+v", list)
	}
}

func TestReaderIsReadOnly(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	if err := s.Read().SetSetting(ctx, db.SetSettingParams{Key: "k", Value: "v", Now: now}); err == nil {
		t.Fatal("write through the read pool succeeded")
	}
}

func TestTx(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())

	boom := errors.New("boom")
	err := s.Tx(ctx, func(q *db.Queries) error {
		if err := q.SetSetting(ctx, db.SetSettingParams{Key: "k", Value: "v", Now: now}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Read().GetSetting(ctx, "k"); !IsNotFound(err) {
		t.Errorf("rolled back write is visible: %v", err)
	}

	err = s.Tx(ctx, func(q *db.Queries) error {
		return q.SetSetting(ctx, db.SetSettingParams{Key: "k", Value: "v", Now: now})
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Read().GetSetting(ctx, "k"); v != "v" {
		t.Errorf("committed value = %q", v)
	}
}
