-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Audit log of user actions (#79, ADR-0036): every state-changing request, terminal session,
-- authorization denial, login and logout. Request bodies are not stored.

-- +goose Up

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY,
    at          INTEGER NOT NULL,
    username    TEXT    NOT NULL DEFAULT '',
    api_key     INTEGER NOT NULL DEFAULT 0,
    -- authorization action, e.g. containers:write, or auth:login for authentication
    action      TEXT    NOT NULL,
    method      TEXT    NOT NULL DEFAULT '',
    path        TEXT    NOT NULL DEFAULT '',
    environment TEXT    NOT NULL DEFAULT '',
    namespace   TEXT    NOT NULL DEFAULT '',
    object      TEXT    NOT NULL DEFAULT '',
    status      INTEGER NOT NULL DEFAULT 0,
    -- ok, failed, denied, or for background jobs accepted until they finish, then ok or failed
    result      TEXT    NOT NULL,
    message     TEXT    NOT NULL DEFAULT '',
    job_id      TEXT    NOT NULL DEFAULT '',
    client_ip   TEXT    NOT NULL DEFAULT '',
    user_agent  TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX audit_log_at ON audit_log (at);
CREATE INDEX audit_log_job ON audit_log (job_id) WHERE job_id != '';
