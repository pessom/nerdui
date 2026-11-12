-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: InsertEvent :exec
INSERT INTO events (environment, ts, namespace, topic, kind, action, object_id, object_name, attributes)
VALUES (@environment, @ts, @namespace, @topic, @kind, @action, @object_id, @object_name, @attributes);

-- name: ListEvents :many
-- Newest first, before the cursor id (0 = from the newest). Empty filters match everything.
SELECT * FROM events
WHERE environment = @environment
  AND (CAST(@before AS INTEGER) = 0 OR id < CAST(@before AS INTEGER))
  AND (CAST(@namespace AS TEXT) = '' OR namespace = CAST(@namespace AS TEXT) OR kind = 'nerdui')
  AND (CAST(@kind AS TEXT) = '' OR kind = CAST(@kind AS TEXT))
  AND (CAST(@object AS TEXT) = '' OR object_id = CAST(@object AS TEXT) OR object_name = CAST(@object AS TEXT))
ORDER BY id DESC
LIMIT @lim;

-- name: DeleteEventsBefore :execrows
DELETE FROM events WHERE ts < @ts;

-- name: DeleteEventsBeyond :execrows
-- Keeps the newest @keep events.
DELETE FROM events WHERE id <= (SELECT id FROM events ORDER BY id DESC LIMIT 1 OFFSET @keep);
