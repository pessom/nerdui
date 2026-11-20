-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: SetMFASecret :execrows
-- Starts (or restarts) enrollment; MFA stays off until confirmed.
UPDATE users SET mfa_secret = @secret, mfa_enabled = 0, mfa_last_step = 0, updated_at = @now WHERE id = @id;

-- name: EnableMFA :execrows
UPDATE users SET mfa_enabled = 1, mfa_last_step = @step, updated_at = @now WHERE id = @id AND mfa_secret != '';

-- name: DisableMFA :execrows
UPDATE users SET mfa_secret = '', mfa_enabled = 0, mfa_last_step = 0, updated_at = @now WHERE id = @id;

-- name: UseMFAStep :execrows
-- Accepts a TOTP step only once and only forward.
UPDATE users SET mfa_last_step = @step WHERE id = @id AND mfa_last_step < @step;

-- name: DeleteBackupCodes :exec
DELETE FROM mfa_backup_codes WHERE user_id = ?;

-- name: AddBackupCode :exec
INSERT INTO mfa_backup_codes (user_id, code_hash) VALUES (@user_id, @code_hash);

-- name: UseBackupCode :execrows
DELETE FROM mfa_backup_codes WHERE user_id = @user_id AND code_hash = @code_hash;

-- name: CountBackupCodes :one
SELECT COUNT(*) FROM mfa_backup_codes WHERE user_id = ?;
