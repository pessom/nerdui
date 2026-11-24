-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Stack templates saved by users (#34, ADR-0051): a compose file and an .env.example.

-- +goose Up

CREATE TABLE stack_templates (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL UNIQUE,
    description TEXT    NOT NULL DEFAULT '',
    compose     TEXT    NOT NULL,
    -- .env.example: KEY=default lines described by the comments above them
    env         TEXT    NOT NULL DEFAULT '',
    created_by  TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

-- +goose Down

DROP TABLE stack_templates;
