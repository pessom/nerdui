-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListVariables :many
SELECT * FROM variables WHERE stack = ? ORDER BY name;

-- name: GetVariable :one
SELECT * FROM variables WHERE stack = @stack AND name = @name;

-- name: PutVariable :exec
INSERT INTO variables (stack, name, value, secret, updated_by, updated_at)
VALUES (@stack, @name, @value, @secret, @updated_by, @updated_at)
ON CONFLICT (stack, name) DO UPDATE SET
    value = excluded.value, secret = excluded.secret,
    updated_by = excluded.updated_by, updated_at = excluded.updated_at;

-- name: DeleteVariable :execrows
DELETE FROM variables WHERE stack = @stack AND name = @name;

-- name: DeleteStackVariables :exec
DELETE FROM variables WHERE stack = ?;
