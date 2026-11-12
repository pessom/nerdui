-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: CreateUser :one
INSERT INTO users (username, password_hash, role, must_change_password, created_at, updated_at)
VALUES (@username, @password_hash, @role, @must_change_password, @now, @now)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountActiveAdmins :one
SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled_at IS NULL;

-- name: UpdateUserPassword :execrows
UPDATE users
SET password_hash = @password_hash, must_change_password = @must_change_password, updated_at = @now
WHERE id = @id;

-- name: SetUserDisabled :execrows
UPDATE users SET disabled_at = @disabled_at, updated_at = @now WHERE id = @id;

-- name: SetUserLastLogin :execrows
UPDATE users SET last_login_at = @now WHERE id = @id;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = ?;
