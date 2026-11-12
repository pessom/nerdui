# 0018. Background jobs: terminal output, retention, cancellation

- **Status:** Accepted (details [0006](0006-api-model-environments-jobs.md) for jobs)
- **Date:** 2026-11-12

## Context

ADR-0006 sets the job model: long operations answer `202` with a job, progress streams over
`WS /api/v1/jobs/{id}/stream`, jobs live in memory, one job per resource. The first job is the
image pull (#43); prune (#52), container creation (#17) and stack deploy follow. nerdctl reports
pull progress as a terminal table redrawn every 100 ms with ANSI cursor sequences; compose
output is terminal text as well.

## Options considered

### Progress

- **Terminal output** — the job writes what nerdctl writes; the browser renders it with xterm.js
  (already used for logs, ADR-0014). One mechanism for every job, identical to the CLI.
- **Structured progress** (per-layer bytes) — nicer bars for pull, but job-specific code, a copy
  of nerdctl's status collection, and concurrent pulls share containerd's content statuses.

### Where users see jobs

- **Header indicator and panel** — a running-job count in the header and a panel with recent
  jobs, live output and cancel; jobs survive navigation.
- **Only in the dialog that started them** — simpler, but a closed dialog loses the job.

## Decision

Terminal output, header indicator and panel, cancellation from the start.

- `internal/jobs.Manager` runs jobs on a server-lifetime context: `POST /jobs/{id}/cancel`
  cancels one, shutdown cancels all and waits. Status: `running`, `succeeded`, `failed`,
  `canceled`; a panic fails the job instead of the server.
- **Output:** live subscribers get the raw bytes in write order. The stored copy applies the
  progress writer's redraw sequence (`ESC[1A ESC[2K CR`, erase the previous line), so it holds the
  screen as a terminal shows it: a long pull stores one progress table, not every frame. A new
  subscriber first gets that screen (`reset`), then live output. Stored output is capped at
  1 MiB, oldest lines first.
- **Retention:** the last 100 jobs; finished jobs are dropped after an hour. Running jobs are
  never dropped. Nothing survives a restart (persistence comes with the scheduler, #82).
- **Locks:** a job names a resource (`env/<id>/ns/<ns>/image/<ref>` for pulls); a second job on
  it gets `409 conflict` until the first finishes.
- **Authorization:** `jobs:read` lists jobs, but each job is visible and cancelable only for
  callers allowed the action that started it (`images:write` for pulls).
- **Pull:** `POST /environments/{env}/images/pull` runs nerdctl's `image.Pull` (same API in
  nerdctl 2.3 and 2.4, ADR-0017) with mode `always`, the host platform, a chosen one or all
  platforms. nerdctl's log warnings (e.g. skipped TLS verification) go to the job output.
- **UI:** the header shows the running-job count; the Jobs panel lists jobs with status, user
  and time, expands one to its live xterm output, and cancels. Starting a pull opens the panel
  at the new job.

## Consequences

- Jobs started before a restart are gone; a pull interrupted by a restart leaves partially
  downloaded content that containerd reuses on the next pull.
- Progress is only as structured as nerdctl's output: no percentage in the job list.
- New long operations only provide a `jobs.Func` writing terminal output and a resource key.
