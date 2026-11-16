// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/stacks"
)

func TestStackFilesAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The runtime is read-only: editing files does not need a writable nerdctl.
	rt := &fakeRuntime{}
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: m, Stacks: &stacks.Service{Dir: dir}}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const base = "/api/v1/environments/local/stacks"

	rec := send(t, r, http.MethodGet, base+"/app/files/compose", "")
	var f stacks.StackFile
	_ = json.Unmarshal(rec.Body.Bytes(), &f)
	if rec.Code != http.StatusOK || f.Content != "services: {}\n" || f.Version == "" {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodGet, base+"/app/files/passwd", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown file: %d", rec.Code)
	}
	if rec := send(t, r, http.MethodGet, base+"/ghost/files/compose", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown stack: %d", rec.Code)
	}

	// Validation reports the loader's error and its line.
	rec = send(t, r, http.MethodPost, base+"/app/validate", `{"compose":"services:\n  broken"}`)
	var v StackValidation
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != http.StatusOK || v.Valid || v.Line != 2 || !strings.Contains(v.Error, "did not find expected key") {
		t.Errorf("validate broken: %d %s", rec.Code, rec.Body)
	}
	rec = send(t, r, http.MethodPost, base+"/app/validate", `{}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if !v.Valid || len(v.Services) != 1 {
		t.Errorf("validate disk: %s", rec.Body)
	}

	// Save: invalid content is refused, a stale version conflicts, a valid save backs up.
	put := func(content, version string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"content": content, "version": version})
		return send(t, r, http.MethodPut, base+"/app/files/compose", string(b))
	}
	if rec := put("services:\n  broken", f.Version); rec.Code != http.StatusBadRequest {
		t.Errorf("save invalid: %d %s", rec.Code, rec.Body)
	}
	if rec := put("services:\n  web: {image: nginx}\n", "stale"); rec.Code != http.StatusConflict {
		t.Errorf("save stale: %d %s", rec.Code, rec.Body)
	}
	if rec := put("services:\n  web: {image: nginx}\n", f.Version); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "app", "compose.yaml.bak")); string(b) != "services: {}\n" {
		t.Errorf("backup %q", b)
	}
	// .env: created when missing.
	if rec := send(t, r, http.MethodPut, base+"/app/files/env", `{"content":"A=1\n","version":""}`); rec.Code != http.StatusOK {
		t.Errorf("save .env: %d %s", rec.Code, rec.Body)
	}

	// No saving while an operation of the stack runs.
	hold := make(chan struct{})
	if _, err := m.Start(jobs.Spec{Kind: "stack.up", Title: "Deploy stack app", Resource: "env/local/ns/k8s.io/stack/app"},
		func(context.Context, *jobs.Output) error { <-hold; return nil }); err != nil {
		t.Fatal(err)
	}
	cur := send(t, r, http.MethodGet, base+"/app/files/compose", "")
	_ = json.Unmarshal(cur.Body.Bytes(), &f)
	if rec := put("services: {}\n", f.Version); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Deploy stack app") {
		t.Errorf("save during a job: %d %s", rec.Code, rec.Body)
	}
	close(hold)

	// Create: default content, duplicates, invalid content leaves nothing behind.
	rec = send(t, r, http.MethodPost, base, `{"name":"blog"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "blog", "compose.yaml")); string(b) != stacks.DefaultCompose {
		t.Errorf("created content %q", b)
	}
	if rec := send(t, r, http.MethodPost, base, `{"name":"blog"}`); rec.Code != http.StatusConflict {
		t.Errorf("create twice: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPost, base, `{"name":"bad","compose":"broken"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("create invalid: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad")); !os.IsNotExist(err) {
		t.Errorf("invalid stack left behind: %v", err)
	}
}
