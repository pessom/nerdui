-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: RegisterStack :exec
INSERT INTO stacks (name, path, mode, created_at) VALUES (@name, @path, @mode, @now);

-- name: GetStack :one
SELECT * FROM stacks WHERE name = ?;

-- name: ListStacks :many
SELECT * FROM stacks ORDER BY name;

-- name: DeleteStack :execrows
DELETE FROM stacks WHERE name = ?;
