-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: CreateUser :one
INSERT INTO users (username, password_hash, role, created_at, updated_at)
VALUES (@username, @password_hash, @role, @now, @now)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: UpdateUserPassword :execrows
UPDATE users SET password_hash = @password_hash, updated_at = @now WHERE id = @id;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;
