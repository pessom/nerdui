-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListStackTemplates :many
SELECT * FROM stack_templates ORDER BY name;

-- name: GetStackTemplate :one
SELECT * FROM stack_templates WHERE id = ?;

-- name: CreateStackTemplate :one
INSERT INTO stack_templates (name, description, compose, env, created_by, created_at, updated_at)
VALUES (@name, @description, @compose, @env, @created_by, @now, @now)
RETURNING *;

-- name: UpdateStackTemplate :one
UPDATE stack_templates SET name = @name, description = @description, compose = @compose, env = @env,
    updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteStackTemplate :execrows
DELETE FROM stack_templates WHERE id = ?;
