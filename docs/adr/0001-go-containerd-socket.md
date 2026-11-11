# 0001. Go backend over the containerd socket with nerdctl packages

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

nerdui is a web UI for nerdctl. The backend must list and manage containers, images, volumes and
networks, stream logs and events, and provide interactive `exec`. Objects created through the UI
must remain fully manageable with the `nerdctl` CLI.

containerd only manages containers, tasks, images, content and snapshots. Container names, CNI
networking, port mapping, volumes, log files and Compose are implemented by nerdctl on top of
containerd (labels such as `nerdctl/name`, state under `/var/lib/nerdctl`).

## Options considered

### Integration method

1. **Wrap the nerdctl CLI** (`nerdctl ... --format json`): simple and always CLI-compatible, but a
   process per request, output parsing, and awkward streaming.
2. **containerd gRPC socket only**: fast, streaming events, but CNI/ports/volumes/logs would have
   to be re-implemented and would not be CLI-compatible.
3. **containerd gRPC socket + nerdctl Go packages**: fast and streaming, and CLI-compatible by
   reusing nerdctl's own code. nerdctl packages are not a stable public API.

### Language

- **Go**: official containerd client; nerdctl itself is written in Go and its packages can be
  imported directly.
- **Rust**: lower memory footprint (~10–20 MB less), but only generated gRPC stubs for containerd
  and no nerdctl logic — everything would be re-implemented. Request latency is dominated by
  containerd, not by the backend language, so there is no meaningful speed gain.
- **Python / Node**: generated gRPC stubs only; same re-implementation problem, plus runtime
  dependencies.

## Decision

Go backend that talks to containerd directly over its unix socket using the official client, and
imports nerdctl packages for nerdctl-specific behavior. The React frontend is embedded into the
binary.

## Consequences

- Low latency and native streaming (events, logs, exec).
- CLI compatibility comes from shared code rather than re-implementation.
- nerdui must run on the containerd host (needs `/var/lib/nerdctl`, CNI, netns), with root.
- nerdctl and containerd versions must be pinned and upgraded together; nerdctl internal API
  changes need review on each bump.
