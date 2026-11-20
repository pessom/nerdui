// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// Two-factor authentication with TOTP and backup codes (#75, ADR-0037).

var (
	ErrMFAInvalidCode    = errors.New("invalid or already used code")
	ErrMFANotStarted     = errors.New("start two-factor setup first")
	ErrMFANotEnabled     = errors.New("two-factor authentication is not enabled")
	ErrMFAChallenge      = errors.New("the sign-in attempt expired; enter your password again")
	ErrMFASetupRequired  = errors.New("set up two-factor authentication first")
	ErrMFAAlreadyEnabled = errors.New("two-factor authentication is already enabled; disable it first")
)

const (
	mfaIssuer         = "nerdui"
	challengeTTL      = 5 * time.Minute
	challengeAttempts = 5
	backupCodeCount   = 10
	requireMFAKey     = "auth/require-mfa"
)

// MFAChallengeError is returned by Login when the password was right and a second factor is
// needed: the client sends Token with a code to LoginMFA.
type MFAChallengeError struct{ Token string }

func (e *MFAChallengeError) Error() string { return "two-factor code required" }

type challenge struct {
	userID   int64
	expires  time.Time
	attempts int
}

type challenges struct {
	mu sync.Mutex
	m  map[string]*challenge // by token hash
}

func (c *challenges) add(userID int64, now time.Time) string {
	token := randomToken(32)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*challenge{}
	}
	for k, v := range c.m {
		if !now.Before(v.expires) {
			delete(c.m, k)
		}
	}
	c.m[string(hashToken(token))] = &challenge{userID: userID, expires: now.Add(challengeTTL)}
	return token
}

// use returns the user of a live challenge and counts an attempt; done removes it.
func (c *challenges) use(token string, now time.Time) (int64, func(), bool) {
	key := string(hashToken(token))
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.m[key]
	if !ok || !now.Before(ch.expires) || ch.attempts >= challengeAttempts {
		delete(c.m, key)
		return 0, nil, false
	}
	ch.attempts++
	return ch.userID, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.m, key)
	}, true
}

// mfaState holds the "require two-factor authentication" setting, read at start.
type mfaState struct {
	required atomic.Bool
	loaded   atomic.Bool
}

// RequireMFA reports whether every user must use two-factor authentication.
func (s *Service) RequireMFA(ctx context.Context) bool {
	if !s.mfa.loaded.Load() {
		v, err := s.store.Read().GetSetting(ctx, requireMFAKey)
		if err == nil || store.IsNotFound(err) {
			s.mfa.required.Store(v == "true")
			s.mfa.loaded.Store(true)
		}
	}
	return s.mfa.required.Load()
}

// SetRequireMFA turns the requirement on or off.
func (s *Service) SetRequireMFA(ctx context.Context, on bool) error {
	if err := s.store.Write().SetSetting(ctx, db.SetSettingParams{
		Key: requireMFAKey, Value: fmt.Sprint(on), Now: s.opts.Now().Unix(),
	}); err != nil {
		return err
	}
	s.mfa.required.Store(on)
	s.mfa.loaded.Store(true)
	return nil
}

// checkPassword re-authenticates a signed-in user for sensitive changes.
func (s *Service) checkPassword(ctx context.Context, userID int64, password string) (db.User, error) {
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return db.User{}, err
	}
	if len(password) > MaxPasswordLen {
		return db.User{}, ErrWrongPassword
	}
	ok, _, err := VerifyPassword(password, u.PasswordHash, s.opts.Argon2)
	if err != nil {
		return db.User{}, err
	}
	if !ok {
		return db.User{}, ErrWrongPassword
	}
	return u, nil
}

// MFAStatus describes a user's two-factor authentication. Keep in sync with api/openapi.yaml.
type MFAStatus struct {
	Enabled         bool `json:"enabled"`
	BackupCodesLeft int  `json:"backupCodesLeft"`
	// Required: the administrator requires it from every user.
	Required bool `json:"required"`
}

// MFAStatusOf returns a user's status.
func (s *Service) MFAStatusOf(ctx context.Context, userID int64) (MFAStatus, error) {
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return MFAStatus{}, err
	}
	n, err := s.store.Read().CountBackupCodes(ctx, userID)
	if err != nil {
		return MFAStatus{}, err
	}
	return MFAStatus{Enabled: u.MfaEnabled != 0, BackupCodesLeft: int(n), Required: s.RequireMFA(ctx)}, nil
}

// MFAEnrollment is a new secret to add to an authenticator app.
type MFAEnrollment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// StartMFA creates a pending secret after checking the password.
func (s *Service) StartMFA(ctx context.Context, userID int64, password string) (MFAEnrollment, error) {
	u, err := s.checkPassword(ctx, userID, password)
	if err != nil {
		return MFAEnrollment{}, err
	}
	if u.MfaEnabled != 0 {
		return MFAEnrollment{}, ErrMFAAlreadyEnabled
	}
	secret := newTOTPSecret()
	if _, err := s.store.Write().SetMFASecret(ctx, db.SetMFASecretParams{ID: userID, Secret: secret, Now: s.opts.Now().Unix()}); err != nil {
		return MFAEnrollment{}, err
	}
	return MFAEnrollment{Secret: secret, URI: totpURI(mfaIssuer, u.Username, secret)}, nil
}

// EnableMFA confirms the pending secret with a code and returns new backup codes.
func (s *Service) EnableMFA(ctx context.Context, userID int64, code string) ([]string, error) {
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	switch {
	case u.MfaEnabled != 0:
		return nil, ErrMFAAlreadyEnabled
	case u.MfaSecret == "":
		return nil, ErrMFANotStarted
	}
	step, ok := verifyTOTP(u.MfaSecret, code, s.opts.Now())
	if !ok {
		return nil, ErrMFAInvalidCode
	}
	var codes []string
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.EnableMFA(ctx, db.EnableMFAParams{ID: userID, Step: step, Now: s.opts.Now().Unix()}); err != nil {
			return err
		}
		codes, err = replaceBackupCodes(ctx, q, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.opts.Logger.Info("two-factor authentication enabled", "username", u.Username)
	return codes, nil
}

// NewBackupCodes replaces a user's backup codes after checking the password.
func (s *Service) NewBackupCodes(ctx context.Context, userID int64, password string) ([]string, error) {
	u, err := s.checkPassword(ctx, userID, password)
	if err != nil {
		return nil, err
	}
	if u.MfaEnabled == 0 {
		return nil, ErrMFANotEnabled
	}
	var codes []string
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		codes, err = replaceBackupCodes(ctx, q, userID)
		return err
	})
	return codes, err
}

// DisableMFA turns two-factor authentication off after checking the password.
func (s *Service) DisableMFA(ctx context.Context, userID int64, password string) error {
	u, err := s.checkPassword(ctx, userID, password)
	if err != nil {
		return err
	}
	if err := s.resetMFA(ctx, userID); err != nil {
		return err
	}
	s.opts.Logger.Info("two-factor authentication disabled", "username", u.Username)
	return nil
}

// ResetMFA turns off another user's two-factor authentication (administrators, e.g. a lost
// phone).
func (s *Service) ResetMFA(ctx context.Context, actorID, userID int64) error {
	if actorID == userID {
		return ErrOwnAccount
	}
	if _, err := s.store.Read().GetUserByID(ctx, userID); store.IsNotFound(err) {
		return ErrUserNotFound
	} else if err != nil {
		return err
	}
	return s.resetMFA(ctx, userID)
}

func (s *Service) resetMFA(ctx context.Context, userID int64) error {
	return s.store.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.DisableMFA(ctx, db.DisableMFAParams{ID: userID, Now: s.opts.Now().Unix()}); err != nil {
			return err
		}
		return q.DeleteBackupCodes(ctx, userID)
	})
}

func normalizeBackupCode(c string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}

func backupHash(c string) []byte {
	sum := sha256.Sum256([]byte(normalizeBackupCode(c)))
	return sum[:]
}

func replaceBackupCodes(ctx context.Context, q *db.Queries, userID int64) ([]string, error) {
	if err := q.DeleteBackupCodes(ctx, userID); err != nil {
		return nil, err
	}
	codes := make([]string, backupCodeCount)
	for i := range codes {
		raw := strings.ToLower(newTOTPSecret()[:10]) // 50 bits
		codes[i] = raw[:5] + "-" + raw[5:]
		if err := q.AddBackupCode(ctx, db.AddBackupCodeParams{UserID: userID, CodeHash: backupHash(raw)}); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// verifySecondFactor accepts a TOTP code (once) or a backup code (consumed).
func (s *Service) verifySecondFactor(ctx context.Context, u db.User, code string) (bool, error) {
	if step, ok := verifyTOTP(u.MfaSecret, code, s.opts.Now()); ok {
		n, err := s.store.Write().UseMFAStep(ctx, db.UseMFAStepParams{ID: u.ID, Step: step})
		return n == 1, err
	}
	if len(normalizeBackupCode(code)) != 10 {
		return false, nil
	}
	n, err := s.store.Write().UseBackupCode(ctx, db.UseBackupCodeParams{UserID: u.ID, CodeHash: backupHash(code)})
	if n == 1 {
		s.opts.Logger.Warn("backup code used", "username", u.Username)
	}
	return n == 1, err
}

// LoginMFA completes a login with the second factor and creates a session.
func (s *Service) LoginMFA(ctx context.Context, token, code string, c Client) (User, Issued, error) {
	ipKey := "login-ip:" + c.IP
	if wait, ok := s.limiter.Allow(ipKey); !ok {
		return User{}, Issued{}, &RateLimitedError{RetryAfter: wait}
	}
	userID, done, ok := s.challenges.use(token, s.opts.Now())
	if !ok {
		return User{}, Issued{}, ErrMFAChallenge
	}
	u, err := s.store.Read().GetUserByID(ctx, userID)
	if err != nil {
		return User{}, Issued{}, err
	}
	userKey := "login-user:" + strings.ToLower(u.Username)
	if wait, ok := s.limiter.Allow(userKey); !ok {
		return User{}, Issued{}, &RateLimitedError{RetryAfter: wait}
	}
	if u.DisabledAt.Valid || u.MfaEnabled == 0 {
		done()
		return User{}, Issued{}, ErrMFAChallenge
	}
	ok, err = s.verifySecondFactor(ctx, u, code)
	if err != nil {
		return User{}, Issued{}, err
	}
	if !ok {
		s.limiter.Fail(ipKey)
		s.limiter.Fail(userKey)
		s.opts.Logger.Warn("failed second factor", "username", u.Username, "client_ip", c.IP)
		return toUser(u), Issued{}, ErrMFAInvalidCode
	}
	done()
	s.limiter.Reset(ipKey)
	s.limiter.Reset(userKey)
	return s.finishLogin(ctx, u, c)
}

// ResetMFAOffline turns off a user's two-factor authentication and revokes their sessions. It
// is used by `nerdui admin reset-mfa` and works without a running server, e.g. when the only
// administrator lost both the phone and the backup codes.
func ResetMFAOffline(ctx context.Context, st *store.Store, username string, now time.Time) error {
	return st.Tx(ctx, func(q *db.Queries) error {
		u, err := q.GetUserByUsername(ctx, username)
		if store.IsNotFound(err) {
			return fmt.Errorf("%w: %q", ErrUserNotFound, username)
		}
		if err != nil {
			return err
		}
		if _, err := q.DisableMFA(ctx, db.DisableMFAParams{ID: u.ID, Now: now.Unix()}); err != nil {
			return err
		}
		if err := q.DeleteBackupCodes(ctx, u.ID); err != nil {
			return err
		}
		_, err = q.DeleteUserSessions(ctx, u.ID)
		return err
	})
}
