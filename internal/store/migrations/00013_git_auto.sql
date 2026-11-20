-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only
--
-- Automatic updates of Git stacks: a webhook (#38) and polling (#39) find new commits, which are
-- announced or pulled and redeployed (ADR-0040).

-- +goose Up

-- off: nothing; notify: announce new commits; deploy: pull and redeploy
ALTER TABLE git_stacks ADD COLUMN auto_update TEXT NOT NULL DEFAULT 'off'
    CHECK (auto_update IN ('off', 'notify', 'deploy'));
-- seconds between checks of the remote; 0: no polling
ALTER TABLE git_stacks ADD COLUMN poll_interval INTEGER NOT NULL DEFAULT 0;
-- SHA-256 (hex) of the webhook token; '' when there is no webhook
ALTER TABLE git_stacks ADD COLUMN webhook_hash TEXT NOT NULL DEFAULT '';
-- last check of the remote, and the newest remote commit when it differs from commit_hash
ALTER TABLE git_stacks ADD COLUMN checked_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE git_stacks ADD COLUMN remote_hash TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX git_stacks_webhook ON git_stacks (webhook_hash) WHERE webhook_hash != '';
