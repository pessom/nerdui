// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gitstacks

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"golang.org/x/crypto/ssh"

	"github.com/pessom/nerdui/internal/gitrepo"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Saved Git credentials (#93): one credential serves any number of stacks.

// CredentialView is a credential without its secrets. Keep in sync with api/openapi.yaml.
type CredentialView struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Username string `json:"username,omitempty"`
	// PublicKey of an SSH key credential, to add as a deploy key.
	PublicKey  string    `json:"publicKey,omitempty"`
	Passphrase bool      `json:"passphrase"`
	Stacks     int64     `json:"stacks"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// CredentialInput creates or replaces a credential. An empty Secret keeps the stored secret
// and passphrase while the kind is unchanged; Generate creates a new ed25519 SSH key. Keep in
// sync with api/openapi.yaml.
type CredentialInput struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
	Generate   bool   `json:"generate"`
}

const maxSecret = 64 << 10

func (s *Service) credentialView(ctx context.Context, r db.GitCredential) (CredentialView, error) {
	n, err := s.Store.CountGitStacksByCredential(ctx, sql.NullInt64{Int64: r.ID, Valid: true})
	if err != nil {
		return CredentialView{}, err
	}
	v := CredentialView{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Username: r.Username, Passphrase: r.Passphrase != "",
		Stacks: n, UpdatedAt: time.UnixMilli(r.UpdatedAt).UTC(),
	}
	if r.Kind == gitrepo.KindSSH {
		v.PublicKey, _ = publicKey(r.Secret, r.Passphrase)
	}
	return v, nil
}

// publicKey returns the authorized_keys line of a PEM private key.
func publicKey(pemKey, passphrase string) (string, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pemKey), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(pemKey))
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " nerdui", nil
}

// generateKey returns a new ed25519 private key in OpenSSH PEM format.
func generateKey() (string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "nerdui")
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(block)), nil
}

// Credentials lists the credentials.
func (s *Service) Credentials(ctx context.Context) ([]CredentialView, error) {
	if s.Store == nil {
		return nil, fmt.Errorf("%w: Git stacks are not available", errdefs.ErrNotImplemented)
	}
	rows, err := s.Store.ListGitCredentials(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CredentialView, 0, len(rows))
	for _, r := range rows {
		v, err := s.credentialView(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// credentialFrom validates the input; stored supplies an empty secret of the same kind.
func credentialFrom(in CredentialInput, stored *db.GitCredential) (db.GitCredential, error) {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", errdefs.ErrInvalidArgument, fmt.Sprintf(format, a...))
	}
	c := db.GitCredential{
		Name: strings.TrimSpace(in.Name), Kind: in.Kind, Username: strings.TrimSpace(in.Username),
		Secret: in.Secret, Passphrase: in.Passphrase,
	}
	if c.Name == "" || len(c.Name) > 100 {
		return c, bad("name must be 1 to 100 characters")
	}
	if len(c.Username) > 200 || len(c.Secret) > maxSecret || len(c.Passphrase) > 1024 {
		return c, bad("username, secret or passphrase is too long")
	}
	switch {
	case in.Generate:
		if c.Kind != gitrepo.KindSSH {
			return c, bad("only SSH keys can be generated")
		}
		key, err := generateKey()
		if err != nil {
			return c, err
		}
		c.Secret, c.Passphrase = key, ""
	case c.Secret == "" && stored != nil && stored.Kind == c.Kind:
		c.Secret, c.Passphrase = stored.Secret, stored.Passphrase
	}
	switch c.Kind {
	case gitrepo.KindToken:
		c.Passphrase = ""
		if c.Secret == "" {
			return c, bad("a token or password is required")
		}
		if strings.ContainsAny(c.Secret, "\r\n") {
			return c, bad("the token must be a single line")
		}
	case gitrepo.KindSSH:
		if c.Secret == "" {
			return c, bad("a private key is required (or generate one)")
		}
		c.Secret = strings.TrimSpace(c.Secret) + "\n"
		if _, err := publicKey(c.Secret, c.Passphrase); err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return c, bad("the private key is encrypted: enter its passphrase")
			}
			return c, bad("invalid private key: %v", err)
		}
	default:
		return c, bad("kind must be %s or %s", gitrepo.KindToken, gitrepo.KindSSH)
	}
	return c, nil
}

// CreateCredential saves a new credential.
func (s *Service) CreateCredential(ctx context.Context, in CredentialInput) (CredentialView, error) {
	c, err := credentialFrom(in, nil)
	if err != nil {
		return CredentialView{}, err
	}
	row, err := s.Store.CreateGitCredential(ctx, db.CreateGitCredentialParams{
		Name: c.Name, Kind: c.Kind, Username: c.Username, Secret: c.Secret, Passphrase: c.Passphrase,
		Now: s.now().UnixMilli(),
	})
	if store.IsUniqueViolation(err) {
		return CredentialView{}, fmt.Errorf("%w: a Git credential named %q exists", errdefs.ErrAlreadyExists, c.Name)
	}
	if err != nil {
		return CredentialView{}, err
	}
	return s.credentialView(ctx, row)
}

func (s *Service) credential(ctx context.Context, id int64) (db.GitCredential, error) {
	row, err := s.Store.GetGitCredential(ctx, id)
	if store.IsNotFound(err) {
		return row, fmt.Errorf("%w: Git credential %d not found", errdefs.ErrNotFound, id)
	}
	return row, err
}

// UpdateCredential replaces a credential; stacks using it pick it up on their next pull.
func (s *Service) UpdateCredential(ctx context.Context, id int64, in CredentialInput) (CredentialView, error) {
	stored, err := s.credential(ctx, id)
	if err != nil {
		return CredentialView{}, err
	}
	c, err := credentialFrom(in, &stored)
	if err != nil {
		return CredentialView{}, err
	}
	row, err := s.Store.UpdateGitCredential(ctx, db.UpdateGitCredentialParams{
		ID: id, Name: c.Name, Kind: c.Kind, Username: c.Username, Secret: c.Secret, Passphrase: c.Passphrase,
		Now: s.now().UnixMilli(),
	})
	if store.IsUniqueViolation(err) {
		return CredentialView{}, fmt.Errorf("%w: a Git credential named %q exists", errdefs.ErrAlreadyExists, c.Name)
	}
	if err != nil {
		return CredentialView{}, err
	}
	return s.credentialView(ctx, row)
}

// DeleteCredential deletes a credential no stack uses.
func (s *Service) DeleteCredential(ctx context.Context, id int64) error {
	if _, err := s.credential(ctx, id); err != nil {
		return err
	}
	n, err := s.Store.CountGitStacksByCredential(ctx, sql.NullInt64{Int64: id, Valid: true})
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %d Git stack(s) use this credential", errdefs.ErrFailedPrecondition, n)
	}
	_, err = s.Store.DeleteGitCredential(ctx, id)
	return err
}

// KnownHost is a trusted SSH host key. Keep in sync with api/openapi.yaml.
type KnownHost struct {
	Host        string    `json:"host"`
	Type        string    `json:"type"`
	Fingerprint string    `json:"fingerprint"`
	AddedAt     time.Time `json:"addedAt"`
}

// KnownHostList lists the trusted SSH host keys.
func (s *Service) KnownHostList(ctx context.Context) ([]KnownHost, error) {
	rows, err := s.Store.ListGitKnownHosts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]KnownHost, 0, len(rows))
	for _, r := range rows {
		h := KnownHost{Host: r.Host, AddedAt: time.UnixMilli(r.CreatedAt).UTC()}
		if k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(r.Key)); err == nil {
			h.Type, h.Fingerprint = k.Type(), ssh.FingerprintSHA256(k)
		}
		out = append(out, h)
	}
	return out, nil
}

// ForgetHost removes the trusted keys of a host: the next connection trusts the key it sees.
func (s *Service) ForgetHost(ctx context.Context, host string) error {
	n, err := s.Store.DeleteGitHostKeys(ctx, host)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: no stored key for %q", errdefs.ErrNotFound, host)
	}
	return nil
}
