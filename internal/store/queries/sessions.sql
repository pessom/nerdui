-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, client_ip, user_agent)
VALUES (@token_hash, @user_id, @csrf_token, @now, @now, @expires_at, @client_ip, @user_agent);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: TouchSession :execrows
UPDATE sessions SET last_seen_at = @now WHERE token_hash = @token_hash;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteUserSessions :execrows
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= @now OR last_seen_at <= @idle_before;
