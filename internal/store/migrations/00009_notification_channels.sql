-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Notification channels and the events each receives (#86, ADR-0034). config holds the
-- channel's settings as JSON, secrets included; the API never returns them.

-- +goose Up

CREATE TABLE notification_channels (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL UNIQUE,
    type         TEXT    NOT NULL,
    config       TEXT    NOT NULL,
    -- event kinds, JSON array
    events       TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    last_sent_at INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;
