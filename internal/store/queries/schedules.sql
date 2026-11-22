-- SPDX-FileCopyrightText: 2026 The nerdui Authors
-- SPDX-License-Identifier: AGPL-3.0-only

-- name: ListSchedules :many
SELECT * FROM schedules ORDER BY name;

-- name: GetSchedule :one
SELECT * FROM schedules WHERE id = ?;

-- name: CreateSchedule :one
INSERT INTO schedules (name, kind, environment, namespace, target, options, secret, cron, enabled,
                       created_by, created_at, updated_at)
VALUES (@name, @kind, @environment, @namespace, @target, @options, @secret, @cron, @enabled,
        @created_by, @now, @now)
RETURNING *;

-- name: UpdateSchedule :one
UPDATE schedules
SET name = @name, kind = @kind, environment = @environment, namespace = @namespace, target = @target,
    options = @options, secret = @secret, cron = @cron, enabled = @enabled, updated_at = @now
WHERE id = @id
RETURNING *;

-- name: DeleteSchedule :execrows
DELETE FROM schedules WHERE id = ?;

-- name: SetScheduleLastRun :exec
UPDATE schedules
SET last_run_at = @last_run_at, last_status = @last_status, last_message = @last_message
WHERE id = @id;

-- name: InsertScheduleRun :one
INSERT INTO schedule_runs (schedule_id, trigger, started_at, status, message, job_id)
VALUES (@schedule_id, @trigger, @started_at, @status, @message, @job_id)
RETURNING id;

-- name: FinishScheduleRun :exec
UPDATE schedule_runs SET finished_at = @finished_at, status = @status, message = @message
WHERE id = @id;

-- name: ListScheduleRuns :many
SELECT * FROM schedule_runs WHERE schedule_id = @schedule_id ORDER BY started_at DESC, id DESC
LIMIT @limit;

-- name: PruneScheduleRuns :exec
DELETE FROM schedule_runs
WHERE schedule_runs.schedule_id = sqlc.arg(schedule_id)
  AND schedule_runs.id NOT IN (SELECT r.id FROM schedule_runs AS r WHERE r.schedule_id = sqlc.arg(schedule_id)
                               ORDER BY r.started_at DESC, r.id DESC LIMIT sqlc.arg(keep));

-- name: FailUnfinishedScheduleRuns :exec
UPDATE schedule_runs SET status = 'failed', finished_at = @now, message = 'nerdui stopped during the run'
WHERE finished_at = 0;

-- name: SetScheduleRunJob :exec
UPDATE schedule_runs SET job_id = @job_id WHERE id = @id;
