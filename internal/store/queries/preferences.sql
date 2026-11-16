-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: GetUserPreferences :one
SELECT data FROM user_preferences WHERE user_id = ?;

-- name: SetUserPreferences :exec
INSERT INTO user_preferences (user_id, data, updated_at) VALUES (@user_id, @data, @now)
ON CONFLICT (user_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at;
