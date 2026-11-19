# 0032. Automatic updates

- **Status:** Accepted
- **Date:** 2026-11-19

## Context

Image update checks (#53, ADR-0030) find tags that point to newer images; recreating a container
with a pulled image is available on request (#18, ADR-0031). Users want nerdui to do both on its
own for chosen containers (#54), as Arcane, Dockhand and Komodo do. An update replaces a running
container, so it must never happen to a container nobody chose, and must leave a trace.

## Options considered

- **How to opt in:** a label on the container or compose service, a flag stored in nerdui's
  database, or an opt-out label (Arcane). The user chose opt-in. A label travels with the
  container through recreates, is visible in `nerdctl inspect`, and works the same way in compose
  files, where a database flag would need its own UI per stack and be lost on `compose up` by
  hand. The container form shows the label as a switch.
- **When:** a separate cron schedule, or right after each scheduled image check. A second
  schedule adds a setting without a clear benefit; updates follow the check.
- **Stacks:** recreate each container by hand (breaks compose's own state), or run
  `nerdctl compose pull` and `up -d` for the opted-in services, which also keeps dependency
  order.
- **Delay and confirmation** before updating (#56) are separate work.

## Decision

- `nerdui.auto-update=true` (also `1`, `yes`, `on`) on a container or compose service opts it
  in. "Update automatically" in the create and edit forms sets it.
- After each successful scheduled check of an environment (not a check started from the UI),
  nerdui updates, one at a time, the opted-in containers whose image tag has a newer registry
  digest than the local image:
  - a standalone container is recreated with its current settings after a pull, with the
    rollback of ADR-0031; a container with settings the form cannot express is skipped;
  - the opted-in services of a stack nerdui knows the compose file of get `compose pull` and
    `compose up -d` with those services; unknown stacks are skipped.
- Every update runs as a job (user "nerdui (auto-update)", locked like a manual operation) and
  is logged in the database (migration 7, `auto_updates`): updated, failed or skipped, with the
  reason. A skip is logged once per target, reason and digest. The log is kept for 90 days and
  shown under Settings → Image updates (`GET /auto-updates`, `containers:read`).

## Consequences

- Nothing changes for containers without the label; turning the checks off also stops updates.
- Whether an update is due is decided from the namespace's image, not the image a container was
  created from: after a manual pull without recreating, containers on the old image are not
  updated automatically until the tag moves again.
- A failed container update is rolled back and retried after the next check.
