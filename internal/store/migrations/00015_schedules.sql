-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Scheduled jobs (#82, ADR-0048): a cron expression runs one task (prune, application backup,
-- restart or redeploy of a stack or container, image update check), with a history of runs.

-- +goose Up

CREATE TABLE schedules (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL UNIQUE,
    kind         TEXT    NOT NULL CHECK (kind IN ('prune', 'backup', 'stack.redeploy', 'stack.restart',
                                                  'container.redeploy', 'container.restart',
                                                  'image-updates.check')),
    environment  TEXT    NOT NULL DEFAULT '',
    namespace    TEXT    NOT NULL DEFAULT '',
    -- stack or container name, '' for other kinds
    target       TEXT    NOT NULL DEFAULT '',
    -- kind-specific settings as JSON, e.g. prune options or the backup directory
    options      TEXT    NOT NULL DEFAULT '{}',
    -- the backup password; never returned by the API
    secret       TEXT    NOT NULL DEFAULT '',
    -- five-field cron expression or a descriptor such as @daily, in the server's time zone
    cron         TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_by   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    last_run_at  INTEGER NOT NULL DEFAULT 0,
    -- '', running, succeeded, failed or skipped
    last_status  TEXT    NOT NULL DEFAULT '',
    last_message TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE schedule_runs (
    id          INTEGER PRIMARY KEY,
    schedule_id INTEGER NOT NULL REFERENCES schedules (id) ON DELETE CASCADE,
    -- schedule or manual
    trigger     TEXT    NOT NULL,
    started_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL,
    message     TEXT    NOT NULL DEFAULT '',
    job_id      TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX schedule_runs_by_schedule ON schedule_runs (schedule_id, started_at DESC);

-- +goose Down

DROP TABLE schedule_runs;
DROP TABLE schedules;
