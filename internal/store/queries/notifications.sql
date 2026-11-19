-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListNotificationChannels :many
SELECT * FROM notification_channels ORDER BY name;

-- name: GetNotificationChannel :one
SELECT * FROM notification_channels WHERE id = ?;

-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (name, type, config, events, enabled, created_at, updated_at)
VALUES (@name, @type, @config, @events, @enabled, @now, @now)
RETURNING *;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET name = @name, type = @type, config = @config, events = @events, enabled = @enabled, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels WHERE id = ?;

-- name: SetNotificationResult :exec
UPDATE notification_channels SET last_sent_at = @sent_at, last_error = @error WHERE id = @id;
