// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Passkeys (WebAuthn) as a second factor and for sign-in without a password (#226, ADR-0057).

var (
	ErrPasskeyHost     = errors.New("passkeys need nerdui opened by a host name over HTTPS (or localhost); browsers refuse them on IP addresses")
	ErrPasskeyFailed   = errors.New("the passkey was not accepted")
	ErrPasskeyCeremony = errors.New("the passkey request expired; try again")
	ErrPasskeyNotFound = errors.New("passkey not found")
	ErrPasskeyLimit    = fmt.Errorf("a user can have at most %d passkeys", maxPasskeys)
	ErrPasskeyName     = errors.New("passkey name must be at most 64 characters")
)

const (
	// MethodPasskey: the session signed in with a passkey alone (with user verification).
	MethodPasskey = "passkey"

	maxPasskeys     = 20
	ceremonyTTL     = 5 * time.Minute
	maxCeremonies   = 1000
	passkeyNameMax  = 64
	defaultPassName = "Passkey"
)

// relyingParty is where the browser runs WebAuthn: the host name nerdui was opened by.
type relyingParty struct {
	ID     string // host name, without the port
	Origin string // scheme://host[:port]
}

// newRelyingParty checks that browsers allow passkeys there: a host name (not an IP address)
// in a secure context, which is HTTPS or localhost.
func newRelyingParty(host string, secure bool) (relyingParty, error) {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	localhost := name == "localhost" || strings.HasSuffix(name, ".localhost")
	if name == "" || net.ParseIP(strings.Trim(name, "[]")) != nil || (!secure && !localhost) {
		return relyingParty{}, ErrPasskeyHost
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return relyingParty{ID: name, Origin: scheme + "://" + strings.ToLower(host)}, nil
}

func (rp relyingParty) webAuthn() (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID: rp.ID, RPDisplayName: "nerdui", RPOrigins: []string{rp.Origin},
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

// ceremony is a registration or sign-in waiting for the browser's answer.
type ceremony struct {
	rp      relyingParty
	session webauthn.SessionData
	userID  int64 // 0 for sign-in without a password
	expires time.Time
}

type ceremonies struct {
	mu sync.Mutex
	m  map[string]*ceremony // by token hash
}

func (c *ceremonies) add(ce *ceremony, now time.Time) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*ceremony{}
	}
	for k, v := range c.m {
		if !now.Before(v.expires) {
			delete(c.m, k)
		}
	}
	if len(c.m) >= maxCeremonies {
		return "", &RateLimitedError{RetryAfter: ceremonyTTL}
	}
	token := randomToken(32)
	ce.expires = now.Add(ceremonyTTL)
	c.m[string(hashToken(token))] = ce
	return token, nil
}

// take returns a live ceremony once.
func (c *ceremonies) take(token string, now time.Time) (*ceremony, bool) {
	key := string(hashToken(token))
	c.mu.Lock()
	defer c.mu.Unlock()
	ce, ok := c.m[key]
	delete(c.m, key)
	if !ok || !now.Before(ce.expires) {
		return nil, false
	}
	return ce, true
}

// webAuthnUser adapts a user and their passkeys for one relying party to the library.
type webAuthnUser struct {
	u     db.User
	keys  []db.Passkey
	creds []webauthn.Credential
}

func (w *webAuthnUser) WebAuthnID() []byte                         { return w.u.WebauthnHandle }
func (w *webAuthnUser) WebAuthnName() string                       { return w.u.Username }
func (w *webAuthnUser) WebAuthnDisplayName() string                { return w.u.Username }
func (w *webAuthnUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (w *webAuthnUser) descriptors() []protocol.CredentialDescriptor {
	d := make([]protocol.CredentialDescriptor, len(w.creds))
	for i := range w.creds {
		d[i] = w.creds[i].Descriptor()
	}
	return d
}

// passkeyOf returns the stored passkey of a credential.
func (w *webAuthnUser) passkeyOf(id []byte) (db.Passkey, bool) {
	for _, k := range w.keys {
		if bytes.Equal(k.CredentialID, id) {
			return k, true
		}
	}
	return db.Passkey{}, false
}

// loadWebAuthnUser reads the user's passkeys for rp.
func loadWebAuthnUser(ctx context.Context, q *db.Queries, u db.User, rp relyingParty) (*webAuthnUser, error) {
	keys, err := q.ListPasskeys(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	w := &webAuthnUser{u: u}
	for _, k := range keys {
		if k.RpID != rp.ID {
			continue
		}
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(k.Credential), &c); err != nil {
			return nil, fmt.Errorf("passkey %d: %w", k.ID, err)
		}
		w.keys = append(w.keys, k)
		w.creds = append(w.creds, c)
	}
	return w, nil
}

// hasSecondFactor reports whether the user signs in with a second factor: an authenticator
// app or a passkey.
func hasSecondFactor(u db.User) bool { return u.MfaEnabled != 0 || u.PasskeyCount > 0 }

// Passkey is the public view of a passkey. Keep in sync with api/openapi.yaml.
type Passkey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Host       string     `json:"host"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

func toPasskey(k db.Passkey) Passkey {
	p := Passkey{ID: k.ID, Name: k.Name, Host: k.RpID, CreatedAt: time.Unix(k.CreatedAt, 0).UTC()}
	if k.LastUsedAt.Valid {
		t := time.Unix(k.LastUsedAt.Int64, 0).UTC()
		p.LastUsedAt = &t
	}
	return p
}

// Passkeys lists a user's passkeys.
func (s *Service) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	keys, err := s.store.Read().ListPasskeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Passkey, len(keys))
	for i, k := range keys {
		out[i] = toPasskey(k)
	}
	return out, nil
}

// BeginPasskeyRegistration checks the password and returns the options for the browser.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, userID int64, password string, rp relyingParty) (string, *protocol.CredentialCreation, error) {
	if _, err := s.checkPassword(ctx, userID, password); err != nil {
		return "", nil, err
	}
	if _, err := s.store.Write().SetWebAuthnHandle(ctx, db.SetWebAuthnHandleParams{ID: userID, Handle: randomBytes(32)}); err != nil {
		return "", nil, err
	}
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	if u.PasskeyCount >= maxPasskeys {
		return "", nil, ErrPasskeyLimit
	}
	wu, err := loadWebAuthnUser(ctx, s.store.Read(), u, rp)
	if err != nil {
		return "", nil, err
	}
	wa, err := rp.webAuthn()
	if err != nil {
		return "", nil, err
	}
	// A discoverable credential signs in without a username; a security key without space
	// for one still works as a second factor.
	creation, session, err := wa.BeginRegistration(wu,
		webauthn.WithExclusions(wu.descriptors()),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		return "", nil, err
	}
	token, err := s.passkeys.add(&ceremony{rp: rp, session: *session, userID: userID}, s.opts.Now())
	return token, creation, err
}

// FinishPasskeyRegistration stores the new passkey. The first second factor of a user also
// gets backup codes.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, userID int64, token, name string, response []byte) (Passkey, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultPassName
	}
	if utf8.RuneCountInString(name) > passkeyNameMax {
		return Passkey{}, nil, ErrPasskeyName
	}
	ce, ok := s.passkeys.take(token, s.opts.Now())
	if !ok || ce.userID != userID {
		return Passkey{}, nil, ErrPasskeyCeremony
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		s.opts.Logger.Warn("passkey registration response not parsed", "error", err)
		return Passkey{}, nil, ErrPasskeyFailed
	}
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return Passkey{}, nil, err
	}
	wu := &webAuthnUser{u: u}
	wa, err := ce.rp.webAuthn()
	if err != nil {
		return Passkey{}, nil, err
	}
	cred, err := wa.CreateCredential(wu, ce.session, parsed)
	if err != nil {
		s.opts.Logger.Warn("passkey registration refused", "username", u.Username, "error", err)
		return Passkey{}, nil, ErrPasskeyFailed
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return Passkey{}, nil, err
	}
	var (
		key   db.Passkey
		codes []string
	)
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if u.PasskeyCount >= maxPasskeys {
			return ErrPasskeyLimit
		}
		first := !hasSecondFactor(u)
		key, err = q.CreatePasskey(ctx, db.CreatePasskeyParams{
			UserID: userID, CredentialID: cred.ID, Name: name, RpID: ce.rp.ID, Credential: string(raw), Now: s.opts.Now().Unix(),
		})
		if err != nil {
			return err
		}
		if err := q.CountPasskeys(ctx, userID); err != nil {
			return err
		}
		if first {
			codes, err = replaceBackupCodes(ctx, q, userID)
		}
		return err
	})
	if store.IsUniqueViolation(err) {
		return Passkey{}, nil, ErrPasskeyFailed // the credential is registered already
	}
	if err != nil {
		return Passkey{}, nil, err
	}
	s.opts.Logger.Info("passkey added", "username", u.Username, "host", ce.rp.ID)
	return toPasskey(key), codes, nil
}

// DeletePasskey removes one of the user's passkeys after checking the password. Without
// another second factor left, the backup codes go too.
func (s *Service) DeletePasskey(ctx context.Context, userID, id int64, password string) error {
	u, err := s.checkPassword(ctx, userID, password)
	if err != nil {
		return err
	}
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		n, err := q.DeletePasskey(ctx, db.DeletePasskeyParams{ID: id, UserID: userID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrPasskeyNotFound
		}
		if err := q.CountPasskeys(ctx, userID); err != nil {
			return err
		}
		u, err := q.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		if !hasSecondFactor(u) {
			return q.DeleteBackupCodes(ctx, userID)
		}
		return nil
	})
	if err == nil {
		s.opts.Logger.Info("passkey removed", "username", u.Username)
	}
	return err
}

// BeginPasskeyLogin returns the options for signing in without a password: the browser
// offers the passkeys it has for this host.
func (s *Service) BeginPasskeyLogin(rp relyingParty, c Client) (string, *protocol.CredentialAssertion, error) {
	if wait, ok := s.limiter.Allow("login-ip:" + c.IP); !ok {
		return "", nil, &RateLimitedError{RetryAfter: wait}
	}
	wa, err := rp.webAuthn()
	if err != nil {
		return "", nil, err
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return "", nil, err
	}
	token, err := s.passkeys.add(&ceremony{rp: rp, session: *session}, s.opts.Now())
	return token, assertion, err
}

// FinishPasskeyLogin signs in with a passkey alone: it is both factors, as the authenticator
// verified the user (PIN, biometrics).
func (s *Service) FinishPasskeyLogin(ctx context.Context, token string, response []byte, c Client) (User, Issued, error) {
	ipKey := "login-ip:" + c.IP
	if wait, ok := s.limiter.Allow(ipKey); !ok {
		return User{}, Issued{}, &RateLimitedError{RetryAfter: wait}
	}
	ce, ok := s.passkeys.take(token, s.opts.Now())
	if !ok || ce.userID != 0 {
		return User{}, Issued{}, ErrPasskeyCeremony
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		s.limiter.Fail(ipKey)
		return User{}, Issued{}, ErrPasskeyFailed
	}
	wa, err := ce.rp.webAuthn()
	if err != nil {
		return User{}, Issued{}, err
	}
	var wu *webAuthnUser
	_, cred, err := wa.ValidatePasskeyLogin(func(_, handle []byte) (webauthn.User, error) {
		u, err := s.store.Read().GetUserByWebAuthnHandle(ctx, handle)
		if err != nil {
			return nil, err
		}
		wu, err = loadWebAuthnUser(ctx, s.store.Read(), u, ce.rp)
		return wu, err
	}, ce.session, parsed)
	if err != nil || wu == nil {
		s.limiter.Fail(ipKey)
		s.opts.Logger.Warn("passkey sign-in refused", "client_ip", c.IP, "error", err)
		return User{}, Issued{}, ErrPasskeyFailed
	}
	u, err := s.usePasskey(ctx, wu, cred)
	if err != nil {
		s.limiter.Fail(ipKey)
		return toUser(wu.u), Issued{}, err
	}
	s.limiter.Reset(ipKey)
	_, _ = s.store.Write().SetUserLastLogin(ctx, db.SetUserLastLoginParams{
		ID: u.ID, Now: sql.NullInt64{Int64: s.opts.Now().Unix(), Valid: true},
	})
	issued, err := s.createSession(ctx, u.ID, MethodPasskey, c)
	return toUser(u), issued, err
}

// usePasskey records a successful assertion: the signature counter and the flags.
func (s *Service) usePasskey(ctx context.Context, wu *webAuthnUser, cred *webauthn.Credential) (db.User, error) {
	if wu.u.DisabledAt.Valid {
		return db.User{}, ErrUserDisabled
	}
	key, ok := wu.passkeyOf(cred.ID)
	if !ok {
		return db.User{}, ErrPasskeyFailed
	}
	if cred.Authenticator.CloneWarning {
		s.opts.Logger.Warn("passkey signature counter went back: it may have been cloned", "username", wu.u.Username, "passkey", key.Name)
		return db.User{}, ErrPasskeyFailed
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return db.User{}, err
	}
	if _, err := s.store.Write().UpdatePasskeyUse(ctx, db.UpdatePasskeyUseParams{
		ID: key.ID, Credential: string(raw), Now: nullInt(s.opts.Now().Unix()),
	}); err != nil {
		return db.User{}, err
	}
	return wu.u, nil
}

// BeginPasskeyMFA returns the options for the second step of a login with a password.
func (s *Service) BeginPasskeyMFA(ctx context.Context, mfaToken string, rp relyingParty) (*protocol.CredentialAssertion, error) {
	userID, ok := s.challenges.peek(mfaToken, s.opts.Now())
	if !ok {
		return nil, ErrMFAChallenge
	}
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	wu, err := loadWebAuthnUser(ctx, s.store.Read(), u, rp)
	if err != nil {
		return nil, err
	}
	if len(wu.creds) == 0 {
		return nil, ErrPasskeyNotFound
	}
	wa, err := rp.webAuthn()
	if err != nil {
		return nil, err
	}
	assertion, session, err := wa.BeginLogin(wu, webauthn.WithUserVerification(protocol.VerificationDiscouraged))
	if err != nil {
		return nil, err
	}
	if !s.challenges.setPasskey(mfaToken, &ceremony{rp: rp, session: *session, userID: userID}) {
		return nil, ErrMFAChallenge
	}
	return assertion, nil
}

// verifyPasskeyFactor checks the passkey answer of the second step.
func (s *Service) verifyPasskeyFactor(ctx context.Context, u db.User, ce *ceremony, response []byte) (bool, error) {
	if ce == nil || ce.userID != u.ID {
		return false, nil
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return false, nil //nolint:nilerr // a malformed answer is a failed attempt
	}
	wu, err := loadWebAuthnUser(ctx, s.store.Read(), u, ce.rp)
	if err != nil {
		return false, err
	}
	wa, err := ce.rp.webAuthn()
	if err != nil {
		return false, err
	}
	cred, err := wa.ValidateLogin(wu, ce.session, parsed)
	if err != nil {
		s.opts.Logger.Warn("passkey refused", "username", u.Username, "error", err)
		return false, nil
	}
	if _, err := s.usePasskey(ctx, wu, cred); errors.Is(err, ErrPasskeyFailed) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return b
}
