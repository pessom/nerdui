-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Event history for the event feed (#65, ADR-0021): containerd events of every environment,
-- kept for a limited time (--events-retention).

-- +goose Up

CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    environment TEXT    NOT NULL,
    ts          INTEGER NOT NULL, -- Unix milliseconds
    namespace   TEXT    NOT NULL DEFAULT '',
    topic       TEXT    NOT NULL,
    kind        TEXT    NOT NULL,
    action      TEXT    NOT NULL,
    object_id   TEXT    NOT NULL DEFAULT '',
    -- Container name at the time of the event (containers are gone after a delete).
    object_name TEXT    NOT NULL DEFAULT '',
    attributes  TEXT    NOT NULL DEFAULT '{}' -- JSON object
) STRICT;

CREATE INDEX events_environment_id ON events (environment, id);
CREATE INDEX events_ts ON events (ts);
