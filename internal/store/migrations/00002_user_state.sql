-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- User management (#74, ADR-0016): disabled accounts, temporary passwords that must be
-- changed at the next sign-in, last sign-in time.

-- +goose Up

-- NULL = active; otherwise when the account was disabled (Unix seconds).
ALTER TABLE users ADD COLUMN disabled_at INTEGER;
-- 1 after an administrator set the password: only a password change is allowed.
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0
    CHECK (must_change_password IN (0, 1));
ALTER TABLE users ADD COLUMN last_login_at INTEGER;
