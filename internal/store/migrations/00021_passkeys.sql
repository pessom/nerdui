-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Passkeys (WebAuthn) as a second factor and for sign-in without a password (#226, ADR-0057).

-- +goose Up

-- The random user handle stored with the user's passkeys, set when the first one is added.
ALTER TABLE users ADD COLUMN webauthn_handle BLOB;
CREATE UNIQUE INDEX users_webauthn_handle ON users (webauthn_handle) WHERE webauthn_handle IS NOT NULL;
-- The number of the user's passkeys, kept with the passkeys table: a passkey is a second factor.
ALTER TABLE users ADD COLUMN passkey_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE passkeys (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id BLOB    NOT NULL UNIQUE,
    name          TEXT    NOT NULL,
    -- The host name the passkey is bound to (WebAuthn relying party ID).
    rp_id         TEXT    NOT NULL,
    -- The public key, the signature counter and the flags (JSON of the WebAuthn library).
    credential    TEXT    NOT NULL,
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER
) STRICT;

CREATE INDEX passkeys_user_id ON passkeys (user_id);

-- +goose Down

DROP TABLE passkeys;
ALTER TABLE users DROP COLUMN passkey_count;
DROP INDEX users_webauthn_handle;
ALTER TABLE users DROP COLUMN webauthn_handle;
