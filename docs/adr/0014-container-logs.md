# 0014. Container logs: nerdctl log viewers and xterm.js

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The container page shows logs (#14): the last N lines, live follow, stdout/stderr separation,
ANSI colors, timestamps, filter and search, download. Containers created by nerdctl log
through nerdctl's logging driver (json-file by default, also journald, CRI files for
Kubernetes containers). ADR-0003 reads logs with nerdctl packages; ADR-0007 streams over
WebSocket.

nerdctl's `pkg/cmd/container.Logs` (what `nerdctl logs` runs) cannot serve a web UI:

- a follow stream ends only on SIGINT/SIGTERM (`signal.Notify` on a fresh channel) or when the
  container exits, so a closed browser tab would leave the reader and its file watcher running;
- every call registers a signal handler and never removes it.

## Options considered

### Backend

- **A. nerdctl `pkg/logging` viewers with our own stop channel** — `InitContainerLogViewer` and
  `PrintLogsTo` read every supported driver; nerdui closes the stop channel when the client
  goes away. The follow logic around it (wait for the task, then for the logger) and
  `getLogPath` are copied from `container.Logs` (~60 lines).
- **B. `nerdctl logs -f` subprocess per stream** — identical to the CLI and isolated, but one
  process per open tab, stdout/stderr only through pipes, and a departure from ADR-0003.
- **C. `container.Logs` as is** — rejected for the reasons above.

### Viewer

- **xterm.js** — a real terminal: ANSI colors as programs emit them, fast rendering of large
  outputs, search with highlighted matches (addon), reusable for exec (#15).
- **Virtualized list (@tanstack/react-virtual) + ANSI-to-HTML** — filtering and toggles are
  plain re-renders, but colors and performance are ours to get right.

## Decision

Backend A, viewer xterm.js.

- `runtime.Containers.Logs(ctx, ns, id, opts, emit)` looks up the exact container ID, always
  asks the viewer for timestamps and splits them off into `LogLine{stream, ts, line}`
  (journald writes no timestamps: `ts` is empty). Canceling `ctx` stops the viewer; nerdctl
  checks the stop channel at least once a second. Log driver `none` → 409.
- `WS …/containers/{id}/logs/stream?follow=&tail=&since=` sends batches of lines (up to 500
  lines or 50 ms per frame). The log reader is paced by the client (`ws.Conn.SendDataWait`)
  instead of disconnecting slow clients: unlike the shared containerd event reader, it reads
  a file for this connection only. The stream ends with `end` when the logs are exhausted or
  the followed container stops; the UI resumes it when the container runs again.
- Reconnects resume with `since` = last timestamp. nerdctl filters `since` by whole seconds,
  so the client drops lines up to the last timestamp it has.
- `GET …/containers/{id}/logs?tail=&since=&until=&timestamps=` downloads plain text like
  `nerdctl logs`.
- The browser keeps the last 10 000 lines (xterm scrollback); filter (text or regex, only
  matching lines or highlight only), stream selection and timestamps redraw the terminal from
  that buffer. xterm.js is loaded lazily with the Logs tab (~94 kB gzip).

## Consequences

- `internal/runtime/logs.go` carries Apache-2.0 code from nerdctl (dual SPDX header); updates
  of nerdctl must check `container.Logs` for changes in the follow sequence.
- journald lines have no timestamps: the timestamp toggle shows nothing for them, and a
  reconnect may repeat lines.
- Filtering happens in the browser over the loaded lines; older lines need a larger tail or
  the download.
- If xterm.js makes filtering or accessibility hard (it renders to a canvas-like DOM that
  screen readers do not read line by line), the viewer can be replaced without API changes.
