// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Sign-in with an OpenID Connect provider (#76, ADR-0056).

// oidcSettingKey stores the provider settings, with the client secret sealed.
const oidcSettingKey = "auth.oidc"

// Session methods.
const (
	MethodPassword = "password"
	MethodOIDC     = "oidc"
)

const (
	oidcStateTTL     = 10 * time.Minute
	oidcMaxPending   = 1000
	oidcProviderTTL  = time.Hour
	oidcDiscoveryTTL = 15 * time.Second
)

var (
	ErrOIDCDisabled   = errors.New("single sign-on is not enabled")
	ErrOIDCState      = errors.New("the sign-in attempt expired or did not start here; try again")
	ErrOIDCNoAccount  = errors.New("there is no nerdui account for you; ask an administrator to create one")
	ErrOIDCNotInGroup = errors.New("your account at the provider is not allowed to sign in to nerdui")
	ErrOIDCLinked     = errors.New("this nerdui account is linked to another account at the provider")
	ErrInvalidOIDC    = errors.New("invalid single sign-on settings")
)

// OIDCSettings configure the provider. The client secret is write-only: ClientSecret is never
// returned, HasClientSecret tells whether one is stored. Keep in sync with api/openapi.yaml.
type OIDCSettings struct {
	Enabled  bool   `json:"enabled"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// ClientSecret replaces the stored secret when set; "" removes it (public clients).
	ClientSecret    *string `json:"clientSecret,omitempty"`
	HasClientSecret bool    `json:"hasClientSecret"`
	// Scopes besides openid; default profile and email, and groups when Group is set.
	Scopes []string `json:"scopes"`
	// UsernameClaim names the nerdui account; default preferred_username.
	UsernameClaim string `json:"usernameClaim"`
	// GroupsClaim lists the user's groups; default groups.
	GroupsClaim string `json:"groupsClaim"`
	// Group, when set, is required to sign in, and its members without an account get one.
	// Without it only existing accounts can sign in.
	Group string `json:"group"`
	// Role is given to accounts created at the first sign-in (ADR-0058); settings saved
	// before roles existed read as admin, new ones default to viewer.
	Role string `json:"role"`
	// ButtonLabel is the text of the sign-in button; default "Sign in with SSO".
	ButtonLabel string `json:"buttonLabel"`
	// RedirectURL overrides the callback URL nerdui derives from the request.
	RedirectURL string `json:"redirectUrl"`
}

type storedOIDC struct {
	OIDCSettings
	SealedSecret string `json:"sealedSecret,omitempty"`
}

func (c OIDCSettings) withDefaults() OIDCSettings {
	if c.UsernameClaim == "" {
		c.UsernameClaim = "preferred_username"
	}
	if c.GroupsClaim == "" {
		c.GroupsClaim = "groups"
	}
	if c.ButtonLabel == "" {
		c.ButtonLabel = "Sign in with SSO"
	}
	return c
}

func (c OIDCSettings) scopes() []string {
	scopes := []string{oidc.ScopeOpenID}
	extra := c.Scopes
	if len(extra) == 0 {
		extra = []string{"profile", "email"}
		if c.Group != "" {
			extra = append(extra, "groups")
		}
	}
	for _, s := range extra {
		if s != "" && !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	return scopes
}

// validate checks the settings that are needed when sign-in is enabled.
func (c OIDCSettings) validate() error {
	if len(c.Issuer) > 512 || len(c.ClientID) > 256 || len(c.Group) > 256 || len(c.ButtonLabel) > 64 ||
		len(c.RedirectURL) > 512 || len(c.Scopes) > 20 || len(c.UsernameClaim) > 64 || len(c.GroupsClaim) > 64 {
		return fmt.Errorf("%w: a value is too long", ErrInvalidOIDC)
	}
	for _, s := range c.Scopes {
		if s == "" || strings.ContainsAny(s, " \t\r\n") {
			return fmt.Errorf("%w: scope %q", ErrInvalidOIDC, s)
		}
	}
	if c.RedirectURL != "" {
		if u, err := url.Parse(c.RedirectURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%w: the redirect URL must be an http(s) URL", ErrInvalidOIDC)
		}
	}
	if !c.Enabled {
		return nil
	}
	if u, err := url.Parse(c.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%w: the issuer must be an http(s) URL", ErrInvalidOIDC)
	}
	if c.ClientID == "" {
		return fmt.Errorf("%w: the client ID is required", ErrInvalidOIDC)
	}
	return nil
}

// oidcState is the provider cache and the sign-ins in progress.
type oidcState struct {
	mu       sync.Mutex
	provider *oidc.Provider
	issuer   string
	fetched  time.Time
	pending  map[string]pendingLogin
}

type pendingLogin struct {
	nonce, verifier, redirect, callback string
	created                             time.Time
}

// OIDCSettings returns the stored settings, without the secret.
func (s *Service) OIDCSettings(ctx context.Context) (OIDCSettings, error) {
	st, err := s.loadOIDC(ctx)
	if err != nil {
		return OIDCSettings{}, err
	}
	c := st.OIDCSettings
	if c.Role == "" {
		c.Role = RoleAdmin
	}
	c.ClientSecret = nil
	c.HasClientSecret = st.SealedSecret != ""
	return c, nil
}

func (s *Service) loadOIDC(ctx context.Context) (storedOIDC, error) {
	v, err := s.store.Read().GetSetting(ctx, oidcSettingKey)
	if store.IsNotFound(err) {
		return storedOIDC{}, nil
	}
	if err != nil {
		return storedOIDC{}, err
	}
	var st storedOIDC
	if err := json.Unmarshal([]byte(v), &st); err != nil {
		return storedOIDC{}, fmt.Errorf("stored single sign-on settings: %w", err)
	}
	st.ClientSecret = nil
	return st, nil
}

// SetOIDCSettings validates and stores the settings. When sign-in is enabled the provider's
// discovery document must be reachable, so a wrong issuer shows at once.
func (s *Service) SetOIDCSettings(ctx context.Context, c OIDCSettings) (OIDCSettings, error) {
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.Group = strings.TrimSpace(c.Group)
	c.RedirectURL = strings.TrimSpace(c.RedirectURL)
	if c.Role = strings.TrimSpace(c.Role); c.Role == "" {
		c.Role = authz.RoleViewer
	}
	if err := c.validate(); err != nil {
		return OIDCSettings{}, err
	}
	cur, err := s.loadOIDC(ctx)
	if err != nil {
		return OIDCSettings{}, err
	}
	next := storedOIDC{OIDCSettings: c, SealedSecret: cur.SealedSecret}
	next.ClientSecret, next.HasClientSecret = nil, false
	if c.ClientSecret != nil {
		next.SealedSecret = ""
		if *c.ClientSecret != "" {
			if s.opts.Box == nil {
				return OIDCSettings{}, fmt.Errorf("%w: no key to encrypt the client secret", ErrInvalidOIDC)
			}
			if next.SealedSecret, err = s.opts.Box.Seal(*c.ClientSecret, oidcSettingKey); err != nil {
				return OIDCSettings{}, err
			}
		}
	}
	if c.Enabled {
		dctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTTL)
		defer cancel()
		if _, err := s.oidcProvider(dctx, c.Issuer, true); err != nil {
			return OIDCSettings{}, fmt.Errorf("%w: cannot read the provider's discovery document: %w", ErrInvalidOIDC, err)
		}
	}
	b, _ := json.Marshal(next)
	if err := s.store.Write().SetSetting(ctx, db.SetSettingParams{Key: oidcSettingKey, Value: string(b), Now: s.opts.Now().Unix()}); err != nil {
		return OIDCSettings{}, err
	}
	return s.OIDCSettings(ctx)
}

// oidcProvider returns the provider of issuer, from its discovery document, cached for an hour.
func (s *Service) oidcProvider(ctx context.Context, issuer string, refresh bool) (*oidc.Provider, error) {
	s.oidc.mu.Lock()
	if !refresh && s.oidc.provider != nil && s.oidc.issuer == issuer && s.opts.Now().Sub(s.oidc.fetched) < oidcProviderTTL {
		p := s.oidc.provider
		s.oidc.mu.Unlock()
		return p, nil
	}
	s.oidc.mu.Unlock()
	p, err := oidc.NewProvider(s.oidcContext(ctx), issuer)
	if err != nil {
		return nil, err
	}
	s.oidc.mu.Lock()
	s.oidc.provider, s.oidc.issuer, s.oidc.fetched = p, issuer, s.opts.Now()
	s.oidc.mu.Unlock()
	return p, nil
}

// oidcContext carries the HTTP client for the provider, when one is configured.
func (s *Service) oidcContext(ctx context.Context) context.Context {
	if s.opts.HTTPClient == nil {
		return ctx
	}
	return oidc.ClientContext(ctx, s.opts.HTTPClient)
}

// oauth2Config builds the OAuth 2.0 client of the settings.
func (s *Service) oauth2Config(st storedOIDC, p *oidc.Provider, callback string) (*oauth2.Config, error) {
	secret := ""
	if st.SealedSecret != "" {
		if s.opts.Box == nil {
			return nil, fmt.Errorf("no key to decrypt the client secret")
		}
		var err error
		if secret, err = s.opts.Box.Open(st.SealedSecret, oidcSettingKey); err != nil {
			return nil, fmt.Errorf("decrypt the client secret: %w", err)
		}
	}
	return &oauth2.Config{
		ClientID: st.ClientID, ClientSecret: secret, Endpoint: p.Endpoint(),
		RedirectURL: callback, Scopes: st.withDefaults().scopes(),
	}, nil
}

// OIDCStart begins a sign-in: it returns the provider's authorization URL and the state, which
// the caller keeps in a cookie. redirect is where to go after signing in; callback is the
// callback URL derived from the request, unless the settings override it.
func (s *Service) OIDCStart(ctx context.Context, redirect, callback string) (authURL, state string, err error) {
	st, err := s.loadOIDC(ctx)
	if err != nil {
		return "", "", err
	}
	if !st.Enabled {
		return "", "", ErrOIDCDisabled
	}
	if st.RedirectURL != "" {
		callback = st.RedirectURL
	}
	p, err := s.oidcProvider(ctx, st.Issuer, false)
	if err != nil {
		return "", "", fmt.Errorf("single sign-on provider: %w", err)
	}
	cfg, err := s.oauth2Config(st, p, callback)
	if err != nil {
		return "", "", err
	}
	state, nonce, verifier := randomToken(24), randomToken(24), oauth2.GenerateVerifier()
	now := s.opts.Now()
	s.oidc.mu.Lock()
	if s.oidc.pending == nil {
		s.oidc.pending = map[string]pendingLogin{}
	}
	for k, v := range s.oidc.pending {
		if now.Sub(v.created) > oidcStateTTL {
			delete(s.oidc.pending, k)
		}
	}
	if len(s.oidc.pending) >= oidcMaxPending {
		s.oidc.mu.Unlock()
		return "", "", &RateLimitedError{RetryAfter: time.Minute}
	}
	s.oidc.pending[state] = pendingLogin{nonce: nonce, verifier: verifier, redirect: redirect, callback: callback, created: now}
	s.oidc.mu.Unlock()
	return cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), state, nil
}

// OIDCIdentity is what the provider says about the user.
type OIDCIdentity struct {
	// ID is "<issuer> <subject>", unique per account at the provider.
	ID       string
	Username string
	Groups   []string
}

// OIDCFinish completes a sign-in: it checks the state against the one kept in the cookie,
// exchanges the code, verifies the ID token and signs the user in. It returns the user, the
// session and where to go next.
func (s *Service) OIDCFinish(ctx context.Context, cookieState, state, code string, c Client) (User, Issued, string, OIDCIdentity, error) {
	if state == "" || subtle.ConstantTimeCompare([]byte(cookieState), []byte(state)) != 1 {
		return User{}, Issued{}, "", OIDCIdentity{}, ErrOIDCState
	}
	s.oidc.mu.Lock()
	pl, ok := s.oidc.pending[state]
	delete(s.oidc.pending, state)
	s.oidc.mu.Unlock()
	if !ok || s.opts.Now().Sub(pl.created) > oidcStateTTL {
		return User{}, Issued{}, "", OIDCIdentity{}, ErrOIDCState
	}
	st, err := s.loadOIDC(ctx)
	if err != nil {
		return User{}, Issued{}, "", OIDCIdentity{}, err
	}
	if !st.Enabled {
		return User{}, Issued{}, pl.redirect, OIDCIdentity{}, ErrOIDCDisabled
	}
	id, err := s.oidcIdentity(ctx, st, pl, code)
	if err != nil {
		return User{}, Issued{}, pl.redirect, OIDCIdentity{}, err
	}
	u, err := s.oidcUser(ctx, st.withDefaults(), id)
	if err != nil {
		return User{}, Issued{}, pl.redirect, id, err
	}
	_, _ = s.store.Write().SetUserLastLogin(ctx, db.SetUserLastLoginParams{
		ID: u.ID, Now: nullInt(s.opts.Now().Unix()),
	})
	issued, err := s.createSession(ctx, u.ID, MethodOIDC, c)
	out := toUser(u)
	out.MustChangePassword = false // as in Authenticate
	return out, issued, pl.redirect, id, err
}

// oidcIdentity exchanges the code and reads the verified claims, completed from the userinfo
// endpoint when the ID token lacks the username or the groups.
func (s *Service) oidcIdentity(ctx context.Context, st storedOIDC, pl pendingLogin, code string) (OIDCIdentity, error) {
	ctx = s.oidcContext(ctx)
	p, err := s.oidcProvider(ctx, st.Issuer, false)
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("single sign-on provider: %w", err)
	}
	cfg, err := s.oauth2Config(st, p, pl.callback)
	if err != nil {
		return OIDCIdentity{}, err
	}
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(pl.verifier))
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("the provider refused the sign-in: %w", err)
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return OIDCIdentity{}, errors.New("the provider returned no ID token")
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: st.ClientID, Now: s.opts.Now}).Verify(ctx, raw)
	if err != nil {
		return OIDCIdentity{}, fmt.Errorf("invalid ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(pl.nonce)) != 1 {
		return OIDCIdentity{}, errors.New("invalid ID token: the nonce does not match")
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		return OIDCIdentity{}, fmt.Errorf("ID token claims: %w", err)
	}
	c := st.withDefaults()
	_, hasUser := claims[c.UsernameClaim]
	_, hasGroups := claims[c.GroupsClaim]
	if !hasUser || (c.Group != "" && !hasGroups) {
		if ui, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
			extra := map[string]any{}
			if ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, ok := claims[k]; !ok {
						claims[k] = v
					}
				}
			}
		}
	}
	id := OIDCIdentity{ID: idt.Issuer + " " + idt.Subject}
	id.Username, _ = claims[c.UsernameClaim].(string)
	switch g := claims[c.GroupsClaim].(type) {
	case []any:
		for _, v := range g {
			if s, ok := v.(string); ok {
				id.Groups = append(id.Groups, s)
			}
		}
	case string:
		id.Groups = []string{g}
	}
	return id, nil
}

// oidcUser finds, links or creates the nerdui account of an identity:
//   - an account linked to it signs in;
//   - otherwise an account whose username is the username claim is linked to it;
//   - otherwise, when Group is set and the user is a member, an account is created.
//
// When Group is set, it is required in every case.
func (s *Service) oidcUser(ctx context.Context, c OIDCSettings, id OIDCIdentity) (db.User, error) {
	if c.Group != "" && !slices.Contains(id.Groups, c.Group) {
		return db.User{}, ErrOIDCNotInGroup
	}
	var u db.User
	err := s.store.Tx(ctx, func(q *db.Queries) error {
		var err error
		u, err = q.GetUserByOIDCIdentity(ctx, nullString(id.ID))
		if err == nil || !store.IsNotFound(err) {
			return err
		}
		if id.Username == "" {
			return fmt.Errorf("%w: the provider sent no %q claim", ErrOIDCNoAccount, c.UsernameClaim)
		}
		u, err = q.GetUserByUsername(ctx, id.Username)
		switch {
		case err == nil:
			if u.OidcIdentity.Valid {
				return ErrOIDCLinked
			}
		case !store.IsNotFound(err):
			return err
		case c.Group == "":
			return ErrOIDCNoAccount
		default:
			if !usernameRe.MatchString(id.Username) {
				return fmt.Errorf("%w: %q is not a valid nerdui username", ErrOIDCNoAccount, id.Username)
			}
			// The account has no usable password: it signs in through the provider.
			hash, err := HashPassword(randomToken(32), s.opts.Argon2)
			if err != nil {
				return err
			}
			if u, err = q.CreateUser(ctx, db.CreateUserParams{
				Username: id.Username, PasswordHash: hash, Role: cmp.Or(c.Role, RoleAdmin), Now: s.opts.Now().Unix(),
			}); err != nil {
				return err
			}
			s.opts.Logger.Info("account created for single sign-on", "username", u.Username, "role", u.Role)
		}
		if _, err := q.SetUserOIDCIdentity(ctx, db.SetUserOIDCIdentityParams{
			ID: u.ID, OidcIdentity: nullString(id.ID), Now: s.opts.Now().Unix(),
		}); err != nil {
			return err
		}
		u.OidcIdentity = nullString(id.ID)
		return nil
	})
	if err != nil {
		return db.User{}, err
	}
	if u.DisabledAt.Valid {
		return db.User{}, ErrUserDisabled
	}
	return u, nil
}

// UnlinkOIDC removes the link of a user to their account at the provider.
func (s *Service) UnlinkOIDC(ctx context.Context, id int64) (UserDetail, error) {
	n, err := s.store.Write().SetUserOIDCIdentity(ctx, db.SetUserOIDCIdentityParams{ID: id, Now: s.opts.Now().Unix()})
	if err != nil {
		return UserDetail{}, err
	}
	if n == 0 {
		return UserDetail{}, ErrUserNotFound
	}
	return s.GetUser(ctx, id)
}
