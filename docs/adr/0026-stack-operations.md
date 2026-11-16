# 0026. Stack operations with the host's nerdctl compose

- **Status:** Accepted
- **Date:** 2026-11-16

## Context

Stacks are deployed, stopped, started, restarted, taken down and pulled (#28) with live output
(#31). ADR-0008 planned nerdctl's `pkg/composer` library, but it cannot be imported (ADR-0025:
its test dependencies reference a module that only exists inside nerdctl's repository), and
composer itself runs the host's nerdctl binary for most of the work (ADR-0003).

## Options considered

- **Host `nerdctl compose`** — the operation is exactly what the administrator would run on the
  host; one code path for every nerdctl build; nerdctl takes its own compose lock.
- **`pkg/composer` with a `replace` for `mod/tigron`** — a pseudo-version of a nerdctl-internal
  module to maintain on every nerdctl bump, for little gain.

## Decision

- An operation runs `nerdctl <global flags> compose --project-name=<stack>
  --project-directory=<dir> <op> …` in the stack directory, with the environment's address,
  namespace, data root and CNI paths. The project is loaded from the directory's default files,
  like the listing (ADR-0025).
- Operations: `up` (`--detach`, optional `--pull=always` and `--remove-orphans`), `down`
  (optional `--volumes` and `--remove-orphans`), `start`, `stop`, `restart`, `pull`.
- `POST /environments/{env}/stacks/{name}/{op}` (`stacks:deploy`) starts a job (ADR-0006,
  ADR-0018): stdout and stderr go to the job's terminal output and stream over the job's
  WebSocket; the job fails with nerdctl's exit code. One job per stack and namespace at a time
  (409). Unmanaged projects must be adopted first.
- `down` with `volumes` deletes the project's named volumes: the request must repeat the stack
  name in `confirm` (400 otherwise), and the UI asks to type it.
- Operations need a writable nerdctl (ADR-0011).

## Consequences

- Behavior follows the host's nerdctl compose, including its messages and limitations.
- Job output is kept in memory with the job (ADR-0018); the event history (ADR-0021) records the
  resulting container events.
- ADR-0008's use of `pkg/composer` for operations is superseded.
