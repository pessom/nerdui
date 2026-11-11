# 0008. Stacks: compose files on disk, nerdctl composer

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

Stacks (#24–#31) are the main way home users run multi-container apps. They must stay compatible
with `nerdctl compose`, be editable by hand and work with Git later (#37–#41).

## Options considered

- **Files on disk + `pkg/composer`** — same behavior as `nerdctl compose`; files editable by hand
  and Git-friendly (Dockge, Dockhand, Arcane do this).
- **Compose files in the database** (Portainer) — easier versioning, but no manual edits and
  awkward Git integration.
- **Files on disk + `nerdctl compose` CLI** — simpler output capture, but diverges from ADR-0001.

## Decision

- A **stack** = a compose project = a directory `<stacks-dir>/<name>/` containing
  `compose.yaml` (or `docker-compose.yml`) and optional `.env`.
  `--stacks-dir` defaults to `<data-dir>/stacks`.
- The stack name is the compose project name (validated with nerdctl's
  `identifiers.ValidateDockerCompat`).
- Operations (`up`, `down`, `restart`, `pull`, `stop`, `start`) use `pkg/cmd/compose.New` /
  `pkg/composer`. As per [0003](0003-nerdctl-binary-runtime-dependency.md), composer executes the
  host `nerdctl` binary for container/volume/network creation. Its stdout/stderr are captured
  into the job output (ADR-0006) to provide live progress (#31).
- **Status** (#24) is computed from containers with label `com.docker.compose.project=<name>`
  compared to services in the compose file: `running`, `partial`, `stopped`, `unknown`
  (project running but no compose file found).
- **Adopt** (#25): nerdui lists compose projects found via labels and scans configured
  directories (`--stacks-scan-dirs`) for compose files. Adopting either registers the external
  directory in place (path stored in SQLite) or copies files into `stacks-dir` — user's choice.
- **Validation** (#26) uses the same compose-go loader as nerdctl before saving; save is atomic
  (write temp file + rename).
- **Concurrency:** one running job per stack (per-stack lock); edits during a running job are
  rejected with `409`.
- nerdui never deletes a stack directory without explicit confirmation; `down` keeps files.

## Consequences

- `nerdctl compose -f <stacks-dir>/<name>/compose.yaml ...` on the host manages the same project.
- Git-based stacks (v2) become "a directory that is a Git checkout".
- Composer depends on nerdctl internal packages; changes are reviewed on every nerdctl bump.
