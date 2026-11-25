-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Sign-in with an OpenID Connect provider (#76, ADR-0056).

-- +goose Up

-- The account at the provider a user is linked to: "<issuer> <subject>".
ALTER TABLE users ADD COLUMN oidc_identity TEXT;
CREATE UNIQUE INDEX users_oidc_identity ON users (oidc_identity) WHERE oidc_identity IS NOT NULL;

-- How the session signed in: 'password' (with the second factor, if any) or 'oidc'.
ALTER TABLE sessions ADD COLUMN method TEXT NOT NULL DEFAULT 'password';

-- +goose Down

ALTER TABLE sessions DROP COLUMN method;
DROP INDEX users_oidc_identity;
ALTER TABLE users DROP COLUMN oidc_identity;
