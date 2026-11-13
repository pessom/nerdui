# 0022. Web terminal: containerd exec over WebSocket

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

#15 asks for an interactive terminal in containers (shell and user choice, resize, copy). It is
the riskiest feature: it gives a shell inside any running container. nerdctl's `container.Exec`
uses the process console (`consoleutil.Current`) and cannot serve a web client. ADR-0007
reserves binary WebSocket frames for exec; ADR-0009 has a separate `containers:exec` action.

## Options considered

### Process

- **containerd `task.Exec` with a terminal** — the same calls nerdctl makes (`cio.WithStreams`,
  `cio.WithTerminal`, `process.Resize`), with the WebSocket as stdin/stdout; no extra process.
- **`nerdctl exec -it` subprocess under a PTY** — identical to the CLI, but a process and a PTY
  library per session, resize through the PTY.

### Session lifetime

- **Ends with the WebSocket** — nothing keeps running in the container after the tab is gone.
- **Persistent sessions with replay** (Komodo) — reconnect after network drops, but lifecycle,
  limits and cleanup of abandoned shells.

## Decision

containerd `task.Exec`; sessions end with the WebSocket; a server switch.

- `WS /environments/{env}/containers/{id}/exec?cmd=&user=&workdir=&cols=&rows=`
  (`containers:exec`). Binary frames carry input and output; the client sends
  `{"type":"resize"}` text frames. The session ends with `{"exitCode": n}` and `end`.
- The process spec comes from the container's spec (environment, working directory, user);
  `user` is resolved in the container like `nerdctl exec --user` (`oci.WithUser`,
  `WithAdditionalGIDs`), `TERM=xterm-256color` is set, and without `cmd` bash runs when the image
  has it, sh otherwise.
- Closing the WebSocket cancels the session: the process is killed (SIGKILL) and deleted.
- Output is paced to the client (`SendBinaryWait`) instead of disconnecting it.
- `--disable-exec` / `NERDUI_DISABLE_EXEC` turns the terminal off server-wide (403); the
  environment status reports `features.exec` so the UI explains it.
- Every session is logged (user, container, command, exec user, exit) until the audit log
  (#79) exists.
- UI: a Terminal tab on the container page (xterm.js, lazy, shared with logs) with command,
  user and working directory; "New session" starts another process.

## Consequences

- A dropped connection ends the shell and what runs in it; long jobs belong in `nohup` or a
  container, not a browser tab.
- Until roles exist (#78), every user can open terminals; `--disable-exec` is the way to forbid
  it on a server.
- Only one terminal per page; more sessions need more browser tabs.
