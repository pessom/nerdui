-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Initial schema (ADR-0004). Timestamps are Unix seconds (UTC).
-- Migrations are forward-only: there are no Down sections.

-- +goose Up

-- Local users (#74, ADR-0005). Role is checked by the authorizer (ADR-0009).
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin')),
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
) STRICT;

-- Server-side sessions (ADR-0005). Only the SHA-256 of the session token is stored.
CREATE TABLE sessions (
    token_hash   BLOB    PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token   TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    client_ip    TEXT    NOT NULL DEFAULT '',
    user_agent   TEXT    NOT NULL DEFAULT ''
) STRICT, WITHOUT ROWID;

CREATE INDEX sessions_user_id ON sessions (user_id);
CREATE INDEX sessions_expires_at ON sessions (expires_at);

-- Key/value application settings.
CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

-- Stacks registered with nerdui (ADR-0008): directories inside --stacks-dir ("managed")
-- or adopted in place from elsewhere ("external", #25).
CREATE TABLE stacks (
    name       TEXT    PRIMARY KEY,
    path       TEXT    NOT NULL UNIQUE,
    mode       TEXT    NOT NULL CHECK (mode IN ('managed', 'external')),
    created_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;
