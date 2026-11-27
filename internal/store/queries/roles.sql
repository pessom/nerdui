-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListRoles :many
SELECT * FROM roles ORDER BY name;

-- name: GetRole :one
SELECT * FROM roles WHERE name = ?;

-- name: CreateRole :one
INSERT INTO roles (name, description, permissions, created_at, updated_at)
VALUES (@name, @description, @permissions, @now, @now)
RETURNING *;

-- name: UpdateRole :execrows
UPDATE roles SET description = @description, permissions = @permissions, updated_at = @now
WHERE name = @name;

-- name: DeleteRole :execrows
DELETE FROM roles WHERE name = ?;

-- name: CountRoleUsers :many
SELECT role, COUNT(*) AS users FROM users GROUP BY role;

-- name: CountUsersWithRole :one
SELECT COUNT(*) FROM users WHERE role = ?;
