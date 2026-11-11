# Architecture

## Overview

```
 Browser ──HTTP/WS──▶ nerdui (single Go binary) ──gRPC (unix socket)──▶ containerd
                      │                                                  │
                      ├─ embedded React UI (web/dist)                    ├─ runc / shims
                      ├─ REST API /api/v1 (api/openapi.yaml)             └─ snapshotters
                      └─ internal/runtime
                           ├─ containerd Go client
                           └─ nerdctl packages ──▶ /var/lib/nerdctl, CNI (/etc/cni, /opt/cni/bin)
```

## Key decisions

- **Direct containerd socket, not the nerdctl CLI** — lower latency, streaming APIs (events, logs,
  exec), no process spawning per request. See [ADR-0001](adr/0001-go-containerd-socket.md).
- **nerdctl packages as a library** — containerd itself does not implement container names, CNI
  networking, port mapping, volumes, log files or Compose; nerdctl does. Reusing its packages keeps
  everything created via nerdui compatible with the `nerdctl` CLI.
- **Single binary** — the frontend is embedded with `go:embed`, so deployment is one file
  (or one image).
- **Contract-first API** — `api/openapi.yaml` is the source of truth; TypeScript types are generated.

## Repository layout

| Path                      | Purpose                                                            |
| ------------------------- | ------------------------------------------------------------------ |
| `cmd/nerdui`              | Entry point: flags/env, logging, HTTP server lifecycle             |
| `internal/runtime`        | containerd client + nerdctl integration. The only package that talks to containerd |
| `internal/server`         | HTTP router, handlers, middleware, SPA serving                     |
| `internal/version`        | Build metadata injected via `-ldflags`                             |
| `web/`                    | React + Vite + TypeScript frontend; `web/embed.go` embeds `dist/`  |
| `api/openapi.yaml`        | HTTP API contract                                                   |
| `deploy/`                 | Deployment examples                                                 |
| `docs/`                   | Documentation, ADRs, requirements                                   |
| `scripts/`                | Helper scripts (commit message check, …)                            |

## Runtime constraints

- nerdui must run **on the same Linux host** as containerd: nerdctl packages need local access to
  `/var/lib/nerdctl`, CNI binaries/configs and network namespaces — not only to the socket.
- It requires root (or equivalent capabilities) for the same reasons as `nerdctl`.
- The containerd connection is lazy: nerdui starts even if containerd is down and reports the error
  via `/api/v1/version`.

## Testing layers

| Layer       | Tooling                                   | Location                         |
| ----------- | ----------------------------------------- | -------------------------------- |
| Go unit     | `go test`, `httptest`, fakes of `Runtime` | `*_test.go`                      |
| Integration | real containerd + nerdctl-full in CI      | `*_integration_test.go` (`integration` tag) |
| Frontend    | Vitest, Testing Library, jsdom            | `web/src/**/*.test.tsx`          |
