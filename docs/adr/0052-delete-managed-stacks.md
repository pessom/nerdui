# 0052. Deleting stacks created in nerdui

- **Status:** Accepted
- **Date:** 2026-11-24

## Context

Stacks created in nerdui live in the stacks directory (ADR-0008). They could be taken down but
not deleted: forgetting is only for adopted directories (#25), and Git stacks have their own
removal (#37). Removing a directory by hand left the stack's variables and secrets
(ADR-0049) in the database, and a new stack with the same name received them (#248).

## Options considered

- **Refuse while the stack has containers** (as Git stacks do): the user runs Down first, then
  deletes. Two steps, and the files are gone only when the user remembers the second one.
- **Down and delete in one job**: compose down (optionally with volumes), then the directory
  and the variables.

## Decision

- `DELETE /environments/{env}/stacks/{name}?confirm=<name>&volumes=` runs a `stack.delete`
  job: `nerdctl compose down --remove-orphans` (`-v` with `volumes`), then the stack's
  directory is removed with all its files, then its variables.
- The stack name must be repeated as confirmation; removing volumes is a separate choice.
- A failed down stops the job before anything is deleted, so the stack can be fixed and
  deleted again.
- Stacks in the stacks directory and Git stacks. For a Git stack the job removes the clone,
  its `.env` and the Git settings with the webhook; the repository is not touched. The stack
  page shows Delete for both, and the old Git-only removal (`DELETE …/git`, which refuses a
  stack with containers) stays in the API but not in the UI. Adopted stacks are forgotten
  instead (409).
- Down takes a stop icon, not a trash can: it removes containers but no files.
- It needs `stacks:write` and a writable runtime, and waits for other jobs of the stack (409).

## Consequences

- Schedules that target the stack (redeploy, restart) are kept and fail until they are
  changed or removed.
- Volumes that are not named volumes of the compose project, and images, are not touched.
