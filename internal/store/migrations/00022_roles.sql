-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Roles (#78, ADR-0058).

-- +goose Up

-- users.role allowed only 'admin'. A column CHECK goes away with its column, so the role
-- moves to a new column without it; the authorizer checks role names.
ALTER TABLE users ADD COLUMN role_name TEXT NOT NULL DEFAULT 'viewer';
UPDATE users SET role_name = role;
ALTER TABLE users DROP COLUMN role;
ALTER TABLE users RENAME COLUMN role_name TO role;

-- Custom roles; the built-in ones (admin, editor, operator, viewer) are defined in code.
-- permissions is a JSON array of authorization actions (ADR-0009).
CREATE TABLE roles (
    name        TEXT    PRIMARY KEY,
    description TEXT    NOT NULL DEFAULT '',
    permissions TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT, WITHOUT ROWID;
