// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// fastArgon2 keeps tests quick; production uses DefaultArgon2.
var fastArgon2 = Argon2Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

const goodPassword = "correct horse battery"

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	svc   *Service
	st    *store.Store
	clock *fakeClock
	logs  *bytes.Buffer
}

func newEnv(t *testing.T, secure bool) *env {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clock := &fakeClock{t: time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)}
	logs := &bytes.Buffer{}
	svc, err := New(context.Background(), st, Options{
		SecureCookie: secure,
		Argon2:       fastArgon2,
		Now:          clock.Now,
		Logger:       slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{svc: svc, st: st, clock: clock, logs: logs}
}

// setupToken extracts the token from the startup log line.
func (e *env) setupToken(t *testing.T) string {
	t.Helper()
	for _, line := range strings.Split(e.logs.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) == nil {
			if tok, ok := rec["setup_token"].(string); ok {
				return tok
			}
		}
	}
	t.Fatalf("setup token not logged: %s", e.logs)
	return ""
}

func (e *env) setupAdmin(t *testing.T) (User, Issued) {
	t.Helper()
	u, is, err := e.svc.Setup(context.Background(), e.setupToken(t), "admin", goodPassword, Client{IP: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return u, is
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword(goodPassword, fastArgon2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("hash format: %s", h)
	}
	if ok, rehash, err := VerifyPassword(goodPassword, h, fastArgon2); !ok || rehash || err != nil {
		t.Errorf("verify: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, _ := VerifyPassword("wrong password!!", h, fastArgon2); ok {
		t.Error("wrong password accepted")
	}
	if _, rehash, _ := VerifyPassword(goodPassword, h, DefaultArgon2); !rehash {
		t.Error("weaker parameters not flagged for rehash")
	}
	h2, _ := HashPassword(goodPassword, fastArgon2)
	if h == h2 {
		t.Error("salt not random")
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$a$b", "$argon2id$v=19$m=x$a$b"} {
		if _, _, err := VerifyPassword(goodPassword, bad, fastArgon2); err == nil {
			t.Errorf("malformed hash %q accepted", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	for p, ok := range map[string]bool{
		"short":                   false,
		"exactly12chr":            true,
		"пароль-из-12":            true, // characters, not bytes
		strings.Repeat("a", 1025): false,
	} {
		if err := ValidatePassword(p); (err == nil) != ok {
			t.Errorf("ValidatePassword(%q) = %v, want ok=%v", p, err, ok)
		}
	}
}

func TestLimiter(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	l := NewLimiter(clock.Now)

	for i := 0; i < 4; i++ {
		l.Fail("k")
	}
	if _, ok := l.Allow("k"); !ok {
		t.Fatal("blocked before reaching the limit")
	}
	l.Fail("k")
	wait, ok := l.Allow("k")
	if ok || wait != time.Minute {
		t.Fatalf("after 5 failures: ok=%v wait=%v", ok, wait)
	}

	clock.Add(time.Minute)
	if _, ok := l.Allow("k"); !ok {
		t.Fatal("not unblocked after the block")
	}
	for i := 0; i < 5; i++ {
		l.Fail("k")
	}
	if wait, _ := l.Allow("k"); wait != 2*time.Minute {
		t.Errorf("second block = %v, want doubling to 2m", wait)
	}
	for i := 0; i < 10; i++ {
		clock.Add(l.MaxBlock)
		for j := 0; j < 5; j++ {
			l.Fail("k")
		}
	}
	if wait, _ := l.Allow("k"); wait > l.MaxBlock {
		t.Errorf("block %v exceeds MaxBlock", wait)
	}

	l.Reset("k")
	if _, ok := l.Allow("k"); !ok {
		t.Error("Reset did not unblock")
	}

	// Failures spread over more than the window do not accumulate.
	for i := 0; i < 10; i++ {
		l.Fail("slow")
		clock.Add(2 * l.Window)
	}
	if _, ok := l.Allow("slow"); !ok {
		t.Error("slow failures blocked")
	}
}

func TestSetup(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if !e.svc.SetupRequired() {
		t.Fatal("setup not required on empty database")
	}
	tok := e.setupToken(t)

	if _, _, err := e.svc.Setup(ctx, "wrong", "admin", goodPassword, Client{IP: "1.1.1.1"}); !errors.Is(err, ErrInvalidSetupToken) {
		t.Fatalf("wrong token: %v", err)
	}
	if _, _, err := e.svc.Setup(ctx, tok, "bad name!", goodPassword, Client{}); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("bad username: %v", err)
	}
	if _, _, err := e.svc.Setup(ctx, tok, "admin", "short", Client{}); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password: %v", err)
	}

	u, is, err := e.svc.Setup(ctx, tok, "admin", goodPassword, Client{IP: "1.1.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleAdmin || is.Token == "" || is.CSRFToken == "" {
		t.Errorf("setup result: %+v %+v", u, is)
	}
	if e.svc.SetupRequired() {
		t.Error("setup still required")
	}
	if _, _, err := e.svc.Setup(ctx, tok, "second", goodPassword, Client{}); !errors.Is(err, ErrSetupCompleted) {
		t.Errorf("second setup: %v", err)
	}

	// A restarted service does not ask for setup again.
	svc2, err := New(ctx, e.st, Options{Argon2: fastArgon2, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if svc2.SetupRequired() {
		t.Error("setup required after restart")
	}
}

func TestSetupTokenRateLimited(t *testing.T) {
	e := newEnv(t, true)
	for i := 0; i < 5; i++ {
		_, _, _ = e.svc.Setup(context.Background(), "guess", "admin", goodPassword, Client{IP: "6.6.6.6"})
	}
	_, _, err := e.svc.Setup(context.Background(), e.setupToken(t), "admin", goodPassword, Client{IP: "6.6.6.6"})
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want rate limited", err)
	}
}

func TestLoginAndSessions(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.setupAdmin(t)

	if _, _, err := e.svc.Login(ctx, "admin", "wrong password", Client{IP: "2.2.2.2"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := e.svc.Login(ctx, "nobody", goodPassword, Client{IP: "2.2.2.2"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: %v", err)
	}

	u, is, err := e.svc.Login(ctx, "ADMIN", goodPassword, Client{IP: "2.2.2.2", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "admin" {
		t.Errorf("user = %+v", u)
	}
	if !is.ExpiresAt.Equal(e.clock.Now().Add(DefaultAbsoluteTimeout).Truncate(time.Second)) {
		t.Errorf("ExpiresAt = %v", is.ExpiresAt)
	}

	p, err := e.svc.Authenticate(ctx, is.Token)
	if err != nil || p.User.ID != u.ID || p.CSRFToken != is.CSRFToken {
		t.Fatalf("Authenticate: %+v, %v", p, err)
	}
	if _, err := e.svc.Authenticate(ctx, is.Token+"x"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("tampered token: %v", err)
	}
	if _, err := e.svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("empty token: %v", err)
	}

	// The token itself is never stored, only its hash.
	var stored int
	if err := e.st.Tx(ctx, func(q *db.Queries) error {
		_, err := q.GetSession(ctx, []byte(is.Token))
		if store.IsNotFound(err) {
			return nil
		}
		stored++
		return err
	}); err != nil || stored != 0 {
		t.Errorf("raw token stored: %v", err)
	}

	if err := e.svc.Logout(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, is.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("session valid after logout: %v", err)
	}
}

func TestSessionTimeouts(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	_, is := e.setupAdmin(t)

	// Activity within the idle timeout keeps the session alive.
	for i := 0; i < 3; i++ {
		e.clock.Add(DefaultIdleTimeout - time.Hour)
		if _, err := e.svc.Authenticate(ctx, is.Token); err != nil {
			t.Fatalf("active session expired after %d steps: %v", i, err)
		}
	}
	// Idle timeout.
	e.clock.Add(DefaultIdleTimeout + time.Minute)
	if _, err := e.svc.Authenticate(ctx, is.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("idle session still valid: %v", err)
	}

	// Absolute lifetime, even with constant activity.
	_, is2, err := e.svc.Login(ctx, "admin", goodPassword, Client{})
	if err != nil {
		t.Fatal(err)
	}
	for elapsed := time.Duration(0); elapsed < DefaultAbsoluteTimeout-12*time.Hour; elapsed += 12 * time.Hour {
		e.clock.Add(12 * time.Hour)
		if _, err := e.svc.Authenticate(ctx, is2.Token); err != nil {
			t.Fatalf("session expired early at %v: %v", elapsed, err)
		}
	}
	e.clock.Add(13 * time.Hour)
	if _, err := e.svc.Authenticate(ctx, is2.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("session valid after absolute lifetime: %v", err)
	}
}

func TestCleanup(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.setupAdmin(t)
	e.clock.Add(DefaultIdleTimeout + time.Hour)
	e.svc.cleanup(ctx)
	if !strings.Contains(e.logs.String(), "expired sessions removed") {
		t.Errorf("expired session not cleaned up; logs: %s", e.logs)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.setupAdmin(t)

	for i := 0; i < 5; i++ {
		_, _, _ = e.svc.Login(ctx, "admin", "wrong password", Client{IP: "3.3.3.3"})
	}
	var rl *RateLimitedError
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{IP: "3.3.3.3"}); !errors.As(err, &rl) {
		t.Fatalf("not rate limited by IP: %v", err)
	}
	// The username is blocked from other addresses as well.
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{IP: "4.4.4.4"}); !errors.As(err, &rl) {
		t.Fatalf("not rate limited by username: %v", err)
	}
	e.clock.Add(rl.RetryAfter)
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{IP: "3.3.3.3"}); err != nil {
		t.Fatalf("still blocked after waiting: %v", err)
	}
}

func TestRehashOnLogin(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.setupAdmin(t)

	e.svc.opts.Argon2 = Argon2Params{Memory: 128, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{}); err != nil {
		t.Fatal(err)
	}
	u, _ := e.st.Read().GetUserByUsername(ctx, "admin")
	if !strings.Contains(u.PasswordHash, "m=128,") {
		t.Errorf("hash not upgraded: %s", u.PasswordHash)
	}
}

func TestResetPassword(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	_, is := e.setupAdmin(t)

	if err := ResetPassword(ctx, e.st, "nobody", "a brand new password", time.Now()); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
	if err := ResetPassword(ctx, e.st, "admin", "short", time.Now()); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak password: %v", err)
	}
	if err := ResetPassword(ctx, e.st, "admin", "a brand new password", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, is.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Error("sessions not revoked by reset")
	}
	if _, _, err := e.svc.Login(ctx, "admin", goodPassword, Client{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Error("old password still works")
	}
	if _, _, err := e.svc.Login(ctx, "admin", "a brand new password", Client{}); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
}
