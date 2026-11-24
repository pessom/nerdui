-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListBackupTargets :many
SELECT * FROM backup_targets ORDER BY name;

-- name: GetBackupTarget :one
SELECT * FROM backup_targets WHERE id = ?;

-- name: CreateBackupTarget :one
INSERT INTO backup_targets (name, kind, options, secret, created_at, updated_at)
VALUES (@name, @kind, @options, @secret, @now, @now)
RETURNING *;

-- name: UpdateBackupTarget :one
UPDATE backup_targets SET name = @name, kind = @kind, options = @options, secret = @secret, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteBackupTarget :execrows
DELETE FROM backup_targets WHERE id = ?;
