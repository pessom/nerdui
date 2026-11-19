-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: AddAutoUpdate :exec
INSERT INTO auto_updates (environment, namespace, kind, name, refs, digests, status, message, job_id, started_at, finished_at)
VALUES (@environment, @namespace, @kind, @name, @refs, @digests, @status, @message, @job_id, @started_at, @finished_at);

-- name: LastAutoUpdate :one
SELECT * FROM auto_updates
WHERE environment = @environment AND namespace = @namespace AND kind = @kind AND name = @name
ORDER BY id DESC LIMIT 1;

-- name: ListAutoUpdates :many
SELECT * FROM auto_updates
WHERE environment = @environment AND (CAST(@namespace AS TEXT) = '' OR namespace = CAST(@namespace AS TEXT))
ORDER BY id DESC LIMIT @max_rows;

-- name: PruneAutoUpdates :exec
DELETE FROM auto_updates WHERE environment = @environment AND started_at < @before;
