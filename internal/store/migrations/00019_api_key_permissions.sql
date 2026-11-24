-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- API keys limited to chosen permissions (#80, ADR-0053), and the key that made a request in
-- the audit log.

-- +goose Up

-- JSON array of authorization actions; NULL: everything its user may do.
ALTER TABLE api_keys ADD COLUMN permissions TEXT;

-- "<name> (<prefix>…)" of the API key, or '' for a password session.
ALTER TABLE audit_log ADD COLUMN api_key_name TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE audit_log DROP COLUMN api_key_name;
ALTER TABLE api_keys DROP COLUMN permissions;
