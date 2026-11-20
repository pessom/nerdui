-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: InsertAudit :exec
INSERT INTO audit_log (at, username, api_key, action, method, path, environment, namespace, object,
                       status, result, message, job_id, client_ip, user_agent)
VALUES (@at, @username, @api_key, @action, @method, @path, @environment, @namespace, @object,
        @status, @result, @message, @job_id, @client_ip, @user_agent);

-- name: SetAuditJobResult :exec
UPDATE audit_log SET result = @result, message = @message WHERE job_id = @job_id AND result = 'accepted';

-- name: ListAudit :many
-- Newest first, before an id for paging; q matches path, object, action, message and user.
SELECT * FROM audit_log
WHERE (CAST(@before AS INTEGER) = 0 OR id < CAST(@before AS INTEGER))
  AND (CAST(@since AS INTEGER) = 0 OR at >= CAST(@since AS INTEGER))
  AND (CAST(@until AS INTEGER) = 0 OR at < CAST(@until AS INTEGER))
  AND (CAST(@username AS TEXT) = '' OR username = CAST(@username AS TEXT))
  AND (CAST(@result AS TEXT) = '' OR result = CAST(@result AS TEXT))
  AND (CAST(@q AS TEXT) = '' OR instr(lower(path || ' ' || object || ' ' || action || ' ' || message || ' ' || username), lower(CAST(@q AS TEXT))) > 0)
ORDER BY id DESC
LIMIT @max_rows;

-- name: PruneAudit :execrows
DELETE FROM audit_log WHERE at < @before;
