// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/stacks"
)

// TestStackTreeAPI browses and edits the files of a stack's directory (#27).
func TestStackTreeAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app", "conf"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"compose.yaml": "services:\n  web: {image: nginx}\n", "conf/site.conf": "server {}\n"} {
		if err := os.WriteFile(filepath.Join(dir, "app", name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{Envs: environment.NewRegistry(environment.Local(&fakeRuntime{}, "default")), Jobs: m, Stacks: &stacks.Service{Dir: dir}}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	const base = "/api/v1/environments/local/stacks/app/tree"
	file := func(p string) string { return base + "/file?path=" + url.QueryEscape(p) }

	rec := send(t, r, http.MethodGet, base, "")
	var tree stacks.Tree
	_ = json.Unmarshal(rec.Body.Bytes(), &tree)
	if rec.Code != http.StatusOK || len(tree.Entries) != 3 || tree.Entries[0].Path != "conf" || !tree.Entries[2].Protected {
		t.Fatalf("tree: %d %s", rec.Code, rec.Body)
	}
	rec = send(t, r, http.MethodGet, file("conf/site.conf"), "")
	var f stacks.PathFile
	_ = json.Unmarshal(rec.Body.Bytes(), &f)
	if rec.Code != http.StatusOK || f.Content != "server {}\n" {
		t.Fatalf("read: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodGet, file("../../etc/passwd"), ""); rec.Code != http.StatusBadRequest {
		t.Errorf("escape: %d %s", rec.Code, rec.Body)
	}

	put := func(p, content, version string) (int, StackPathSaved) {
		b, _ := json.Marshal(map[string]string{"content": content, "version": version})
		rec := send(t, r, http.MethodPut, file(p), string(b))
		var out StackPathSaved
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	// A config file: saved, the project still loads.
	if code, out := put("conf/site.conf", "server { listen 80; }\n", f.Version); code != http.StatusOK || !out.Validation.Valid || out.File.Version == f.Version {
		t.Errorf("save config: %d %+v", code, out)
	}
	// An override file is saved, then the project is checked with it (the fake runtime does not
	// read override files; nerdctl's loader does).
	code, out := put("compose.override.yaml", "services:\n  web: {restart: always}\n", "")
	if code != http.StatusOK || !out.File.Exists || out.Validation.Services == nil {
		t.Errorf("save override: %d %+v", code, out)
	}
	// The compose file is checked before it is saved, with a backup.
	rec = send(t, r, http.MethodDelete, file("compose.override.yaml")+"&version="+out.File.Version, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete override: %d %s", rec.Code, rec.Body)
	}
	cur, _ := stacks.ReadPath(stacks.Definition{Dir: filepath.Join(dir, "app")}, "compose.yaml")
	if code, _ := put("compose.yaml", "services:\n  broken", cur.Version); code != http.StatusBadRequest {
		t.Errorf("save invalid compose: %d", code)
	}
	if code, out := put("compose.yaml", "services:\n  web: {image: nginx:1.27}\n", cur.Version); code != http.StatusOK || !out.Validation.Valid {
		t.Errorf("save compose: %d %+v", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "compose.yaml.bak")); err != nil {
		t.Errorf("backup: %v", err)
	}
	if rec := send(t, r, http.MethodDelete, file("compose.yaml"), ""); rec.Code != http.StatusConflict {
		t.Errorf("delete compose: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodGet, "/api/v1/environments/local/stacks/ghost/tree", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown stack: %d", rec.Code)
	}
}
