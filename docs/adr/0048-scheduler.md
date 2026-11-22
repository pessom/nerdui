# 0048. Scheduled jobs

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

Some work should run on a schedule (#82): pruning, backups of nerdui (ADR-0047), nightly
restarts or redeploys, image update checks. Image update checks had their own interval setting
(1, 6, 12 or 24 hours, ADR-0030) and loop; Git stacks poll their repositories on a per-stack
interval (ADR-0040).

## Options considered

- **Intervals** (every N hours) or **cron expressions** (minute hour day month weekday), which
  also say *when* ("Sundays at 04:00") and are what users know from other tools.
- **Task kinds**: arbitrary commands (a shell on the host by another name) or a fixed set of
  nerdui operations.
- **Existing schedules**: keep image update checks on their own interval, or move them to the
  scheduler; the same for Git polling.
- **Missed runs** while nerdui was stopped: skip them, or run once after the start.

## Decision

- A schedule has a name, a kind, its target, kind-specific options, a cron expression
  (`robfig/cron`: five fields or descriptors such as `@daily` and `@every 6h`, in the server's
  time zone) and an enabled flag. The UI describes expressions in words (`cronstrue`).
- Kinds: `prune` (the options of the Prune dialog), `backup` (to a host directory, default
  `<data-dir>/backups`, keeping the newest N, default 7; the password is stored like other
  credentials and never returned), `stack.redeploy`, `stack.restart`, `container.redeploy`,
  `container.restart` (the actions of incoming webhooks, ADR-0041, shared code), and
  `image-updates.check` (followed by automatic updates, as before).
- Every 15 seconds the scheduler starts each enabled schedule whose next time after its last
  run (or last change) has passed: a run missed while nerdui was stopped runs once after start.
  A schedule whose previous run has not finished is skipped. Runs are background jobs (output,
  cancel, `job.failed` notifications) with the user `nerdui (scheduler)`; the last 50 runs of
  each schedule are kept with status and message. Runs left unfinished by a stop are marked
  failed at start.
- Image update checks move to the scheduler: at the first start, each environment gets an
  "Image update check" schedule from its interval (off becomes a disabled schedule), the
  interval endpoints `GET/PUT /environments/{env}/settings/image-updates` are removed and the
  overview's `nextCheck` comes from the schedule.
- Git polling stays a setting of each Git stack, next to its webhook; the Schedules page lists
  it with a link to the stack.
- Schedules are managed with `settings:write`: they act later without a user.

## Consequences

- Removing the image update schedule turns scheduled checks off; it is not created again.
- The systemd unit has a private `/tmp`: backup directories must be elsewhere.
