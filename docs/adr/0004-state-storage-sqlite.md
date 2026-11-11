# 0004. State storage: SQLite in /var/lib/nerdui

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

nerdui needs to persist its own data: users, sessions, settings; later (v2) audit log, schedules,
job history, environments/agents, notification channels. Runtime objects (containers, images,
volumes, networks, compose projects) already live in containerd and nerdctl.

## Options considered

- **SQLite (pure Go, `modernc.org/sqlite`)** — single file, SQL queries for audit/history,
  transactions; no CGO, so the binary stays static.
- **bbolt** — reliable KV store used by containerd/Portainer, but every query/filter is manual.
- **YAML/JSON files** — simplest, but no transactions or concurrent access; does not scale to v2.
- **Inside nerdctl's data root** (`/var/lib/nerdctl/...`) — internal nerdctl layout keyed by
  containerd address hash; surprising for backups/uninstall.
- **Labels in containerd metadata** — would expose password hashes/tokens to anyone with socket
  access (`nerdctl inspect`), size-limited, no queries.

## Decision

- **Source of truth for runtime state is containerd/nerdctl.** nerdui never copies containers,
  images, volumes, networks or compose projects into its database; it reads them live and reacts
  to containerd events ([0007](0007-streaming-websocket.md)).
- nerdui's own data lives in **SQLite via `modernc.org/sqlite`** at `<data-dir>/nerdui.db`.
- **Data directory:** `/var/lib/nerdui` (FHS), overridable with `--data-dir` / `NERDUI_DATA_DIR`.
  Created with mode `0700`; database file `0600`.

  ```
  /var/lib/nerdui/
  ├── nerdui.db          # SQLite (WAL mode)
  ├── tls/               # self-signed certificate (ADR-0005)
  └── stacks/<name>/     # compose projects as plain files (ADR-0008)
  ```

- **Schema migrations** are plain SQL files embedded in the binary (`internal/store/migrations`)
  and applied on startup in a transaction; forward-only. Before applying migrations, the database
  is copied with `VACUUM INTO nerdui.db.bak-<version>`.
- **Access pattern:** WAL mode, `busy_timeout`, a single writer connection plus a read pool;
  repositories in `internal/store` expose typed methods, handlers never write SQL.
- **Registry credentials** (#51) are *not* stored in SQLite: nerdui reads/writes the same
  Docker-compatible `config.json` that `nerdctl login` uses, so CLI and UI share logins.
- Secrets that must be stored by nerdui later (stack secrets #35, notification tokens #86) are
  encrypted with a key in `<data-dir>/secret.key` (`0600`), generated on first use.

## Consequences

- No external database to operate; backup = copy the data directory (or `VACUUM INTO`, #89).
- Changes made via the nerdctl CLI are always visible in the UI — there is no cache to go stale.
- Migrations must stay backward-compatible with the previous release for downgrade safety.
