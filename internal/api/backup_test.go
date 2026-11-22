// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/backup"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/store"
)

func TestCreateBackup(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck // test
	src := &backup.Source{Store: st, DataDir: data, StacksDir: filepath.Join(data, "stacks")}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(writable(), "default")), Backup: src}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, func(*http.Request) (authz.Subject, bool) {
			return authz.Subject{Role: authz.RoleAdmin}, true
		}, nil))
	})

	if rec := send(t, r, http.MethodPost, "/api/v1/backup", `{"password":"short"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("short password: %d %s", rec.Code, rec.Body)
	}
	rec := send(t, r, http.MethodPost, "/api/v1/backup", `{"password":"correct horse battery"}`)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), `attachment; filename="nerdui-backup-`) {
		t.Fatalf("backup: %d %v", rec.Code, rec.Header())
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("age-encryption.org/v1")) {
		t.Errorf("not an age file: %.40q", rec.Body.Bytes())
	}
	target := t.TempDir()
	if _, err := backup.Restore(ctx, bytes.NewReader(rec.Body.Bytes()), "correct horse battery",
		backup.Target{DataDir: target, StacksDir: filepath.Join(target, "stacks")}); err != nil {
		t.Errorf("restore the download: %v", err)
	}
}
