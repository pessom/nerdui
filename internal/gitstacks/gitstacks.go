// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package gitstacks deploys stacks from Git repositories (#37) with saved credentials (#93,
// ADR-0039). A Git stack is cloned into <data-dir>/git/<name>; its compose file is read-only and
// changes by pulling, while .env and other untracked files stay local.
package gitstacks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/identifiers"

	"github.com/pessom/nerdui/internal/gitrepo"
	"github.com/pessom/nerdui/internal/stacks"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Store keeps Git stacks, credentials and known hosts; *db.Queries implements it.
type Store interface {
	ListGitStacks(ctx context.Context) ([]db.GitStack, error)
	GetGitStack(ctx context.Context, name string) (db.GitStack, error)
	CreateGitStack(ctx context.Context, arg db.CreateGitStackParams) error
	SetGitStackCommit(ctx context.Context, arg db.SetGitStackCommitParams) error
	SetGitStackError(ctx context.Context, arg db.SetGitStackErrorParams) error
	DeleteGitStack(ctx context.Context, name string) (int64, error)
	SetGitStackAuto(ctx context.Context, arg db.SetGitStackAutoParams) (int64, error)
	SetGitStackWebhook(ctx context.Context, arg db.SetGitStackWebhookParams) (int64, error)
	GetGitStackByWebhook(ctx context.Context, webhookHash string) (db.GitStack, error)
	SetGitStackChecked(ctx context.Context, arg db.SetGitStackCheckedParams) error

	ListGitCredentials(ctx context.Context) ([]db.GitCredential, error)
	GetGitCredential(ctx context.Context, id int64) (db.GitCredential, error)
	CreateGitCredential(ctx context.Context, arg db.CreateGitCredentialParams) (db.GitCredential, error)
	UpdateGitCredential(ctx context.Context, arg db.UpdateGitCredentialParams) (db.GitCredential, error)
	DeleteGitCredential(ctx context.Context, id int64) (int64, error)
	CountGitStacksByCredential(ctx context.Context, credentialID sql.NullInt64) (int64, error)

	ListGitKnownHosts(ctx context.Context) ([]db.GitKnownHost, error)
	ListGitHostKeys(ctx context.Context, host string) ([]string, error)
	AddGitHostKey(ctx context.Context, arg db.AddGitHostKeyParams) error
	DeleteGitHostKeys(ctx context.Context, host string) (int64, error)
}

// Service manages Git stacks.
type Service struct {
	// Dir holds the clones (<data-dir>/git); it is also stacks.Service.GitDir.
	Dir    string
	Store  Store
	Stacks *stacks.Service
	// AllowFile permits file:// URLs (tests).
	AllowFile bool
	Now       func() time.Time

	locks sync.Map // stack name → *sync.Mutex: one pull of a clone at a time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// CreateRequest creates a Git stack. Keep in sync with api/openapi.yaml.
type CreateRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Ref is a branch or tag; empty means the default branch.
	Ref string `json:"ref"`
	// ComposePath is the compose file or its directory, relative to the repository root;
	// empty means the root directory.
	ComposePath  string `json:"composePath"`
	CredentialID *int64 `json:"credentialId,omitempty"`
}

// cleanComposePath normalizes a repository-relative path ("" for the root).
func cleanComposePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || p == "." || p == "/" {
		return "", nil
	}
	c := path.Clean(strings.TrimPrefix(p, "./"))
	if path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") || strings.HasPrefix(c, ".git/") || c == ".git" {
		return "", fmt.Errorf("%w: compose path %q must be inside the repository", errdefs.ErrInvalidArgument, p)
	}
	return c, nil
}

// Check validates a request before cloning starts: the name is free, the URL, ref and path are
// valid and the credential exists and suits the URL. It returns the normalized request.
func (s *Service) Check(ctx context.Context, req CreateRequest) (CreateRequest, error) {
	if s.Dir == "" || s.Store == nil || s.Stacks == nil {
		return req, fmt.Errorf("%w: Git stacks are not available", errdefs.ErrNotImplemented)
	}
	req.URL, req.Ref = strings.TrimSpace(req.URL), strings.TrimSpace(req.Ref)
	if err := identifiers.ValidateDockerCompat(req.Name); err != nil {
		return req, fmt.Errorf("%w: stack name: %w", errdefs.ErrInvalidArgument, err)
	}
	if err := gitrepo.ValidateURL(req.URL, s.AllowFile); err != nil {
		return req, err
	}
	if err := gitrepo.ValidateRef(req.Ref); err != nil {
		return req, err
	}
	var err error
	if req.ComposePath, err = cleanComposePath(req.ComposePath); err != nil {
		return req, err
	}
	if _, err := s.options(ctx, req.URL, req.Ref, req.CredentialID); err != nil {
		return req, err
	}
	if _, err := s.Stacks.Find(ctx, req.Name); err == nil {
		return req, fmt.Errorf("%w: stack %q exists already", errdefs.ErrAlreadyExists, req.Name)
	} else if !errdefs.IsNotFound(err) {
		return req, err
	}
	return req, nil
}

// options builds the clone options with the credential of id; SSH needs a key credential, since
// host keys are checked against the stored ones.
func (s *Service) options(ctx context.Context, url, ref string, credentialID *int64) (gitrepo.Options, error) {
	o := gitrepo.Options{URL: url, Ref: ref, KnownHosts: KnownHosts{Store: s.Store, Now: s.now}, AllowFile: s.AllowFile}
	if credentialID != nil {
		row, err := s.Store.GetGitCredential(ctx, *credentialID)
		if store.IsNotFound(err) {
			return o, fmt.Errorf("%w: Git credential %d not found", errdefs.ErrInvalidArgument, *credentialID)
		}
		if err != nil {
			return o, err
		}
		o.Credential = &gitrepo.Credential{Kind: row.Kind, Username: row.Username, Secret: row.Secret, Passphrase: row.Passphrase}
	}
	if gitrepo.IsSSH(url) && (o.Credential == nil || o.Credential.Kind != gitrepo.KindSSH) {
		return o, fmt.Errorf("%w: an ssh:// or git@host:path URL needs an SSH key credential", errdefs.ErrInvalidArgument)
	}
	if o.Credential != nil && o.Credential.Kind == gitrepo.KindToken && gitrepo.IsSSH(url) {
		return o, fmt.Errorf("%w: a token credential needs an https:// URL", errdefs.ErrInvalidArgument)
	}
	return o, nil
}

// composeIn finds the compose file of a clone: rel is a file or a directory (the root when
// empty). It returns the file's repository-relative path. The file must be a regular file:
// a symlink could point outside the clone.
func composeIn(clone, rel string) (string, error) {
	p := filepath.Clean(clone)
	if rel != "" {
		var err error
		if p, err = inside(clone, filepath.FromSlash(rel)); err != nil {
			return "", err
		}
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return "", fmt.Errorf("%w: %q is not in the repository", errdefs.ErrInvalidArgument, rel)
	}
	if fi.IsDir() {
		name := stacks.ComposeFile(p)
		if name == "" {
			where := rel
			if where == "" {
				where = "the repository root"
			}
			return "", fmt.Errorf("%w: no compose file (%s) in %s", errdefs.ErrInvalidArgument,
				strings.Join(stacks.ComposeFileNames, ", "), where)
		}
		if fi, err := os.Lstat(filepath.Join(p, name)); err != nil || !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%w: %s must be a regular file", errdefs.ErrInvalidArgument, path.Join(rel, name))
		}
		return path.Join(rel, name), nil
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s must be a regular file, not a symlink", errdefs.ErrInvalidArgument, rel)
	}
	return rel, nil
}

// inside returns base/rel, cleaned, when it is below base (defense in depth: names and paths
// are validated before).
func inside(base, rel string) (string, error) {
	base = filepath.Clean(base)
	p := filepath.Clean(filepath.Join(base, rel))
	if !strings.HasPrefix(p, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q is outside %s", errdefs.ErrInvalidArgument, rel, base)
	}
	return p, nil
}

// clonePath returns <Dir>/<name>.
func (s *Service) clonePath(name string) (string, error) {
	if err := identifiers.ValidateDockerCompat(name); err != nil {
		return "", fmt.Errorf("%w: stack name: %w", errdefs.ErrInvalidArgument, err)
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("%w: stack name %q", errdefs.ErrInvalidArgument, name)
	}
	return inside(s.Dir, name)
}

// Create clones the repository and registers the stack. The request must have passed Check.
// The clone happens in a temporary directory renamed at the end: nothing is left on failure.
func (s *Service) Create(ctx context.Context, req CreateRequest) (stacks.Definition, error) {
	o, err := s.options(ctx, req.URL, req.Ref, req.CredentialID)
	if err != nil {
		return stacks.Definition{}, err
	}
	target, err := s.clonePath(req.Name)
	if err != nil {
		return stacks.Definition{}, err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return stacks.Definition{}, fmt.Errorf("create %s: %w", s.Dir, err)
	}
	if _, err := os.Lstat(target); err == nil {
		return stacks.Definition{}, fmt.Errorf("%w: %s exists already", errdefs.ErrAlreadyExists, target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return stacks.Definition{}, err
	}
	tmp, err := os.MkdirTemp(s.Dir, ".clone-")
	if err != nil {
		return stacks.Definition{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }() // a no-op once renamed
	work := filepath.Join(tmp, "repo")
	commit, fullRef, err := gitrepo.Clone(ctx, work, o)
	if err != nil {
		return stacks.Definition{}, err
	}
	composePath, err := composeIn(work, req.ComposePath)
	if err != nil {
		return stacks.Definition{}, err
	}
	if err := os.Rename(work, target); err != nil {
		return stacks.Definition{}, fmt.Errorf("move into %s: %w", target, err)
	}
	now := s.now().UnixMilli()
	row := db.CreateGitStackParams{
		Name: req.Name, Url: req.URL, Ref: req.Ref, FullRef: fullRef, ComposePath: composePath,
		CommitHash: commit.Hash, CommitMessage: commit.Message, CommitAuthor: commit.Author,
		CommitAt: commit.Date.UnixMilli(), Now: now,
	}
	if req.CredentialID != nil {
		row.CredentialID = sql.NullInt64{Int64: *req.CredentialID, Valid: true}
	}
	if err := s.Store.CreateGitStack(ctx, row); err != nil {
		_ = os.RemoveAll(target)
		if store.IsUniqueViolation(err) {
			return stacks.Definition{}, fmt.Errorf("%w: stack %q exists already", errdefs.ErrAlreadyExists, req.Name)
		}
		return stacks.Definition{}, fmt.Errorf("register Git stack: %w", err)
	}
	stored, err := s.Store.GetGitStack(ctx, req.Name)
	if err != nil {
		return stacks.Definition{}, err
	}
	return stacks.GitDefinition(s.Dir, stored), nil
}

// Get returns a Git stack.
func (s *Service) Get(ctx context.Context, name string) (db.GitStack, error) {
	if s.Store == nil {
		return db.GitStack{}, fmt.Errorf("%w: Git stacks are not available", errdefs.ErrNotImplemented)
	}
	row, err := s.Store.GetGitStack(ctx, name)
	if store.IsNotFound(err) {
		return row, fmt.Errorf("%w: %q is not a Git stack", errdefs.ErrNotFound, name)
	}
	return row, err
}

// Pull fetches the ref and checks out its newest commit, keeping untracked files. A failure
// is recorded on the stack; the checked-out commit stays.
func (s *Service) Pull(ctx context.Context, name string) (before, after gitrepo.Commit, def stacks.Definition, err error) {
	mu, _ := s.locks.LoadOrStore(name, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	row, err := s.Get(ctx, name)
	if err != nil {
		return before, after, def, err
	}
	defer func() {
		if err != nil && ctx.Err() == nil {
			_ = s.Store.SetGitStackError(context.WithoutCancel(ctx), db.SetGitStackErrorParams{Name: name, Error: err.Error()})
		}
	}()
	var cred *int64
	if row.CredentialID.Valid {
		cred = &row.CredentialID.Int64
	}
	o, err := s.options(ctx, row.Url, row.Ref, cred)
	if err != nil {
		return before, after, def, err
	}
	clone, err := s.clonePath(name)
	if err != nil {
		return before, after, def, err
	}
	before, after, err = gitrepo.Update(ctx, clone, row.FullRef, o)
	if err != nil {
		return before, after, def, err
	}
	if _, err := composeIn(clone, row.ComposePath); err != nil {
		return before, after, def, fmt.Errorf("%w: after pulling %s: %w", errdefs.ErrFailedPrecondition, Short(after.Hash), err)
	}
	if err := s.Store.SetGitStackCommit(ctx, db.SetGitStackCommitParams{
		Name: name, CommitHash: after.Hash, CommitMessage: after.Message, CommitAuthor: after.Author,
		CommitAt: after.Date.UnixMilli(), Now: s.now().UnixMilli(),
	}); err != nil {
		return before, after, def, err
	}
	row, err = s.Store.GetGitStack(ctx, name)
	if err != nil {
		return before, after, def, err
	}
	return before, after, stacks.GitDefinition(s.Dir, row), nil
}

// Remove forgets a Git stack and deletes its clone, .env and other local files included.
func (s *Service) Remove(ctx context.Context, name string) error {
	if _, err := s.Get(ctx, name); err != nil {
		return err
	}
	clone, err := s.clonePath(name)
	if err != nil {
		return err
	}
	if _, err := s.Store.DeleteGitStack(ctx, name); err != nil {
		return fmt.Errorf("forget Git stack: %w", err)
	}
	if err := os.RemoveAll(clone); err != nil {
		return fmt.Errorf("remove %s: %w", clone, err)
	}
	return nil
}

// Short abbreviates a commit hash.
func Short(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

// KnownHosts stores trusted SSH host keys in the database.
type KnownHosts struct {
	Store Store
	Now   func() time.Time
}

// HostKeys returns the keys trusted for a host.
func (k KnownHosts) HostKeys(ctx context.Context, host string) ([]string, error) {
	return k.Store.ListGitHostKeys(ctx, host)
}

// AddHostKey trusts a key for a host.
func (k KnownHosts) AddHostKey(ctx context.Context, host, key string) error {
	now := time.Now
	if k.Now != nil {
		now = k.Now
	}
	return k.Store.AddGitHostKey(ctx, db.AddGitHostKeyParams{Host: host, Key: key, Now: now().UnixMilli()})
}
