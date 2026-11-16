-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: CreateAPIKey :one
INSERT INTO api_keys (user_id, name, prefix, token_hash, read_only, created_at, expires_at)
VALUES (@user_id, @name, @prefix, @token_hash, @read_only, @now, @expires_at)
RETURNING *;

-- name: ListUserAPIKeys :many
SELECT * FROM api_keys WHERE user_id = ? ORDER BY created_at DESC, id DESC;

-- name: GetAPIKeyWithUser :one
SELECT sqlc.embed(api_keys), sqlc.embed(users)
FROM api_keys
JOIN users ON users.id = api_keys.user_id
WHERE api_keys.token_hash = ?;

-- name: TouchAPIKey :execrows
UPDATE api_keys SET last_used_at = @now WHERE id = @id;

-- name: DeleteUserAPIKey :execrows
DELETE FROM api_keys WHERE id = @id AND user_id = @user_id;

-- name: CountUserAPIKeys :one
SELECT COUNT(*) FROM api_keys WHERE user_id = ?;
