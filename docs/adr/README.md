# Architecture Decision Records

We record significant architectural decisions as ADRs
([Michael Nygard format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions)).

- Copy [`0000-template.md`](0000-template.md) to `NNNN-short-title.md` with the next number.
- An ADR is immutable once accepted; to change a decision, write a new ADR that supersedes or
  amends it.

| #    | Title                                                                                   | Status                |
| ---- | --------------------------------------------------------------------------------------- | --------------------- |
| 0001 | [Go backend over the containerd socket](0001-go-containerd-socket.md)                   | Accepted, amended by 0003 |
| 0002 | [License: AGPL-3.0-only with CLA](0002-license-agpl-cla.md)                             | Accepted              |
| 0003 | [nerdctl binary is a required runtime dependency](0003-nerdctl-binary-runtime-dependency.md) | Accepted         |
| 0004 | [State storage: SQLite in /var/lib/nerdui](0004-state-storage-sqlite.md)                | Accepted              |
| 0005 | [Authentication, sessions and transport security](0005-authentication-sessions-tls.md)  | Accepted              |
| 0006 | [API model: environments, conventions and jobs](0006-api-model-environments-jobs.md)    | Accepted              |
| 0007 | [Streaming over WebSocket](0007-streaming-websocket.md)                                 | Accepted              |
| 0008 | [Stacks: compose files on disk, nerdctl composer](0008-stacks-compose.md)               | Accepted              |
| 0009 | [Authorization layer with per-action permissions](0009-authorization.md)                | Accepted              |
| 0010 | [Frontend stack](0010-frontend-stack.md)                                                | Accepted              |
| 0011 | [nerdctl version compatibility policy](0011-nerdctl-version-policy.md)                  | Accepted              |
| 0012 | [Supported containerd versions](0012-containerd-version-support.md)                     | Accepted              |
| 0013 | [Container inspect via nerdctl's container.Inspect](0013-container-inspect-via-nerdctl.md) | Accepted |
| 0014 | [Container logs: nerdctl log viewers and xterm.js](0014-container-logs.md) | Accepted |
| 0015 | [Host and runtime information via nerdctl infoutil](0015-host-and-runtime-info.md) | Accepted |
| 0016 | [User management: temporary passwords, disabling, last-admin guard](0016-user-management.md) | Accepted |
| 0017 | [One build per supported nerdctl minor version](0017-builds-per-nerdctl-version.md) | Accepted |
| 0018 | [Background jobs: terminal output, retention, cancellation](0018-jobs-terminal-output.md) | Accepted |
| 0019 | [Registry credentials in the shared docker config](0019-registry-credentials.md) | Accepted |
| 0020 | [Prune: nerdctl's prunes with an estimated preview, no volumes](0020-prune.md) | Accepted |
| 0021 | [Event history in SQLite](0021-event-history.md) | Accepted |
