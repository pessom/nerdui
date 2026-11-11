# 0006. API model: environments, conventions and long-running jobs

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

MVP manages a single local host, but v2 adds multiple hosts with remote agents (#69, #70).
containerd namespaces (`default`, `k8s.io`, …) must be selectable. Several operations take a
long time and need progress (image pull #43, stack deploy #28/#31, prune #52).

## Options considered

- Environment in the path, namespace as a query parameter.
- Environment and namespace both in the path — strict, but long URLs and awkward
  "all namespaces" queries.
- Flat API now, environments later — a breaking change for API and UI.

## Decision

### Resource layout

```
/api/v1/auth/...                      # login, logout, me, setup
/api/v1/users/...                     # local users (admin)
/api/v1/registries/...                # registry logins
/api/v1/jobs/{id}                     # long-running operation status
/api/v1/environments                  # list; MVP returns only "local"
/api/v1/environments/{env}/info
/api/v1/environments/{env}/containers[/{id}[/action]]
/api/v1/environments/{env}/images/...
/api/v1/environments/{env}/volumes/...
/api/v1/environments/{env}/networks/...
/api/v1/environments/{env}/stacks/...
/api/v1/environments/{env}/system/{df,prune}
/api/v1/environments/{env}/.../stream   # WebSocket endpoints, see ADR-0007
```

- `namespace` is a query parameter (default from `--namespace`, `namespace=*` for read-only
  "all namespaces" listings where supported).
- Containers are addressed by ID, ID prefix or nerdctl name — same rules as the CLI.

### Backend structure

- `internal/environment` keeps a registry `env ID → Runtime`. MVP registers one `local`
  environment backed by the containerd socket. In v2, an agent-backed implementation of the same
  interface is added; handlers and UI do not change.
- `internal/runtime.Runtime` is split into small interfaces (`Containers`, `Images`, `Volumes`,
  `Networks`, `Stacks`, `System`, `Events`) so handlers depend only on what they use and are
  testable with fakes.

### Conventions

- JSON, `camelCase` fields; times in RFC 3339 UTC; sizes in bytes.
- Errors: HTTP status + `{"error": {"code": "not_found", "message": "...", "details": {...}}}`.
  containerd/nerdctl errors are mapped to stable codes (`not_found`, `conflict`,
  `invalid_argument`, `unavailable`, `forbidden`).
- Bulk actions return per-item results with HTTP 207-style semantics in a 200 response:
  `{"results": [{"id": "...", "ok": true}, {"id": "...", "error": {...}}]}`.
- MVP list endpoints return full collections (home scale: hundreds of objects); filtering and
  sorting happen client-side. Server-side pagination is added only if needed.
- `api/openapi.yaml` stays the source of truth (#87); it is also served at
  `/api/v1/openapi.yaml`.

### Long-running operations (jobs)

- Operations that can exceed ~1 s (pull, push, stack up/down/pull, prune, container create with
  pull) return `202 Accepted` with `{"job": {"id": "...", "status": "running"}}`.
- Progress is streamed over `WS /api/v1/jobs/{id}/stream` (ADR-0007); final status is available
  at `GET /api/v1/jobs/{id}`.
- MVP keeps jobs in memory (bounded ring buffer of recent jobs and their output); v2 persists
  job history to SQLite together with the scheduler (#82).
- One job per stack at a time (per-stack lock); concurrent requests get `409 conflict`.

## Consequences

- Multi-host can be added without breaking API clients or UI routes.
- A uniform job model gives consistent progress UI for pull, deploy and prune.
