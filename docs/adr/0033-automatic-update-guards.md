# 0033. Delay and approval of automatic updates

- **Status:** Accepted
- **Date:** 2026-11-19

## Context

Automatic updates (#54, ADR-0032) apply a new image as soon as a scheduled check sees it. A
freshly pushed tag is sometimes broken and fixed within hours, and one check can find updates for
many containers at once (a base image moved), which replaces most of a host at once (#56).
Arcane offers a minimum age of new images and a confirmation before bulk updates.

## Options considered

- **Age of an image:** the image's creation time (set by its builder, needs the config blob) or
  when nerdui first saw the registry digest (no extra request, and what users mean by "wait a day
  after it appears").
- **Bulk confirmation:** a manual "Update all" with a dialog (does not guard automatic updates),
  or a threshold above which automatic updates wait for an administrator. The user chose the
  threshold.

## Decision

- `image_updates` records when the registry digest was first seen (migration 8, `seen_at`). A new
  digest restarts it; a failed check keeps the digest and its time.
- Per environment settings (Settings → Image updates, `/settings/auto-updates`):
  - **delay**: at once (default), 1 or 6 hours, 1, 3 or 7 days since the digest was first seen;
    updates not old enough are left for a later check, without a log entry;
  - **approval threshold**: never, or more than 1, 3, 5 (default), 10 or 20 updates due in one
    run of the environment (all namespaces). Above it nothing is applied; the due updates are
    shown as waiting for approval.
- "Approve and update" (`POST /auto-updates/approve`, `settings:write`: it acts on many
  containers and stacks at once) plans the updates again and applies all of them in the
  background, as jobs logged like any automatic update. One run of an environment at a time
  (409 otherwise).
- Waiting updates are kept in memory: after a restart the next check finds them again.

## Consequences

- With the defaults, small numbers of updates apply as before; a host-wide change waits for a
  person.
- Approving does not bypass the delay: what is approved is re-planned with the current settings.
