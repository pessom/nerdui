// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package gitrepo clones and updates the Git repositories of Git stacks (#37, ADR-0039) with
// go-git: no git binary is needed on the host, and credentials stay in memory.
package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/crypto/ssh"
)

// Credential kinds.
const (
	KindToken = "token" // HTTPS with a username and an access token or password
	KindSSH   = "ssh"   // SSH with a private key
)

// Credential authenticates to a Git server.
type Credential struct {
	Kind     string
	Username string
	// Secret is the token or password, or the PEM private key.
	Secret     string
	Passphrase string
}

// KnownHosts remembers SSH host keys: the first key seen for a host is trusted, later
// connections must present the same key (trust on first use).
type KnownHosts interface {
	HostKeys(ctx context.Context, host string) ([]string, error)
	AddHostKey(ctx context.Context, host, key string) error
}

// Options select a repository and a ref.
type Options struct {
	URL string
	// Ref is a branch or tag name, or a full ref (refs/heads/..., refs/tags/...); empty means
	// the default branch.
	Ref        string
	Credential *Credential
	KnownHosts KnownHosts
	// AllowFile permits file:// and local paths (tests).
	AllowFile bool
}

// Commit describes a checked-out commit. Keep in sync with api/openapi.yaml.
type Commit struct {
	Hash    string    `json:"hash"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}

var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/].*$`)

// ValidateURL accepts https, http, ssh and scp-like (git@host:owner/repo.git) URLs.
func ValidateURL(raw string, allowFile bool) error {
	if scpLike.MatchString(raw) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" && u.Scheme != "file" {
		return fmt.Errorf("%w: %q is not a Git URL (https://, ssh:// or git@host:path)", errdefs.ErrInvalidArgument, raw)
	}
	switch u.Scheme {
	case "https", "http", "ssh":
		if u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				return fmt.Errorf("%w: put the password or token in a Git credential, not in the URL", errdefs.ErrInvalidArgument)
			}
		}
		return nil
	case "file":
		if allowFile {
			return nil
		}
	}
	return fmt.Errorf("%w: unsupported Git URL scheme %q", errdefs.ErrInvalidArgument, u.Scheme)
}

// IsSSH reports whether a URL uses SSH.
func IsSSH(raw string) bool {
	return scpLike.MatchString(raw) || strings.HasPrefix(raw, "ssh://")
}

var refName = regexp.MustCompile(`^[A-Za-z0-9._/+-]{1,200}$`)

// ValidateRef checks a branch or tag name.
func ValidateRef(ref string) error {
	if ref == "" {
		return nil
	}
	if !refName.MatchString(ref) || strings.Contains(ref, "..") || strings.HasPrefix(ref, "-") ||
		strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".lock") {
		return fmt.Errorf("%w: invalid branch or tag %q", errdefs.ErrInvalidArgument, ref)
	}
	return nil
}

func (o Options) auth(ctx context.Context) (transport.AuthMethod, error) {
	c := o.Credential
	if c == nil {
		return nil, nil
	}
	switch c.Kind {
	case KindToken:
		if IsSSH(o.URL) {
			return nil, fmt.Errorf("%w: a token credential needs an https:// URL", errdefs.ErrInvalidArgument)
		}
		user := c.Username
		if user == "" {
			user = "git"
		}
		return &githttp.BasicAuth{Username: user, Password: c.Secret}, nil
	case KindSSH:
		if !IsSSH(o.URL) {
			return nil, fmt.Errorf("%w: an SSH key credential needs an ssh:// or git@host:path URL", errdefs.ErrInvalidArgument)
		}
		user := c.Username
		if user == "" {
			user = "git"
		}
		keys, err := gitssh.NewPublicKeys(user, []byte(c.Secret), c.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("%w: SSH key: %w", errdefs.ErrInvalidArgument, err)
		}
		keys.HostKeyCallback = o.hostKeyCallback(ctx)
		return keys, nil
	}
	return nil, fmt.Errorf("%w: unknown credential kind %q", errdefs.ErrInvalidArgument, c.Kind)
}

// ErrHostKeyChanged is returned when an SSH server presents another key than the one trusted.
var ErrHostKeyChanged = errors.New("the SSH host key changed")

func (o Options) hostKeyCallback(ctx context.Context) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		if o.KnownHosts == nil {
			return errors.New("no SSH known hosts store")
		}
		got := string(ssh.MarshalAuthorizedKey(key))
		got = strings.TrimSpace(got)
		known, err := o.KnownHosts.HostKeys(ctx, hostname)
		if err != nil {
			return err
		}
		for _, k := range known {
			if strings.HasPrefix(k, key.Type()+" ") {
				if k == got {
					return nil
				}
				return fmt.Errorf("%w for %s (now %s): remove the stored key if the change is expected",
					ErrHostKeyChanged, hostname, ssh.FingerprintSHA256(key))
			}
		}
		return o.KnownHosts.AddHostKey(ctx, hostname, got)
	}
}

func commitOf(repo *git.Repository, h plumbing.Hash) (Commit, error) {
	c, err := repo.CommitObject(h)
	if err != nil {
		return Commit{}, err
	}
	msg, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
	return Commit{Hash: c.Hash.String(), Message: msg, Author: c.Author.Name, Date: c.Author.When.UTC()}, nil
}

// clean removes credentials from go-git errors (URLs with user info).
func clean(err error, o Options) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if o.Credential != nil && o.Credential.Secret != "" {
		msg = strings.ReplaceAll(msg, o.Credential.Secret, "<secret>")
	}
	switch {
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed):
		return fmt.Errorf("%w: %s (check the Git credential)", errdefs.ErrFailedPrecondition, msg)
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return fmt.Errorf("%w: %s", errdefs.ErrNotFound, msg)
	}
	return errors.New(msg)
}

// Clone clones the ref of a repository into dir (which must not exist) with depth 1. It returns
// the commit and the full ref name (refs/heads/... or refs/tags/...).
func Clone(ctx context.Context, dir string, o Options) (Commit, string, error) {
	if err := ValidateURL(o.URL, o.AllowFile); err != nil {
		return Commit{}, "", err
	}
	if err := ValidateRef(o.Ref); err != nil {
		return Commit{}, "", err
	}
	auth, err := o.auth(ctx)
	if err != nil {
		return Commit{}, "", err
	}
	try := func(ref plumbing.ReferenceName) (*git.Repository, error) {
		return git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
			URL: o.URL, Auth: auth, ReferenceName: ref, SingleBranch: true, Depth: 1,
			Tags: git.NoTags, RecurseSubmodules: git.NoRecurseSubmodules,
		})
	}
	var refs []plumbing.ReferenceName
	switch {
	case o.Ref == "":
		refs = []plumbing.ReferenceName{""}
	case strings.HasPrefix(o.Ref, "refs/"):
		refs = []plumbing.ReferenceName{plumbing.ReferenceName(o.Ref)}
	default:
		refs = []plumbing.ReferenceName{plumbing.NewBranchReferenceName(o.Ref), plumbing.NewTagReferenceName(o.Ref)}
	}
	var repo *git.Repository
	var used plumbing.ReferenceName
	for _, ref := range refs {
		repo, err = try(ref)
		if err == nil {
			// Some transports "clone" a missing ref into an empty repository.
			if _, herr := repo.Head(); herr != nil {
				repo, err = nil, herr
			}
		}
		if err == nil {
			used = ref
			break
		}
		repo = nil // go-git returns the half-cloned repository along with the error
		removeAll(dir)
		if !isRefNotFound(err) {
			return Commit{}, "", clean(err, o)
		}
	}
	if repo == nil {
		return Commit{}, "", fmt.Errorf("%w: no branch or tag %q in %s", errdefs.ErrNotFound, o.Ref, o.URL)
	}
	head, err := repo.Head()
	if err != nil {
		return Commit{}, "", err
	}
	if used == "" {
		used = head.Name()
	}
	c, err := commitOf(repo, head.Hash())
	return c, used.String(), err
}

func isRefNotFound(err error) bool {
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return true
	}
	var noMatch git.NoMatchingRefSpecError
	if errors.As(err, &noMatch) {
		return true
	}
	return strings.Contains(err.Error(), "couldn't find remote ref") || strings.Contains(err.Error(), "reference not found")
}

// Update fetches the full ref (as returned by Clone) and resets the work tree to it. Files the
// repository does not track (e.g. .env, data of bind mounts) are kept. It returns the commits
// before and after.
func Update(ctx context.Context, dir, fullRef string, o Options) (Commit, Commit, error) {
	if err := ValidateURL(o.URL, o.AllowFile); err != nil {
		return Commit{}, Commit{}, err
	}
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return Commit{}, Commit{}, fmt.Errorf("open repository: %w", err)
	}
	head, err := repo.Head()
	if err != nil {
		return Commit{}, Commit{}, err
	}
	before, err := commitOf(repo, head.Hash())
	if err != nil {
		return Commit{}, Commit{}, err
	}
	auth, err := o.auth(ctx)
	if err != nil {
		return Commit{}, Commit{}, err
	}
	ref := plumbing.ReferenceName(fullRef)
	local := ref
	if ref.IsBranch() {
		local = plumbing.NewRemoteReferenceName("origin", ref.Short())
	}
	// The URL may have changed (e.g. a moved repository).
	if err := repo.DeleteRemote("origin"); err != nil && !errors.Is(err, git.ErrRemoteNotFound) {
		return Commit{}, Commit{}, err
	}
	remote, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{o.URL}})
	if err != nil {
		return Commit{}, Commit{}, err
	}
	err = remote.FetchContext(ctx, &git.FetchOptions{
		RefSpecs: []config.RefSpec{config.RefSpec("+" + ref.String() + ":" + local.String())},
		Auth:     auth, Depth: 1, Force: true, Tags: git.NoTags,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return before, Commit{}, clean(err, o)
	}
	target, err := repo.Reference(local, true)
	if err != nil {
		return before, Commit{}, err
	}
	hash := target.Hash()
	if tag, err := repo.TagObject(hash); err == nil { // annotated tag
		c, err := tag.Commit()
		if err != nil {
			return before, Commit{}, err
		}
		hash = c.Hash
	}
	wt, err := repo.Worktree()
	if err != nil {
		return before, Commit{}, err
	}
	// Limit the reset to tracked files: go-git's hard reset would delete untracked ones.
	files, err := trackedFiles(repo, head.Hash(), hash)
	if err != nil {
		return before, Commit{}, err
	}
	if err := wt.Reset(&git.ResetOptions{Commit: hash, Mode: git.HardReset, Files: files}); err != nil {
		return before, Commit{}, fmt.Errorf("check out %s: %w", hash, err)
	}
	after, err := commitOf(repo, hash)
	return before, after, err
}

// RemoteHead asks the remote for the commit its full ref points to, without fetching objects
// (git ls-remote). For an annotated tag it is the tagged commit.
func RemoteHead(ctx context.Context, fullRef string, o Options) (string, error) {
	if err := ValidateURL(o.URL, o.AllowFile); err != nil {
		return "", err
	}
	auth, err := o.auth(ctx)
	if err != nil {
		return "", err
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{o.URL}})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth, PeelingOption: git.AppendPeeled})
	if err != nil {
		return "", clean(err, o)
	}
	var hash string
	for _, r := range refs {
		switch r.Name().String() {
		case fullRef + "^{}": // the commit of an annotated tag
			return r.Hash().String(), nil
		case fullRef:
			hash = r.Hash().String()
		}
	}
	if hash == "" {
		return "", fmt.Errorf("%w: %s is not in %s any more", errdefs.ErrNotFound, fullRef, o.URL)
	}
	return hash, nil
}

// trackedFiles lists the paths of files in the trees of the given commits.
func trackedFiles(repo *git.Repository, commits ...plumbing.Hash) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, h := range commits {
		c, err := repo.CommitObject(h)
		if err != nil {
			return nil, err
		}
		tree, err := c.Tree()
		if err != nil {
			return nil, err
		}
		err = tree.Files().ForEach(func(f *object.File) error {
			if !seen[f.Name] {
				seen[f.Name] = true
				out = append(out, f.Name)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Head returns the checked-out commit of a clone.
func Head(dir string) (Commit, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return Commit{}, err
	}
	head, err := repo.Head()
	if err != nil {
		return Commit{}, err
	}
	return commitOf(repo, head.Hash())
}

func removeAll(dir string) { _ = os.RemoveAll(dir) }
