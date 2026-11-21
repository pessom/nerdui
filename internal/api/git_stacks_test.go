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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/environment"
	"github.com/pessom/nerdui/internal/gitstacks"
	"github.com/pessom/nerdui/internal/jobs"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
)

func gitSource(t *testing.T) (string, func(path, content, msg string)) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir, func(path, content, msg string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		wt, _ := repo.Worktree()
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "Dev", When: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGitStacksAPI(t *testing.T) {
	src, commit := gitSource(t)
	commit("deploy/compose.yaml", "services: {}\n", "first")

	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	gitDir := filepath.Join(t.TempDir(), "git")
	stackSvc := &stacks.Service{Dir: t.TempDir(), Registry: st.Read(), Git: st.Read(), GitDir: gitDir}
	rt := writable()
	m := jobs.NewManager()
	t.Cleanup(m.Close)
	admin := func(*http.Request) (authz.Subject, bool) { return authz.Subject{Role: authz.RoleAdmin}, true }
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		h := &Handler{
			Envs: environment.NewRegistry(environment.Local(rt, "default")), Jobs: m, Stacks: stackSvc,
			GitStacks: &gitstacks.Service{Dir: gitDir, Store: st.Write(), Stacks: stackSvc, AllowFile: true},
		}
		h.Routes(r, authz.NewGuard(authz.AdminOnly{}, admin, nil))
	})
	wait := func(rec *httptest.ResponseRecorder) jobs.Job {
		t.Helper()
		if rec.Code != http.StatusAccepted {
			t.Fatalf("start: %d %s", rec.Code, rec.Body)
		}
		var resp struct{ Job jobs.Job }
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		deadline := time.Now().Add(5 * time.Second)
		for j, _ := m.Get(resp.Job.ID); ; j, _ = m.Get(resp.Job.ID) {
			if j.Done() {
				return j
			}
			if time.Now().After(deadline) {
				t.Fatalf("job %s not done", resp.Job.ID)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Credentials: secrets are write-only.
	rec := send(t, r, http.MethodPost, "/api/v1/git/credentials", `{"name":"gh","kind":"token","username":"me","secret":"s3cret"}`)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatalf("create credential: %d %s", rec.Code, rec.Body)
	}
	var cred gitstacks.CredentialView
	_ = json.Unmarshal(rec.Body.Bytes(), &cred)
	if rec := send(t, r, http.MethodGet, "/api/v1/git/credentials", ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "s3cret") ||
		!strings.Contains(rec.Body.String(), `"name":"gh"`) {
		t.Errorf("list credentials: %d %s", rec.Code, rec.Body)
	}

	const base = "/api/v1/environments/local/stacks"
	if rec := send(t, r, http.MethodPost, base+"/git", `{"name":"web","url":"ftp://x/y"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad url: %d %s", rec.Code, rec.Body)
	}
	body := `{"name":"web","url":"file://` + src + `","ref":"main","composePath":"deploy","credentialId":` +
		jsonInt(cred.ID) + `,"deploy":true}`
	j := wait(send(t, r, http.MethodPost, base+"/git?namespace=k8s.io", body))
	stackDir := filepath.Join(gitDir, "web", "deploy")
	if j.Status != jobs.StatusSucceeded || j.Result["stack"] != "web" || !slices.Equal(rt.composeRuns, []string{"up web " + stackDir}) {
		t.Fatalf("create job %+v runs %v", j, rt.composeRuns)
	}

	rec = send(t, r, http.MethodGet, base+"/web", "")
	var one stacks.Stack
	_ = json.Unmarshal(rec.Body.Bytes(), &one)
	if rec.Code != http.StatusOK || one.Mode != stacks.ModeGit || one.Git == nil || one.Git.Commit.Message != "first" ||
		one.Git.CredentialID == nil || *one.Git.CredentialID != cred.ID {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, r, http.MethodPut, base+"/web/files/compose", `{"content":"services: {}\n","version":"x"}`); rec.Code == http.StatusOK {
		t.Errorf("compose of a Git stack saved: %s", rec.Body)
	}
	if rec := send(t, r, http.MethodDelete, "/api/v1/git/credentials/"+jsonInt(cred.ID), ""); rec.Code != http.StatusConflict {
		t.Errorf("delete used credential: %d %s", rec.Code, rec.Body)
	}

	// Nothing changed: no redeploy unless forced.
	if j := wait(send(t, r, http.MethodPost, base+"/web/git/pull", `{"redeploy":true}`)); j.Status != jobs.StatusSucceeded || len(rt.composeRuns) != 1 {
		t.Errorf("no-op pull %+v %v", j, rt.composeRuns)
	}
	commit("deploy/compose.yaml", "services: {web: {image: nginx}}\n", "second")
	if j := wait(send(t, r, http.MethodPost, base+"/web/git/pull", `{"redeploy":true,"pull":true}`)); j.Status != jobs.StatusSucceeded ||
		len(rt.composeRuns) != 3 || !strings.HasPrefix(rt.composeRuns[1], "pull web") || !rt.composeOpts[2].RemoveOrphans {
		t.Errorf("pull %+v %v %+v", j, rt.composeRuns, rt.composeOpts)
	}
	if rec := send(t, r, http.MethodPost, base+"/ghost/git/pull", ""); rec.Code != http.StatusNotFound {
		t.Errorf("pull of unknown stack: %d %s", rec.Code, rec.Body)
	}

	// Removing needs the name and no containers.
	if rec := send(t, r, http.MethodDelete, base+"/web/git", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("unconfirmed remove: %d %s", rec.Code, rec.Body)
	}
	rt.containers = []runtime.Container{{ID: "w1", Compose: &runtime.ComposeRef{Project: "web", Service: "web"}}}
	if rec := send(t, r, http.MethodDelete, base+"/web/git?confirm=web", ""); rec.Code != http.StatusConflict {
		t.Errorf("remove with containers: %d %s", rec.Code, rec.Body)
	}
	rt.containers = nil
	if rec := send(t, r, http.MethodDelete, base+"/web/git?confirm=web", ""); rec.Code != http.StatusNoContent {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "web")); !os.IsNotExist(err) {
		t.Error("clone left behind")
	}
	if rec := send(t, r, http.MethodDelete, "/api/v1/git/credentials/"+jsonInt(cred.ID), ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete credential: %d %s", rec.Code, rec.Body)
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }
