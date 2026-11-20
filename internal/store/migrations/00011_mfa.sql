-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Two-factor authentication with TOTP and backup codes (#75, ADR-0037).

-- +goose Up

-- The base32 TOTP secret: pending until confirmed with a code, then active.
ALTER TABLE users ADD COLUMN mfa_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN mfa_enabled INTEGER NOT NULL DEFAULT 0 CHECK (mfa_enabled IN (0, 1));
-- The newest 30-second step accepted, so a code cannot be used twice.
ALTER TABLE users ADD COLUMN mfa_last_step INTEGER NOT NULL DEFAULT 0;

-- One-time backup codes; only their SHA-256 is stored.
CREATE TABLE mfa_backup_codes (
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash BLOB    NOT NULL,
    PRIMARY KEY (user_id, code_hash)
) STRICT, WITHOUT ROWID;
