-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Backups of volumes (#59, ADR-0050): where archives go (a host directory or an S3 bucket), and
-- the volume.backup kind of schedules.

-- +goose Up

CREATE TABLE backup_targets (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    kind       TEXT    NOT NULL CHECK (kind IN ('local', 's3')),
    -- local: directory; s3: endpoint, region, bucket, prefix, access key, path style (JSON)
    options    TEXT    NOT NULL DEFAULT '{}',
    -- the S3 secret key, encrypted (internal/secretbox); never returned by the API
    secret     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

-- The kinds of schedules are a CHECK constraint: rebuild the tables with the new kind. The runs
-- are rebuilt first, so dropping the old schedules cascades to nothing.
CREATE TABLE schedules_new (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL UNIQUE,
    kind         TEXT    NOT NULL CHECK (kind IN ('prune', 'backup', 'stack.redeploy', 'stack.restart',
                                                  'container.redeploy', 'container.restart',
                                                  'image-updates.check', 'volume.backup')),
    environment  TEXT    NOT NULL DEFAULT '',
    namespace    TEXT    NOT NULL DEFAULT '',
    target       TEXT    NOT NULL DEFAULT '',
    options      TEXT    NOT NULL DEFAULT '{}',
    secret       TEXT    NOT NULL DEFAULT '',
    cron         TEXT    NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_by   TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    last_run_at  INTEGER NOT NULL DEFAULT 0,
    last_status  TEXT    NOT NULL DEFAULT '',
    last_message TEXT    NOT NULL DEFAULT ''
) STRICT;
INSERT INTO schedules_new SELECT * FROM schedules;

CREATE TABLE schedule_runs_new (
    id          INTEGER PRIMARY KEY,
    schedule_id INTEGER NOT NULL REFERENCES schedules_new (id) ON DELETE CASCADE,
    trigger     TEXT    NOT NULL,
    started_at  INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL,
    message     TEXT    NOT NULL DEFAULT '',
    job_id      TEXT    NOT NULL DEFAULT ''
) STRICT;
INSERT INTO schedule_runs_new SELECT * FROM schedule_runs;

DROP TABLE schedule_runs;
DROP TABLE schedules;
-- Renaming updates the reference of schedule_runs_new.
ALTER TABLE schedules_new RENAME TO schedules;
ALTER TABLE schedule_runs_new RENAME TO schedule_runs;
CREATE INDEX schedule_runs_by_schedule ON schedule_runs (schedule_id, started_at DESC);

-- +goose Down

DELETE FROM schedules WHERE kind = 'volume.backup';
DROP TABLE backup_targets;
