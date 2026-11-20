// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gitrepo

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"golang.org/x/crypto/ssh"
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
	h, err := wt.Commit(msg+"\n\nbody", &git.CommitOptions{
		Author: &object.Signature{Name: "Dev", Email: "dev@example.com", When: time.Now()},
	})
	if err != nil {
		s.t.Fatal(err)
	}
	return h
}

func (s *source) url() string { return "file://" + s.dir }

func TestCloneAndUpdate(t *testing.T) {
	ctx := context.Background()
	src := newSource(t)
	first := src.commit(map[string]string{"deploy/compose.yaml": "services: {}\n"}, "first")
	if _, err := src.repo.CreateTag("v1", first, &git.CreateTagOptions{
		Tagger: &object.Signature{Name: "Dev", When: time.Now()}, Message: "release",
	}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "clone")
	c, ref, err := Clone(ctx, dir, Options{URL: src.url(), Ref: "main", AllowFile: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Hash != first.String() || c.Message != "first" || c.Author != "Dev" || ref != "refs/heads/main" {
		t.Errorf("clone %+v %s", c, ref)
	}
	if _, err := os.Stat(filepath.Join(dir, "deploy", "compose.yaml")); err != nil {
		t.Error(err)
	}

	// Untracked files (.env, bind-mount data) survive updates.
	if err := os.WriteFile(filepath.Join(dir, "deploy", ".env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := src.commit(map[string]string{"deploy/compose.yaml": "services: {web: {image: nginx}}\n"}, "second")
	before, after, err := Update(ctx, dir, ref, Options{URL: src.url(), AllowFile: true})
	if err != nil {
		t.Fatal(err)
	}
	if before.Hash != first.String() || after.Hash != second.String() {
		t.Errorf("update %s -> %s", before.Hash, after.Hash)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "deploy", "compose.yaml")); string(b) != "services: {web: {image: nginx}}\n" {
		t.Errorf("compose %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "deploy", ".env")); err != nil {
		t.Error(".env removed by update")
	}
	if _, again, err := Update(ctx, dir, ref, Options{URL: src.url(), AllowFile: true}); err != nil || again.Hash != second.String() {
		t.Errorf("no-op update: %v %s", err, again.Hash)
	}
	if h, err := Head(dir); err != nil || h.Hash != second.String() {
		t.Errorf("head %v %v", h, err)
	}

	// An annotated tag.
	tagDir := filepath.Join(t.TempDir(), "tag")
	c, ref, err = Clone(ctx, tagDir, Options{URL: src.url(), Ref: "v1", AllowFile: true})
	if err != nil || c.Hash != first.String() || ref != "refs/tags/v1" {
		t.Fatalf("tag clone %+v %s %v", c, ref, err)
	}
	if _, after, err := Update(ctx, tagDir, ref, Options{URL: src.url(), AllowFile: true}); err != nil || after.Hash != first.String() {
		t.Errorf("tag update %v %v", after, err)
	}

	// The default branch.
	c, ref, err = Clone(ctx, filepath.Join(t.TempDir(), "default"), Options{URL: src.url(), AllowFile: true})
	if err != nil || ref != "refs/heads/main" || c.Hash != second.String() {
		t.Errorf("default %+v %s %v", c, ref, err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if _, _, err := Clone(ctx, missing, Options{URL: src.url(), Ref: "nope", AllowFile: true}); !errdefs.IsNotFound(err) {
		t.Errorf("missing ref: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("failed clone left a directory")
	}
}

func TestValidate(t *testing.T) {
	for _, u := range []string{"https://github.com/o/r.git", "ssh://git@host:2222/o/r", "git@github.com:o/r.git", "http://gitea.lan/o/r"} {
		if err := ValidateURL(u, false); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range []string{"file:///etc", "/srv/repo", "ftp://x/y", "https://user:secret@github.com/o/r", "github.com/o/r"} {
		if ValidateURL(u, false) == nil {
			t.Errorf("%s accepted", u)
		}
	}
	for _, r := range []string{"--upload-pack=x", "a..b", "main/", "x.lock", "a b"} {
		if ValidateRef(r) == nil {
			t.Errorf("ref %q accepted", r)
		}
	}
	if (Options{URL: "git@github.com:o/r.git", Credential: &Credential{Kind: KindToken, Secret: "t"}}).validAuth() == nil {
		t.Error("token over SSH accepted")
	}
}

func (o Options) validAuth() error { _, err := o.auth(context.Background()); return err }

type memHosts map[string][]string

func (m memHosts) HostKeys(_ context.Context, host string) ([]string, error) { return m[host], nil }
func (m memHosts) AddHostKey(_ context.Context, host, key string) error {
	m[host] = append(m[host], key)
	return nil
}

func TestHostKeyTrustOnFirstUse(t *testing.T) {
	pub := func() ssh.PublicKey {
		k, _, _ := ed25519.GenerateKey(rand.Reader)
		p, err := ssh.NewPublicKey(k)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	hosts := memHosts{}
	cb := Options{KnownHosts: hosts}.hostKeyCallback(context.Background())
	k1, k2 := pub(), pub()
	if err := cb("github.com:22", nil, k1); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := cb("github.com:22", nil, k1); err != nil {
		t.Errorf("same key: %v", err)
	}
	if err := cb("github.com:22", nil, k2); !errors.Is(err, ErrHostKeyChanged) {
		t.Errorf("changed key: %v", err)
	}
	if len(hosts["github.com:22"]) != 1 {
		t.Errorf("stored %v", hosts)
	}
}

func TestRemoteHead(t *testing.T) {
	ctx := context.Background()
	src := newSource(t)
	first := src.commit(map[string]string{"a": "1"}, "first")
	o := Options{URL: src.url(), AllowFile: true}
	if h, err := RemoteHead(ctx, "refs/heads/main", o); err != nil || h != first.String() {
		t.Fatalf("branch: %s %v", h, err)
	}
	second := src.commit(map[string]string{"a": "2"}, "second")
	if h, _ := RemoteHead(ctx, "refs/heads/main", o); h != second.String() {
		t.Errorf("after commit: %s", h)
	}
	if _, err := src.repo.CreateTag("v1", first, &git.CreateTagOptions{
		Message: "release", Tagger: &object.Signature{Name: "Dev", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	if h, err := RemoteHead(ctx, "refs/tags/v1", o); err != nil || h != first.String() {
		t.Errorf("annotated tag: %s %v", h, err)
	}
	if _, err := RemoteHead(ctx, "refs/heads/gone", o); !errdefs.IsNotFound(err) {
		t.Errorf("missing ref: %v", err)
	}
}
