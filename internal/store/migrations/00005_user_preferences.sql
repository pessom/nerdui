-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Interface preferences of each user (#90): theme, date format, refresh interval, hidden
-- table columns, as a JSON object validated by the server.

-- +goose Up

CREATE TABLE user_preferences (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    data       TEXT    NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
