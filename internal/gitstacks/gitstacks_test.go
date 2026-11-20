// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gitstacks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
)

type source struct {
	t    *testing.T
	dir  string
	repo *git.Repository
}

func newSource(t *testing.T) *source {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &source{t: t, dir: dir, repo: repo}
}

func (s *source) commit(files map[string]string, msg string) plumbing.Hash {
	s.t.Helper()
	wt, err := s.repo.Worktree()
	if err != nil {
		s.t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(s.dir, name)
		if content == "" {
			if _, err := wt.Remove(name); err != nil {
				s.t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			s.t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			s.t.Fatal(err)
		}
	}
	h, err := wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "Dev", When: time.Now()}})
	if err != nil {
		s.t.Fatal(err)
	}
	return h
}

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	gitDir := filepath.Join(t.TempDir(), "git")
	stackSvc := &stacks.Service{Dir: filepath.Join(t.TempDir(), "stacks"), Registry: st.Read(), Git: st.Read(), GitDir: gitDir}
	return &Service{Dir: gitDir, Store: st.Write(), Stacks: stackSvc, AllowFile: true}
}

func TestCreatePullRemove(t *testing.T) {
	ctx := context.Background()
	src := newSource(t)
	first := src.commit(map[string]string{"deploy/compose.yaml": "services: {}\n", "README": "x"}, "first")
	s := newService(t)

	req, err := s.Check(ctx, CreateRequest{Name: "web", URL: "file://" + src.dir, Ref: "main", ComposePath: "./deploy/"})
	if err != nil || req.ComposePath != "deploy" {
		t.Fatalf("check %+v %v", req, err)
	}
	def, err := s.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if def.Mode != stacks.ModeGit || def.File != "compose.yaml" || def.Dir != filepath.Join(s.Dir, "web", "deploy") ||
		def.Git.Commit.Hash != first.String() || def.Git.ComposePath != "deploy/compose.yaml" || def.Git.FullRef != "refs/heads/main" {
		t.Errorf("def %+v %+v", def, def.Git)
	}
	if found, err := s.Stacks.Find(ctx, "web"); err != nil || found.Mode != stacks.ModeGit {
		t.Errorf("find %+v %v", found, err)
	}
	if _, err := s.Check(ctx, CreateRequest{Name: "web", URL: "file://" + src.dir}); !errdefs.IsAlreadyExists(err) {
		t.Errorf("duplicate: %v", err)
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 1 {
		t.Errorf("leftovers in %s: %v", s.Dir, entries)
	}

	// The compose file is read-only; .env is local and survives pulls.
	if _, err := stacks.WriteFile(def, stacks.FileCompose, []byte("services: {}\n"), ""); !errdefs.IsFailedPrecondition(err) {
		t.Errorf("compose write: %v", err)
	}
	if _, err := stacks.WriteFile(def, stacks.FileEnv, []byte("A=1\n"), ""); err != nil {
		t.Fatal(err)
	}
	second := src.commit(map[string]string{"deploy/compose.yaml": "services: {web: {image: nginx}}\n"}, "second")
	before, after, def, err := s.Pull(ctx, "web")
	if err != nil || before.Hash != first.String() || after.Hash != second.String() || def.Git.Commit.Message != "second" {
		t.Fatalf("pull %s -> %s %v", before.Hash, after.Hash, err)
	}
	if f, err := stacks.ReadFile(def, stacks.FileEnv); err != nil || f.Content != "A=1\n" {
		t.Errorf(".env after pull: %+v %v", f, err)
	}

	// A pull that removes the compose file fails and keeps the error.
	src.commit(map[string]string{"deploy/compose.yaml": ""}, "drop")
	if _, _, _, err := s.Pull(ctx, "web"); !errdefs.IsFailedPrecondition(err) {
		t.Errorf("pull without compose file: %v", err)
	}
	if row, _ := s.Get(ctx, "web"); row.LastError == "" || row.CommitHash != second.String() {
		t.Errorf("row after failed pull %+v", row)
	}

	if err := s.Remove(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "web")); !os.IsNotExist(err) {
		t.Error("clone left behind")
	}
	if _, err := s.Stacks.Find(ctx, "web"); !errdefs.IsNotFound(err) {
		t.Errorf("after remove: %v", err)
	}
}

func TestCreateFailures(t *testing.T) {
	ctx := context.Background()
	src := newSource(t)
	src.commit(map[string]string{"app/compose.yaml": "services: {}\n"}, "first")
	s := newService(t)
	url := "file://" + src.dir

	for _, req := range []CreateRequest{
		{Name: "bad name", URL: url},
		{Name: "ab", URL: "ftp://x/y"},
		{Name: "ab", URL: url, Ref: "--upload-pack=x"},
		{Name: "ab", URL: url, ComposePath: "../etc"},
		{Name: "ab", URL: url, ComposePath: ".git/config"},
		{Name: "ab", URL: "git@github.com:o/r.git"}, // SSH needs a key credential
	} {
		if _, err := s.Check(ctx, req); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", req, err)
		}
	}
	missing := int64(42)
	if _, err := s.Check(ctx, CreateRequest{Name: "ab", URL: url, CredentialID: &missing}); !errdefs.IsInvalidArgument(err) {
		t.Errorf("missing credential: %v", err)
	}
	// No compose file at the root: nothing is left behind.
	if _, err := s.Create(ctx, CreateRequest{Name: "ab", URL: url}); !errdefs.IsInvalidArgument(err) || !strings.Contains(err.Error(), "repository root") {
		t.Errorf("root: %v", err)
	}
	if _, err := s.Create(ctx, CreateRequest{Name: "ab", URL: url, Ref: "nope"}); !errdefs.IsNotFound(err) {
		t.Errorf("missing ref: %v", err)
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Errorf("leftovers: %v", entries)
	}
	if rows, _ := s.Store.ListGitStacks(ctx); len(rows) != 0 {
		t.Errorf("rows %v", rows)
	}
}

func TestCredentials(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	tok, err := s.CreateCredential(ctx, CredentialInput{Name: "github", Kind: "token", Username: "me", Secret: "ghp_x"})
	if err != nil || tok.Kind != "token" || tok.PublicKey != "" {
		t.Fatalf("token %+v %v", tok, err)
	}
	if _, err := s.CreateCredential(ctx, CredentialInput{Name: "github", Kind: "token", Secret: "y"}); !errdefs.IsAlreadyExists(err) {
		t.Errorf("duplicate name: %v", err)
	}
	key, err := s.CreateCredential(ctx, CredentialInput{Name: "deploy", Kind: "ssh", Generate: true})
	if err != nil || !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("generated %+v %v", key, err)
	}
	for _, in := range []CredentialInput{
		{Name: "", Kind: "token", Secret: "x"},
		{Name: "x", Kind: "token"},
		{Name: "x", Kind: "ssh", Secret: "not a key"},
		{Name: "x", Kind: "token", Generate: true},
		{Name: "x", Kind: "basic", Secret: "x"},
	} {
		if _, err := s.CreateCredential(ctx, in); !errdefs.IsInvalidArgument(err) {
			t.Errorf("%+v: %v", in, err)
		}
	}

	// An empty secret keeps the stored one; the public key stays the same.
	renamed, err := s.UpdateCredential(ctx, key.ID, CredentialInput{Name: "deploy-key", Kind: "ssh"})
	if err != nil || renamed.PublicKey != key.PublicKey || renamed.Name != "deploy-key" {
		t.Errorf("rename %+v %v", renamed, err)
	}
	stored, _ := s.Store.GetGitCredential(ctx, tok.ID)
	if _, err := s.UpdateCredential(ctx, tok.ID, CredentialInput{Name: "github", Kind: "token", Username: "me"}); err != nil {
		t.Error(err)
	}
	if again, _ := s.Store.GetGitCredential(ctx, tok.ID); again.Secret != stored.Secret {
		t.Error("secret not kept")
	}
	// Switching the kind needs a new secret.
	if _, err := s.UpdateCredential(ctx, tok.ID, CredentialInput{Name: "github", Kind: "ssh"}); !errdefs.IsInvalidArgument(err) {
		t.Errorf("kind switch: %v", err)
	}

	// A credential in use cannot be deleted.
	src := newSource(t)
	src.commit(map[string]string{"compose.yaml": "services: {}\n"}, "first")
	id := tok.ID
	if _, err := s.Create(ctx, CreateRequest{Name: "app", URL: "file://" + src.dir, CredentialID: &id}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Credentials(ctx)
	if err != nil || len(list) != 2 || list[1].Stacks != 1 {
		t.Errorf("list %+v %v", list, err)
	}
	if err := s.DeleteCredential(ctx, tok.ID); !errdefs.IsFailedPrecondition(err) {
		t.Errorf("delete in use: %v", err)
	}
	if err := s.DeleteCredential(ctx, key.ID); err != nil {
		t.Error(err)
	}
	if err := s.DeleteCredential(ctx, key.ID); !errdefs.IsNotFound(err) {
		t.Errorf("delete twice: %v", err)
	}
}

func TestKnownHosts(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	k := KnownHosts{Store: s.Store}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	if err := k.AddHostKey(ctx, "github.com:22", key); err != nil {
		t.Fatal(err)
	}
	if err := k.AddHostKey(ctx, "github.com:22", key); err != nil {
		t.Fatal(err)
	}
	list, err := s.KnownHostList(ctx)
	if err != nil || len(list) != 1 || list[0].Type != "ssh-ed25519" || !strings.HasPrefix(list[0].Fingerprint, "SHA256:") {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := s.ForgetHost(ctx, "github.com:22"); err != nil {
		t.Error(err)
	}
	if err := s.ForgetHost(ctx, "github.com:22"); !errdefs.IsNotFound(err) {
		t.Errorf("forget twice: %v", err)
	}
}
