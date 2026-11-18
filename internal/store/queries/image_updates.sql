-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListImageUpdates :many
SELECT * FROM image_updates WHERE environment = ? ORDER BY ref;

-- name: SetImageUpdate :exec
INSERT INTO image_updates (environment, ref, remote_digest, error, checked_at)
VALUES (@environment, @ref, @remote_digest, @error, @checked_at)
ON CONFLICT (environment, ref) DO UPDATE SET
    remote_digest = excluded.remote_digest,
    error = excluded.error,
    checked_at = excluded.checked_at;

-- name: LastImageUpdateCheck :one
SELECT CAST(COALESCE(MAX(checked_at), 0) AS INTEGER) FROM image_updates WHERE environment = ?;
