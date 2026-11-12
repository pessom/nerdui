// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// User management errors (#74, ADR-0016).
var (
	ErrUsernameTaken  = errors.New("username is already taken")
	ErrOwnAccount     = errors.New("use your account settings to change your own account")
	ErrLastAdmin      = errors.New("at least one active administrator is required")
	ErrWrongPassword  = errors.New("current password is incorrect")
	ErrSamePassword   = errors.New("the new password must differ from the current one")
	ErrPasswordChange = errors.New("change your temporary password first")
)

// generatedPasswordBytes is the entropy of generated temporary passwords (24 characters).
const generatedPasswordBytes = 18

// UserDetail is the administrator's view of a user.
type UserDetail struct {
	User
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

func toDetail(u db.User) UserDetail {
	d := UserDetail{User: toUser(u), Disabled: u.DisabledAt.Valid, CreatedAt: time.Unix(u.CreatedAt, 0).UTC()}
	if u.LastLoginAt.Valid {
		t := time.Unix(u.LastLoginAt.Int64, 0).UTC()
		d.LastLoginAt = &t
	}
	return d
}

// ListUsers returns all users sorted by username.
func (s *Service) ListUsers(ctx context.Context) ([]UserDetail, error) {
	us, err := s.store.Read().ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserDetail, 0, len(us))
	for _, u := range us {
		out = append(out, toDetail(u))
	}
	return out, nil
}

// GetUser returns one user.
func (s *Service) GetUser(ctx context.Context, id int64) (UserDetail, error) {
	u, err := s.store.Read().GetUserByID(ctx, id)
	if store.IsNotFound(err) {
		return UserDetail{}, ErrUserNotFound
	}
	if err != nil {
		return UserDetail{}, err
	}
	return toDetail(u), nil
}

// CreateUser adds an administrator with a temporary password that must be changed at the
// first sign-in. An empty password is generated and returned; a given one is returned empty.
func (s *Service) CreateUser(ctx context.Context, username, password string) (UserDetail, string, error) {
	if !usernameRe.MatchString(username) {
		return UserDetail{}, "", ErrInvalidUsername
	}
	hash, generated, err := s.temporaryPassword(password)
	if err != nil {
		return UserDetail{}, "", err
	}
	var u db.User
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetUserByUsername(ctx, username); err == nil {
			return ErrUsernameTaken
		} else if !store.IsNotFound(err) {
			return err
		}
		u, err = q.CreateUser(ctx, db.CreateUserParams{
			Username: username, PasswordHash: hash, Role: RoleAdmin, MustChangePassword: 1,
			Now: s.opts.Now().Unix(),
		})
		return err
	})
	if err != nil {
		return UserDetail{}, "", err
	}
	s.opts.Logger.Info("user created", "username", u.Username)
	return toDetail(u), generated, nil
}

// SetDisabled disables or enables another user. Disabling revokes the user's sessions.
func (s *Service) SetDisabled(ctx context.Context, actor, id int64, disabled bool) (UserDetail, error) {
	if actor == id {
		return UserDetail{}, ErrOwnAccount
	}
	now := s.opts.Now().Unix()
	var u db.User
	err := s.store.Tx(ctx, func(q *db.Queries) error {
		var err error
		if u, err = s.lockTarget(ctx, q, id, disabled); err != nil {
			return err
		}
		at := sql.NullInt64{}
		if disabled {
			at = sql.NullInt64{Int64: now, Valid: true}
			if u.DisabledAt.Valid {
				at = u.DisabledAt // keep the original time
			}
			if _, err := q.DeleteUserSessions(ctx, id); err != nil {
				return err
			}
		}
		if _, err := q.SetUserDisabled(ctx, db.SetUserDisabledParams{ID: id, DisabledAt: at, Now: now}); err != nil {
			return err
		}
		u.DisabledAt = at
		return nil
	})
	if err != nil {
		return UserDetail{}, err
	}
	s.opts.Logger.Info("user updated", "username", u.Username, "disabled", disabled, "actor_id", actor)
	return toDetail(u), nil
}

// SetTemporaryPassword sets another user's password, revokes their sessions and requires a
// change at the next sign-in. An empty password is generated and returned.
func (s *Service) SetTemporaryPassword(ctx context.Context, actor, id int64, password string) (string, error) {
	if actor == id {
		return "", ErrOwnAccount
	}
	hash, generated, err := s.temporaryPassword(password)
	if err != nil {
		return "", err
	}
	var name string
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		u, err := s.lockTarget(ctx, q, id, false)
		if err != nil {
			return err
		}
		name = u.Username
		if _, err := q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
			ID: id, PasswordHash: hash, MustChangePassword: 1, Now: s.opts.Now().Unix(),
		}); err != nil {
			return err
		}
		_, err = q.DeleteUserSessions(ctx, id)
		return err
	})
	if err != nil {
		return "", err
	}
	s.opts.Logger.Info("temporary password set", "username", name, "actor_id", actor)
	return generated, nil
}

// DeleteUser removes another user and their sessions.
func (s *Service) DeleteUser(ctx context.Context, actor, id int64) error {
	if actor == id {
		return ErrOwnAccount
	}
	var name string
	err := s.store.Tx(ctx, func(q *db.Queries) error {
		u, err := s.lockTarget(ctx, q, id, true)
		if err != nil {
			return err
		}
		name = u.Username
		_, err = q.DeleteUser(ctx, id) // sessions cascade
		return err
	})
	if err != nil {
		return err
	}
	s.opts.Logger.Info("user deleted", "username", name, "actor_id", actor)
	return nil
}

// lockTarget loads the user inside a write transaction. With deactivating, it refuses to
// remove the last active administrator.
func (s *Service) lockTarget(ctx context.Context, q *db.Queries, id int64, deactivating bool) (db.User, error) {
	u, err := q.GetUserByID(ctx, id)
	if store.IsNotFound(err) {
		return db.User{}, ErrUserNotFound
	}
	if err != nil {
		return db.User{}, err
	}
	if deactivating && u.Role == RoleAdmin && !u.DisabledAt.Valid {
		n, err := q.CountActiveAdmins(ctx)
		if err != nil {
			return db.User{}, err
		}
		if n <= 1 {
			return db.User{}, ErrLastAdmin
		}
	}
	return u, nil
}

// ChangePassword changes the caller's own password after verifying the current one. It
// clears the temporary-password flag and revokes the caller's other sessions.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	key := "password-user:" + strconv.FormatInt(p.User.ID, 10)
	if wait, ok := s.limiter.Allow(key); !ok {
		return &RateLimitedError{RetryAfter: wait}
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	if current == next {
		return ErrSamePassword
	}
	u, err := s.store.Read().GetUserByID(ctx, p.User.ID)
	if err != nil {
		return err
	}
	ok := false
	if len(current) <= MaxPasswordLen {
		if ok, _, err = VerifyPassword(current, u.PasswordHash, s.opts.Argon2); err != nil {
			return fmt.Errorf("verify password of %q: %w", u.Username, err)
		}
	}
	if !ok {
		s.limiter.Fail(key)
		return ErrWrongPassword
	}
	s.limiter.Reset(key)
	hash, err := HashPassword(next, s.opts.Argon2)
	if err != nil {
		return err
	}
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
			ID: u.ID, PasswordHash: hash, Now: s.opts.Now().Unix(),
		}); err != nil {
			return err
		}
		_, err := q.DeleteOtherUserSessions(ctx, db.DeleteOtherUserSessionsParams{UserID: u.ID, Keep: p.tokenHash})
		return err
	})
	if err != nil {
		return err
	}
	s.opts.Logger.Info("password changed", "username", u.Username)
	return nil
}

// temporaryPassword hashes password, or a generated one when it is empty.
func (s *Service) temporaryPassword(password string) (hash, generated string, err error) {
	if password == "" {
		generated = randomToken(generatedPasswordBytes)
		password = generated
	}
	if err := ValidatePassword(password); err != nil {
		return "", "", err
	}
	hash, err = HashPassword(password, s.opts.Argon2)
	return hash, generated, err
}
