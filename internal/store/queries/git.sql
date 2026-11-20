-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListGitCredentials :many
SELECT * FROM git_credentials ORDER BY name;

-- name: GetGitCredential :one
SELECT * FROM git_credentials WHERE id = ?;

-- name: CreateGitCredential :one
INSERT INTO git_credentials (name, kind, username, secret, passphrase, created_at, updated_at)
VALUES (@name, @kind, @username, @secret, @passphrase, @now, @now)
RETURNING *;

-- name: UpdateGitCredential :one
UPDATE git_credentials
SET name = @name, kind = @kind, username = @username, secret = @secret, passphrase = @passphrase, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteGitCredential :execrows
DELETE FROM git_credentials WHERE id = ?;

-- name: CountGitStacksByCredential :one
SELECT count(*) FROM git_stacks WHERE credential_id = ?;

-- name: ListGitStacks :many
SELECT * FROM git_stacks ORDER BY name;

-- name: GetGitStack :one
SELECT * FROM git_stacks WHERE name = ?;

-- name: CreateGitStack :exec
INSERT INTO git_stacks (name, url, ref, full_ref, compose_path, credential_id, commit_hash, commit_message,
                        commit_author, commit_at, pulled_at, created_at)
VALUES (@name, @url, @ref, @full_ref, @compose_path, @credential_id, @commit_hash, @commit_message,
        @commit_author, @commit_at, @now, @now);

-- name: SetGitStackCommit :exec
UPDATE git_stacks
SET commit_hash = @commit_hash, commit_message = @commit_message, commit_author = @commit_author,
    commit_at = @commit_at, pulled_at = @now, last_error = '',
    remote_hash = CASE WHEN remote_hash = @commit_hash THEN '' ELSE remote_hash END
WHERE name = @name;

-- name: SetGitStackError :exec
UPDATE git_stacks SET last_error = @error WHERE name = @name;

-- name: SetGitStackAuto :execrows
UPDATE git_stacks SET auto_update = @auto_update, poll_interval = @poll_interval WHERE name = @name;

-- name: SetGitStackWebhook :execrows
UPDATE git_stacks SET webhook_hash = @webhook_hash WHERE name = @name;

-- name: GetGitStackByWebhook :one
SELECT * FROM git_stacks WHERE webhook_hash = ? AND webhook_hash != '';

-- name: SetGitStackChecked :exec
UPDATE git_stacks
SET checked_at = @now,
    remote_hash = CASE WHEN @remote_hash = commit_hash THEN '' ELSE @remote_hash END,
    last_error = @error
WHERE name = @name;

-- name: DeleteGitStack :execrows
DELETE FROM git_stacks WHERE name = ?;

-- name: ListGitKnownHosts :many
SELECT * FROM git_known_hosts ORDER BY host, created_at;

-- name: ListGitHostKeys :many
SELECT key FROM git_known_hosts WHERE host = ? ORDER BY created_at;

-- name: AddGitHostKey :exec
INSERT INTO git_known_hosts (host, key, created_at) VALUES (@host, @key, @now) ON CONFLICT DO NOTHING;

-- name: DeleteGitHostKeys :execrows
DELETE FROM git_known_hosts WHERE host = ?;
