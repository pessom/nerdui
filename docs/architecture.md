# Architecture

This document describes the target architecture for MVP (milestone `v0.1 (MVP)`) and the
extension points reserved for v2. Decisions and their rationale are recorded as
[ADRs](adr/README.md).

## Overview

```
                         ┌──────────────────────── nerdui (single Go binary) ────────────────────────┐
 Browser                 │                                                                            │
 React SPA ──HTTPS/WSS──▶│ HTTP server (TLS)                                                          │
 (Mantine, TanStack)     │   ├─ session auth + CSRF (ADR-0005)                                        │
                         │   ├─ authorizer: per-action checks (ADR-0009)                              │
                         │   ├─ REST /api/v1/...            (ADR-0006, api/openapi.yaml)              │
                         │   ├─ WebSocket streams           (ADR-0007)                                │
                         │   └─ embedded UI (web/dist)                                                │
                         │                                                                            │
                         │ environment registry: env ID → Runtime   ("local" in MVP, agents in v2)    │
                         │   └─ local Runtime                                                         │
                         │        ├─ containerd Go client ───────gRPC unix socket──────▶ containerd  │
                         │        ├─ nerdctl packages (inspect, logs, stats, images, volumes, CNI)    │
                         │        └─ exec of host `nerdctl` (create, compose)  (ADR-0003, ADR-0008)   │
                         │ events hub: one containerd subscription per env, fan-out to WS clients     │
                         │ jobs: long-running operations with streamed output (ADR-0006)              │
                         │ store: SQLite /var/lib/nerdui/nerdui.db  (ADR-0004)                         │
                         └────────────────────────────────────────────────────────────────────────────┘
                                   │                        │
                                   ▼                        ▼
                     /var/lib/nerdui/stacks/<name>   /var/lib/nerdctl, CNI, docker config.json
                     (compose files, ADR-0008)        (owned by nerdctl, shared with the CLI)
```

## Principles

1. **containerd/nerdctl are the source of truth** for runtime objects. nerdui stores only its own
   data and never caches runtime state in its database ([0004](adr/0004-state-storage-sqlite.md)).
2. **Full nerdctl CLI compatibility.** Anything created in nerdui is manageable with `nerdctl`
   and vice versa; operations that record binary paths into containers go through the host
   `nerdctl` ([0001](adr/0001-go-containerd-socket.md), [0003](adr/0003-nerdctl-binary-runtime-dependency.md)).
3. **Secure by default.** TLS on, authentication required, CSRF protection, per-action
   authorization ([0005](adr/0005-authentication-sessions-tls.md), [0009](adr/0009-authorization.md)).
4. **Multi-host ready.** API and UI are scoped by environment from day one
   ([0006](adr/0006-api-model-environments-jobs.md)).
5. **Live, not polled.** containerd events drive UI updates; streams use WebSocket
   ([0007](adr/0007-streaming-websocket.md)).
6. **Single static binary**, no CGO, no external services.

## Backend packages

| Package | Responsibility |
| --- | --- |
| `cmd/nerdui` | Entry point, flags/env, subcommands (`serve`, `admin reset-password`), lifecycle |
| `internal/config` | Configuration model, defaults, validation |
| `internal/server` | HTTP server, TLS, router, middleware (request ID, logging, recovery, auth, CSRF, authz) |
| `internal/api` | REST handlers per resource; request/response types matching `api/openapi.yaml` |
| `internal/api/ws` | WebSocket upgrade, framing, keep-alive, backpressure |
| `internal/auth` | Users, password hashing (argon2id), sessions, setup token, rate limiting |
| `internal/authz` | Action catalog, `Authorizer` interface, MVP admin-allows-all implementation |
| `internal/store` | SQLite access, embedded migrations, repositories |
| `internal/environment` | Registry of environments → `Runtime` |
| `internal/runtime` | `Runtime` interfaces and the local implementation (containerd client, nerdctl packages, nerdctl exec) |
| `internal/stacks` | Stack directories, compose validation, adopt/discovery, composer integration |
| `internal/events` | containerd event subscription and in-process fan-out |
| `internal/jobs` | Long-running operations: registry, output buffers, status |
| `internal/tlsutil` | Self-signed certificate generation and rotation |
| `internal/version` | Build metadata, source URL (AGPL §13), license |
| `web/` | React SPA, embedded via `web/embed.go` |

Dependency direction: `api → (auth, authz, environment, stacks, jobs, events) → runtime → containerd/nerdctl`.
`store` is used by `auth`, `stacks` (adopted paths) and later features; `runtime` never depends on
`api` or `store`.

## Frontend structure

```
web/src/
├── api/          # generated OpenAPI types + typed client
├── lib/ws.ts     # WebSocket client with reconnect and typed messages
├── routes/       # TanStack Router: /env/:env/containers, /env/:env/stacks/:name, ...
├── features/     # containers, stacks, images, volumes, networks, host, auth, users
├── components/   # shared UI (tables, confirm dialogs, status badges, log viewer, terminal)
└── theme/        # Mantine theme, color scheme
```

## Request flow examples

**Stop a container**

1. `POST /api/v1/environments/local/containers/web/stop?namespace=default` with session cookie
   and `X-CSRF-Token`.
2. Middleware: session → principal; authorizer checks `containers:operate`.
3. Handler resolves environment `local` → `Runtime.Containers().Stop(ctx, ns, "web", opts)` via
   containerd/nerdctl packages.
4. containerd emits `/tasks/exit`; the events hub pushes it to subscribed clients; the UI
   invalidates the containers query and re-renders.

**Deploy a stack**

1. `POST /api/v1/environments/local/stacks/media/up` → `202 {"job": {"id": "j_123"}}`.
2. UI opens `WS /api/v1/jobs/j_123/stream` and renders composer output live.
3. `internal/stacks` takes the per-stack lock, runs composer (which executes host `nerdctl`).
4. Job ends with `end` message; events update the stack status.

## Data on disk

| Path | Owner | Content |
| --- | --- | --- |
| `/var/lib/nerdui/nerdui.db` | nerdui | users, sessions, settings, adopted stack paths |
| `/var/lib/nerdui/tls/` | nerdui | self-signed certificate and key |
| `/var/lib/nerdui/stacks/<name>/` | nerdui (editable by user) | `compose.yaml`, `.env` |
| `/var/lib/nerdctl/` | nerdctl | container names, log files, volumes, network configs |
| `$DOCKER_CONFIG/config.json` (root) | nerdctl / nerdui | registry logins shared with `nerdctl login` |

## Runtime constraints

- nerdui runs **on the containerd host, as root**, alongside a **rootful nerdctl** installation
  (rootless is out of scope for MVP).
- Required: containerd 2.x, nerdctl binary with the **same `major.minor` as the linked nerdctl
  packages** (otherwise read-only mode, [ADR-0011](adr/0011-nerdctl-version-policy.md)), CNI
  plugins (e.g. `nerdctl-full`).
- Default port `9443` (HTTPS); change with `--listen`. On bind failure nerdui exits with a clear
  error suggesting `--listen`.
- The containerd connection is lazy: nerdui starts even if containerd is down and reports the
  error via `/api/v1/version` and the UI.

## Configuration (target for MVP)

| Flag | Env | Default |
| --- | --- | --- |
| `--listen` | `NERDUI_LISTEN` | `:9443` (HTTPS) |
| `--address` | `NERDUI_ADDRESS` | `/run/containerd/containerd.sock` |
| `--namespace` | `NERDUI_NAMESPACE` | `default` |
| `--data-dir` | `NERDUI_DATA_DIR` | `/var/lib/nerdui` |
| `--stacks-dir` | `NERDUI_STACKS_DIR` | `<data-dir>/stacks` |
| `--stacks-scan-dirs` | `NERDUI_STACKS_SCAN_DIRS` | empty |
| `--nerdctl-path` | `NERDUI_NERDCTL_PATH` | `nerdctl` from `PATH` |
| `--skip-nerdctl-version-check` | `NERDUI_SKIP_NERDCTL_VERSION_CHECK` | `false` ([ADR-0011](adr/0011-nerdctl-version-policy.md)) |
| `--tls` / `--tls-cert` / `--tls-key` | `NERDUI_TLS*` | `true` / generated |
| `--log-level` | `NERDUI_LOG_LEVEL` | `info` |

## Testing layers

| Layer | Tooling | Location |
| --- | --- | --- |
| Go unit | `go test`, `httptest`, fakes of `Runtime` interfaces | `*_test.go` |
| Store | SQLite in a temp dir, migrations up from empty | `internal/store` |
| Integration | real containerd + nerdctl-full in CI (`integration` build tag) | `*_integration_test.go` |
| Frontend | Vitest, Testing Library, jsdom | `web/src/**/*.test.tsx` |

## Extension points for v2

| v2 feature | Prepared by |
| --- | --- |
| Multiple hosts, agents (#69, #70) | environment registry, `Runtime` interfaces, WS framing |
| RBAC, API keys, restricted roles (#78, #80, #102) | action catalog, `Authorizer`, `users.role` |
| MFA, OIDC (#75, #76) | session issuance in `internal/auth` |
| Scheduler, job history (#82) | `internal/jobs`, SQLite |
| Git stacks (#37–#41) | stacks as directories |
| Reverse proxy mode, ACME | TLS configuration in `internal/server`, `internal/tlsutil` |
