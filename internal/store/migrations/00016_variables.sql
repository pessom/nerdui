-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Variables of stacks (#35, ADR-0049): global ones (stack '') and per stack, passed to
-- `nerdctl compose` as environment variables. Values are encrypted with the key file.

-- +goose Up

CREATE TABLE variables (
    -- stack name, '' for global variables
    stack      TEXT    NOT NULL,
    name       TEXT    NOT NULL,
    -- AES-256-GCM, see internal/secretbox
    value      TEXT    NOT NULL,
    -- 1: the value is never returned by the API
    secret     INTEGER NOT NULL DEFAULT 0 CHECK (secret IN (0, 1)),
    updated_by TEXT    NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (stack, name)
) STRICT;

-- +goose Down

DROP TABLE variables;
