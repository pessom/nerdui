-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListImageUpdates :many
SELECT * FROM image_updates WHERE environment = ? ORDER BY ref;

-- name: SetImageUpdate :exec
-- A failed check keeps the last digest and when it was first seen (#56); a new digest restarts
-- seen_at.
INSERT INTO image_updates (environment, ref, remote_digest, error, checked_at, seen_at)
VALUES (@environment, @ref, @remote_digest, @error, @checked_at, @checked_at)
ON CONFLICT (environment, ref) DO UPDATE SET
    remote_digest = CASE WHEN excluded.remote_digest = '' THEN image_updates.remote_digest ELSE excluded.remote_digest END,
    seen_at = CASE
        WHEN excluded.remote_digest = '' OR excluded.remote_digest = image_updates.remote_digest THEN image_updates.seen_at
        ELSE excluded.checked_at END,
    error = excluded.error,
    checked_at = excluded.checked_at;

-- name: LastImageUpdateCheck :one
SELECT CAST(COALESCE(MAX(checked_at), 0) AS INTEGER) FROM image_updates WHERE environment = ?;
