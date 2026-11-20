-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Stacks deployed from Git repositories (#37), the credentials to clone them (#93) and the SSH
-- host keys trusted on first use (ADR-0039). Secrets are write-only in the API.

-- +goose Up

CREATE TABLE git_credentials (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    kind       TEXT    NOT NULL CHECK (kind IN ('token', 'ssh')),
    username   TEXT    NOT NULL DEFAULT '',
    -- token or password (token), PEM private key (ssh)
    secret     TEXT    NOT NULL,
    passphrase TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE git_stacks (
    name           TEXT    PRIMARY KEY,
    url            TEXT    NOT NULL,
    -- as entered ('' = default branch) and resolved (refs/heads/... or refs/tags/...)
    ref            TEXT    NOT NULL,
    full_ref       TEXT    NOT NULL,
    -- compose file relative to the repository root
    compose_path   TEXT    NOT NULL,
    credential_id  INTEGER REFERENCES git_credentials (id) ON DELETE RESTRICT,
    commit_hash    TEXT    NOT NULL,
    commit_message TEXT    NOT NULL DEFAULT '',
    commit_author  TEXT    NOT NULL DEFAULT '',
    commit_at      INTEGER NOT NULL DEFAULT 0,
    pulled_at      INTEGER NOT NULL,
    last_error     TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE INDEX git_stacks_credential ON git_stacks (credential_id);

CREATE TABLE git_known_hosts (
    host       TEXT    NOT NULL,
    key        TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (host, key)
) STRICT, WITHOUT ROWID;

