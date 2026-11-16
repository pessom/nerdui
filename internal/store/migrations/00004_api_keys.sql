-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- API keys (#87, ADR-0028): a key acts as its user, optionally read-only. Only the SHA-256 of
-- the key is stored; prefix identifies it in lists.

-- +goose Up

CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    prefix       TEXT    NOT NULL,
    token_hash   BLOB    NOT NULL UNIQUE,
    read_only    INTEGER NOT NULL DEFAULT 0 CHECK (read_only IN (0, 1)),
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER,          -- NULL: does not expire
    last_used_at INTEGER
) STRICT;

CREATE INDEX api_keys_user_id ON api_keys (user_id);
