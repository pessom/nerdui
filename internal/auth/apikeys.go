// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pessom/nerdui/internal/store"
	"github.com/pessom/nerdui/internal/store/db"
)

// API keys (#87, ADR-0028) authenticate scripts and integrations: "Authorization: Bearer
// nerdui_…". A key acts as its user; a read-only key may only read. Only the SHA-256 of a key
// is stored; the key itself is shown once.

// APIKeyPrefix starts every key, so it is recognizable (secret scanners, logs).
const APIKeyPrefix = "nerdui_"

const (
	maxAPIKeyName     = 64
	maxAPIKeysPerUser = 50
	// apiKeyShownPrefix is how much of a key lists show to tell keys apart.
	apiKeyShownPrefix = len(APIKeyPrefix) + 6
)

// Errors of API key management.
var (
	ErrAPIKeyNotFound = errors.New("API key not found")
	ErrInvalidAPIKey  = errors.New("invalid API key settings")
	ErrTooManyAPIKeys = fmt.Errorf("%w: at most %d keys per user", ErrInvalidAPIKey, maxAPIKeysPerUser)
	ErrNotWithAPIKey  = errors.New("not allowed with an API key: sign in with a password")
)

// APIKey describes a key without its secret. Keep in sync with api/openapi.yaml.
type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	ReadOnly   bool       `json:"readOnly"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// KeyRef identifies the API key a principal authenticated with.
type KeyRef struct {
	ID       int64
	ReadOnly bool
}

func toAPIKey(k db.ApiKey) APIKey {
	out := APIKey{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix, ReadOnly: k.ReadOnly != 0,
		CreatedAt: time.Unix(k.CreatedAt, 0).UTC(),
	}
	if k.ExpiresAt.Valid {
		t := time.Unix(k.ExpiresAt.Int64, 0).UTC()
		out.ExpiresAt = &t
	}
	if k.LastUsedAt.Valid {
		t := time.Unix(k.LastUsedAt.Int64, 0).UTC()
		out.LastUsedAt = &t
	}
	return out
}

// CreateAPIKey creates a key for a user and returns it with its secret, which is not stored.
func (s *Service) CreateAPIKey(ctx context.Context, userID int64, name string, readOnly bool, expiresAt *time.Time) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxAPIKeyName {
		return APIKey{}, "", fmt.Errorf("%w: the name must have 1 to %d characters", ErrInvalidAPIKey, maxAPIKeyName)
	}
	now := s.opts.Now()
	var exp sql.NullInt64
	if expiresAt != nil {
		if !expiresAt.After(now) {
			return APIKey{}, "", fmt.Errorf("%w: the expiry must be in the future", ErrInvalidAPIKey)
		}
		exp = sql.NullInt64{Int64: expiresAt.Unix(), Valid: true}
	}
	n, err := s.store.Read().CountUserAPIKeys(ctx, userID)
	if err != nil {
		return APIKey{}, "", err
	}
	if n >= maxAPIKeysPerUser {
		return APIKey{}, "", ErrTooManyAPIKeys
	}
	token := APIKeyPrefix + randomToken(32)
	ro := int64(0)
	if readOnly {
		ro = 1
	}
	row, err := s.store.Write().CreateAPIKey(ctx, db.CreateAPIKeyParams{
		UserID: userID, Name: name, Prefix: token[:apiKeyShownPrefix], TokenHash: hashToken(token),
		ReadOnly: ro, Now: now.Unix(), ExpiresAt: exp,
	})
	if err != nil {
		return APIKey{}, "", err
	}
	s.opts.Logger.Info("API key created", "user_id", userID, "key_id", row.ID, "name", name, "read_only", readOnly)
	return toAPIKey(row), token, nil
}

// ListAPIKeys returns a user's keys, newest first.
func (s *Service) ListAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.store.Read().ListUserAPIKeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]APIKey, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAPIKey(r))
	}
	return out, nil
}

// DeleteAPIKey revokes a key of a user; requests with it are rejected at once.
func (s *Service) DeleteAPIKey(ctx context.Context, userID, id int64) error {
	n, err := s.store.Write().DeleteUserAPIKey(ctx, db.DeleteUserAPIKeyParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAPIKeyNotFound
	}
	s.opts.Logger.Info("API key revoked", "user_id", userID, "key_id", id)
	return nil
}

// AuthenticateKey resolves an API key to its principal. Unknown, expired keys and keys of
// disabled users are ErrUnauthenticated.
func (s *Service) AuthenticateKey(ctx context.Context, token string) (Principal, error) {
	if !strings.HasPrefix(token, APIKeyPrefix) {
		return Principal{}, ErrUnauthenticated
	}
	row, err := s.store.Read().GetAPIKeyWithUser(ctx, hashToken(token))
	if store.IsNotFound(err) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	now := s.opts.Now()
	k := row.ApiKey
	if (k.ExpiresAt.Valid && !now.Before(time.Unix(k.ExpiresAt.Int64, 0))) || row.User.DisabledAt.Valid {
		return Principal{}, ErrUnauthenticated
	}
	if !k.LastUsedAt.Valid || now.Sub(time.Unix(k.LastUsedAt.Int64, 0)) >= touchInterval {
		_, _ = s.store.Write().TouchAPIKey(ctx, db.TouchAPIKeyParams{ID: k.ID, Now: sql.NullInt64{Int64: now.Unix(), Valid: true}})
	}
	return Principal{User: toUser(row.User), APIKey: &KeyRef{ID: k.ID, ReadOnly: k.ReadOnly != 0}}, nil
}
