-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: SetWebAuthnHandle :execrows
-- Sets the user handle once; a user keeps it for all their passkeys.
UPDATE users SET webauthn_handle = @handle WHERE id = @id AND webauthn_handle IS NULL;

-- name: GetUserByWebAuthnHandle :one
SELECT * FROM users WHERE webauthn_handle = ?;

-- name: ListPasskeys :many
SELECT * FROM passkeys WHERE user_id = ? ORDER BY id;

-- name: CreatePasskey :one
INSERT INTO passkeys (user_id, credential_id, name, rp_id, credential, created_at)
VALUES (@user_id, @credential_id, @name, @rp_id, @credential, @now)
RETURNING *;

-- name: UpdatePasskeyUse :execrows
UPDATE passkeys SET credential = @credential, last_used_at = @now WHERE id = @id;

-- name: DeletePasskey :execrows
DELETE FROM passkeys WHERE id = @id AND user_id = @user_id;

-- name: DeleteUserPasskeys :exec
DELETE FROM passkeys WHERE user_id = ?;

-- name: CountPasskeys :exec
-- Keeps users.passkey_count in step with the passkeys table; run after every change.
UPDATE users SET passkey_count = (SELECT COUNT(*) FROM passkeys WHERE passkeys.user_id = users.id) WHERE users.id = @id;
