-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Incoming webhooks (#85, #91, ADR-0041): a secret URL runs one action on one stack or
-- container, e.g. from CI after an image was published.

-- +goose Up

CREATE TABLE webhooks (
    id             INTEGER PRIMARY KEY,
    name           TEXT    NOT NULL UNIQUE,
    environment    TEXT    NOT NULL,
    namespace      TEXT    NOT NULL,
    target_kind    TEXT    NOT NULL CHECK (target_kind IN ('stack', 'container')),
    -- stack name or container name
    target         TEXT    NOT NULL,
    -- redeploy: pull images and recreate; restart: restart as is
    action         TEXT    NOT NULL CHECK (action IN ('redeploy', 'restart')),
    -- SHA-256 (hex) of the secret token; the token is shown once
    token_hash     TEXT    NOT NULL UNIQUE,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_by     TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL,
    last_called_at INTEGER NOT NULL DEFAULT 0,
    -- job of the newest call, or why it could not start
    last_job       TEXT    NOT NULL DEFAULT '',
    last_error     TEXT    NOT NULL DEFAULT ''
) STRICT;
