-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: GetSetting :one
SELECT value FROM settings WHERE key = ?;

-- name: SetSetting :exec
INSERT INTO settings (key, value, updated_at) VALUES (@key, @value, @now)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: ListSettings :many
SELECT * FROM settings ORDER BY key;

-- name: DeleteSetting :execrows
DELETE FROM settings WHERE key = ?;
