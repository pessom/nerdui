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
	"github.com/pessom/nerdui/internal/secretbox"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/variables"
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

func TestComposeSchema(t *testing.T) {
	rec := send(t, newRouter(&fakeRuntime{}), http.MethodGet, "/api/v1/compose-schema", "")
	var schema struct {
		ID         string         `json:"$id"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v", rec.Code, err)
	}
	if _, ok := schema.Properties["services"]; !ok || rec.Header().Get("Content-Type") != "application/schema+json" {
		t.Errorf("schema %q %v", schema.ID, rec.Header())
	}
}

// TestCreateStackFromTemplate creates a stack with .env and variables as a template deploy
// does (#33), and leaves nothing behind when the content is invalid.
func TestCreateStackFromTemplate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck // test
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), secretbox.FileName))
	if err != nil {
		t.Fatal(err)
	}
	vars := &variables.Service{Store: st.Write(), Box: box}
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(&fakeRuntime{}, "default")), Stacks: &stacks.Service{Dir: dir}, Variables: vars}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const base = "/api/v1/environments/local/stacks"
	body := `{"name":"db","compose":"services:\n  db:\n    image: postgres:${TAG:-17}\n","env":"TAG=16\n",` +
		`"variables":[{"name":"POSTGRES_PASSWORD","value":"pw","secret":true}]}`
	if rec := send(t, r, http.MethodPost, base, body); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "db", ".env")); err != nil || string(b) != "TAG=16\n" {
		t.Errorf(".env %q %v", b, err)
	}
	if got, err := vars.Values(ctx, "db"); err != nil || got["POSTGRES_PASSWORD"] != "pw" {
		t.Errorf("variables %v %v", got, err)
	}

	// Invalid content: the directory, .env and variables are removed.
	body = `{"name":"bad","compose":"broken","env":"A=1\n","variables":[{"name":"S","value":"v","secret":true}]}`
	if rec := send(t, r, http.MethodPost, base, body); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad")); !os.IsNotExist(err) {
		t.Errorf("left behind: %v", err)
	}
	if list, _ := vars.List(ctx, "bad"); len(list) != 0 {
		t.Errorf("variables left: %v", list)
	}
	// A reserved variable name is refused before anything is created.
	body = `{"name":"x","variables":[{"name":"PATH","value":"/"}]}`
	if rec := send(t, r, http.MethodPost, base, body); rec.Code != http.StatusBadRequest {
		t.Errorf("reserved: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); !os.IsNotExist(err) {
		t.Errorf("created: %v", err)
	}
}
