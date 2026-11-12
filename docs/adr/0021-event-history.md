# 0021. Event history in SQLite

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The event feed (#65) shows containerd events (create, start, exit, OOM, delete, image changes)
with filters and a history of N days. The events hub (ADR-0007) already holds one containerd
subscription per environment and fans events out to WebSocket clients; events are not stored.

## Options considered

- **SQLite with retention** — a hub subscriber writes events to nerdui's database (ADR-0004);
  history survives restarts.
- **In-memory ring buffer** — no migration, but no history across restarts and no "N days".
- **Live only** — only what arrives while the page is open.

## Decision

SQLite with retention.

- Migration 00003 adds `events` (environment, time in ms, namespace, topic, kind, action, object
  ID, object name, attributes as JSON).
- `events.Recorder` subscribes to each environment's hub and inserts in batches (200 events or
  every 500 ms). It stores the container name at the time of the event: names are cached per
  container, so `delete` and late `exit` events keep the name after the container is gone.
- Retention: `--events-retention` (default 168h, `NERDUI_EVENTS_RETENTION`), at most 100 000
  events, cleaned at start and hourly. `0` disables recording; the API then reports
  `recording: false`.
- `GET /environments/{env}/events?namespace=&kind=&object=&before=&limit=` (`system:read`):
  newest first, cursor paging by id, `namespace=*` for all namespaces, object matches ID or name.
- The UI's Events page reads this API; new events appear because the environment's live event
  stream already refetches the environment's queries (ADR-0007).

## Consequences

- Events that happen while nerdui is stopped are not recorded; containerd does not replay them.
- A recorder that falls behind the hub (very bursty hosts) skips events and logs a warning.
- The database grows with the event rate; the count cap bounds it (~100 000 rows, tens of MB).
