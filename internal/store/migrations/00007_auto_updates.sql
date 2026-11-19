-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Log of automatic updates of containers and stacks that opted in with the nerdui.auto-update
-- label (#54, ADR-0032).

-- +goose Up

CREATE TABLE auto_updates (
    id          INTEGER PRIMARY KEY,
    environment TEXT    NOT NULL,
    namespace   TEXT    NOT NULL,
    -- container or stack
    kind        TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    -- image references being updated, comma-separated
    refs        TEXT    NOT NULL,
    -- registry digests updated to, comma-separated in the order of refs
    digests     TEXT    NOT NULL,
    -- updated, failed or skipped
    status      TEXT    NOT NULL,
    message     TEXT    NOT NULL DEFAULT '',
    job_id      TEXT    NOT NULL DEFAULT '',
    started_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL
) STRICT;

CREATE INDEX auto_updates_target ON auto_updates (environment, namespace, kind, name, id);
CREATE INDEX auto_updates_started ON auto_updates (environment, started_at);

