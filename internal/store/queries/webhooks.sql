-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY name;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE id = ?;

-- name: GetWebhookByToken :one
SELECT * FROM webhooks WHERE token_hash = ?;

-- name: CreateWebhook :one
INSERT INTO webhooks (name, environment, namespace, target_kind, target, action, token_hash, enabled,
                      created_by, created_at, updated_at)
VALUES (@name, @environment, @namespace, @target_kind, @target, @action, @token_hash, @enabled,
        @created_by, @now, @now)
RETURNING *;

-- name: UpdateWebhook :one
UPDATE webhooks
SET name = @name, environment = @environment, namespace = @namespace, target_kind = @target_kind,
    target = @target, action = @action, enabled = @enabled, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: SetWebhookToken :execrows
UPDATE webhooks SET token_hash = @token_hash, updated_at = @now WHERE id = @id;

-- name: SetWebhookCalled :exec
UPDATE webhooks SET last_called_at = @now, last_job = @last_job, last_error = @last_error WHERE id = @id;

-- name: DeleteWebhook :execrows
DELETE FROM webhooks WHERE id = ?;
