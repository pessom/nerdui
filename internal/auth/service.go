// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package auth implements local authentication (ADR-0005): argon2id passwords,
// server-side sessions stored in SQLite, CSRF protection, the first-run setup token and
// login rate limiting.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pessom/nerdui/internal/authz"
	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Defaults (ADR-0005).
const (
	DefaultIdleTimeout     = 24 * time.Hour
	DefaultAbsoluteTimeout = 30 * 24 * time.Hour
	// touchInterval limits last-seen updates to one write per minute per session.
	touchInterval   = time.Minute
	cleanupInterval = time.Hour
	// RoleAdmin is the only role in MVP (ADR-0009).
	RoleAdmin = authz.RoleAdmin
)

// Errors returned by the service.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthenticated    = errors.New("not authenticated")
	ErrSetupCompleted     = errors.New("setup has already been completed")
	ErrInvalidSetupToken  = errors.New("invalid setup token")
	ErrInvalidUsername    = errors.New("username must be 1-64 characters: letters, digits, '.', '_' or '-'")
	ErrUserNotFound       = errors.New("user not found")
)

// RateLimitedError carries the time until the next attempt is allowed.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many attempts, retry in %s", e.RetryAfter.Round(time.Second))
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// User is the public view of a user.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Principal is the authenticated caller attached to a request context.
type Principal struct {
	User      User
	CSRFToken string
	ExpiresAt time.Time
	tokenHash []byte
}

// Issued is a newly created session; Token goes into the cookie and is never stored.
type Issued struct {
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

// Client describes where a request came from (stored with the session for auditing).
type Client struct {
	IP        string
	UserAgent string
}

// Options configures the service.
type Options struct {
	SecureCookie    bool
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	Argon2          Argon2Params
	Now             func() time.Time
	Logger          *slog.Logger
}

// Service is the authentication service.
type Service struct {
	store   *store.Store
	opts    Options
	limiter *Limiter
	// dummyHash is verified when a username does not exist so that response time does not
	// reveal which usernames exist.
	dummyHash string

	mu         sync.Mutex
	setupToken string // non-empty while no user exists
}

// New creates the service. When the database has no users, it generates a one-time setup
// token and logs it (ADR-0005): whoever creates the first admin must have access to the logs.
func New(ctx context.Context, st *store.Store, opts Options) (*Service, error) {
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = DefaultIdleTimeout
	}
	if opts.AbsoluteTimeout == 0 {
		opts.AbsoluteTimeout = DefaultAbsoluteTimeout
	}
	if opts.Argon2 == (Argon2Params{}) {
		opts.Argon2 = DefaultArgon2
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	dummy, err := HashPassword("nerdui-dummy-password", opts.Argon2)
	if err != nil {
		return nil, err
	}
	s := &Service{store: st, opts: opts, limiter: NewLimiter(opts.Now), dummyHash: dummy}

	n, err := st.Read().CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}
	if n == 0 {
		s.setupToken = randomToken(16)
		opts.Logger.Warn("nerdui is not set up yet: open the web UI and enter this one-time setup token "+
			"to create the admin account", "setup_token", s.setupToken)
	}
	return s, nil
}

// SecureCookie reports whether session cookies are marked Secure.
func (s *Service) SecureCookie() bool { return s.opts.SecureCookie }

// SetupRequired reports whether the first admin still has to be created.
func (s *Service) SetupRequired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setupToken != ""
}

// Setup creates the first admin account and logs it in.
func (s *Service) Setup(ctx context.Context, token, username, password string, c Client) (User, Issued, error) {
	key := "setup-ip:" + c.IP
	if wait, ok := s.limiter.Allow(key); !ok {
		return User{}, Issued{}, &RateLimitedError{RetryAfter: wait}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setupToken == "" {
		return User{}, Issued{}, ErrSetupCompleted
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) != 1 {
		s.limiter.Fail(key)
		return User{}, Issued{}, ErrInvalidSetupToken
	}
	if !usernameRe.MatchString(username) {
		return User{}, Issued{}, ErrInvalidUsername
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, Issued{}, err
	}
	hash, err := HashPassword(password, s.opts.Argon2)
	if err != nil {
		return User{}, Issued{}, err
	}

	var u db.User
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		n, err := q.CountUsers(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupCompleted
		}
		u, err = q.CreateUser(ctx, db.CreateUserParams{
			Username: username, PasswordHash: hash, Role: RoleAdmin, Now: s.opts.Now().Unix(),
		})
		return err
	})
	if err != nil {
		return User{}, Issued{}, err
	}
	s.setupToken = ""
	s.limiter.Reset(key)
	s.opts.Logger.Info("setup completed: admin account created", "username", u.Username)

	issued, err := s.createSession(ctx, u.ID, c)
	return toUser(u), issued, err
}

// Login verifies credentials and creates a session.
func (s *Service) Login(ctx context.Context, username, password string, c Client) (User, Issued, error) {
	ipKey := "login-ip:" + c.IP
	userKey := "login-user:" + strings.ToLower(username)
	for _, k := range []string{ipKey, userKey} {
		if wait, ok := s.limiter.Allow(k); !ok {
			return User{}, Issued{}, &RateLimitedError{RetryAfter: wait}
		}
	}
	fail := func() (User, Issued, error) {
		s.limiter.Fail(ipKey)
		s.limiter.Fail(userKey)
		s.opts.Logger.Warn("failed login", "username", username, "client_ip", c.IP)
		return User{}, Issued{}, ErrInvalidCredentials
	}
	if len(password) > MaxPasswordLen {
		return fail()
	}

	u, err := s.store.Read().GetUserByUsername(ctx, username)
	switch {
	case store.IsNotFound(err):
		_, _, _ = VerifyPassword(password, s.dummyHash, s.opts.Argon2)
		return fail()
	case err != nil:
		return User{}, Issued{}, err
	}
	ok, rehash, err := VerifyPassword(password, u.PasswordHash, s.opts.Argon2)
	if err != nil {
		return User{}, Issued{}, fmt.Errorf("verify password of %q: %w", u.Username, err)
	}
	if !ok {
		return fail()
	}
	s.limiter.Reset(ipKey)
	s.limiter.Reset(userKey)

	if rehash {
		if h, err := HashPassword(password, s.opts.Argon2); err == nil {
			_, _ = s.store.Write().UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
				ID: u.ID, PasswordHash: h, Now: s.opts.Now().Unix(),
			})
		}
	}
	issued, err := s.createSession(ctx, u.ID, c)
	return toUser(u), issued, err
}

// Authenticate resolves a session token (from the cookie) to a principal.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	h := hashToken(token)
	row, err := s.store.Read().GetSessionWithUser(ctx, h)
	if store.IsNotFound(err) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	now := s.opts.Now()
	sess := row.Session
	idleDeadline := time.Unix(sess.LastSeenAt, 0).Add(s.opts.IdleTimeout)
	if !now.Before(time.Unix(sess.ExpiresAt, 0)) || !now.Before(idleDeadline) {
		_, _ = s.store.Write().DeleteSession(ctx, h)
		return Principal{}, ErrUnauthenticated
	}
	if now.Sub(time.Unix(sess.LastSeenAt, 0)) >= touchInterval {
		_, _ = s.store.Write().TouchSession(ctx, db.TouchSessionParams{TokenHash: h, Now: now.Unix()})
	}
	return Principal{
		User:      toUser(row.User),
		CSRFToken: sess.CsrfToken,
		ExpiresAt: time.Unix(sess.ExpiresAt, 0),
		tokenHash: h,
	}, nil
}

// Logout revokes the principal's session.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	_, err := s.store.Write().DeleteSession(ctx, p.tokenHash)
	return err
}

// CleanupLoop deletes expired sessions periodically until ctx is done.
func (s *Service) CleanupLoop(ctx context.Context) {
	t := time.NewTicker(cleanupInterval)
	defer t.Stop()
	for {
		s.cleanup(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) cleanup(ctx context.Context) {
	now := s.opts.Now()
	n, err := s.store.Write().DeleteExpiredSessions(ctx, db.DeleteExpiredSessionsParams{
		Now: now.Unix(), IdleBefore: now.Add(-s.opts.IdleTimeout).Unix(),
	})
	if err != nil && ctx.Err() == nil {
		s.opts.Logger.Warn("session cleanup failed", "error", err)
	} else if n > 0 {
		s.opts.Logger.Debug("expired sessions removed", "count", n)
	}
}

func (s *Service) createSession(ctx context.Context, userID int64, c Client) (Issued, error) {
	now := s.opts.Now()
	token := randomToken(32)
	csrf := randomToken(32)
	expires := now.Add(s.opts.AbsoluteTimeout)
	err := s.store.Write().CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(token), UserID: userID, CsrfToken: csrf,
		Now: now.Unix(), ExpiresAt: expires.Unix(),
		ClientIp: c.IP, UserAgent: truncate(c.UserAgent, 512),
	})
	if err != nil {
		return Issued{}, fmt.Errorf("create session: %w", err)
	}
	return Issued{Token: token, CSRFToken: csrf, ExpiresAt: time.Unix(expires.Unix(), 0)}, nil
}

// ResetPassword sets a new password for username and revokes all of their sessions. It is
// used by `nerdui admin reset-password` and works without a running server.
func ResetPassword(ctx context.Context, st *store.Store, username, password string, now time.Time) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password, DefaultArgon2)
	if err != nil {
		return err
	}
	return st.Tx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if store.IsNotFound(err) {
			return fmt.Errorf("%w: %q", ErrUserNotFound, username)
		}
		if err != nil {
			return err
		}
		if _, err := q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
			ID: u.ID, PasswordHash: hash, Now: now.Unix(),
		}); err != nil {
			return err
		}
		_, err = q.DeleteUserSessions(ctx, u.ID)
		return err
	})
}

func toUser(u db.User) User { return User{ID: u.ID, Username: u.Username, Role: u.Role} }

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err)) // never happens on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
