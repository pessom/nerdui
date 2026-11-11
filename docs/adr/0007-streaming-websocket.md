# 0007. Streaming over WebSocket

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

The UI needs live data: container logs (#14) and stack logs (#30), interactive exec (#15),
stats (#16, #63), events (#65) and job progress (#31, #43). Home installations often use
HTTP/1.1 (no proxy), where browsers allow only 6 concurrent connections per origin.

## Options considered

- **WebSocket per stream** — one mechanism for all streams, bidirectional where needed, not
  subject to the HTTP/1.1 per-origin limit, reusable for the v2 agent link.
- **SSE + WebSocket only for exec** — simpler for one-way streams, but SSE hits the 6-connection
  limit over HTTP/1.1 (several tabs with logs and stats freeze); two mechanisms.
- **One multiplexed WebSocket** — fewer connections, but a custom protocol that is harder to debug
  and test.

## Decision

- Every stream is a separate WebSocket endpoint (`.../logs/stream`, `.../exec`,
  `.../stats/stream`, `.../events/stream`, `/api/v1/jobs/{id}/stream`), library
  `github.com/coder/websocket`.
- **Authentication:** session cookie + mandatory same-origin `Origin` check (CSRF protection for
  WebSocket); authorization is checked before upgrade.
- **Message format** (text frames, JSON):
  `{"type": "data" | "error" | "end", "data": ..., "ts": "..."}`.
  **Exec** uses binary frames for stdin/stdout and JSON text frames for control messages
  (`{"type": "resize", "cols": 120, "rows": 40}`).
- **Keep-alive:** ping every 30 s; idle/broken connections are closed and the client reconnects
  with backoff (logs resume from the last timestamp).
- **Backpressure:** each connection has a bounded send buffer; slow clients are disconnected
  instead of blocking containerd readers. Log streams support `tail` and `since` to limit
  initial volume.
- **Events hub:** each environment keeps a single containerd event subscription and fans events
  out in-process (`internal/events`) to WebSocket clients. The UI uses events to invalidate
  TanStack Query caches instead of polling, so changes made with the nerdctl CLI appear within a
  second.
- **Stats** are sampled only for containers that have active subscribers (default every 2 s).

## Consequences

- One client helper (`web/src/lib/ws.ts`) with reconnect logic serves all streams.
- Reverse proxies must allow WebSocket upgrades (documented).
- The same framing can be reused for the server ↔ agent channel in v2.
