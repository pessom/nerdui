# 0030. Image update checks

- **Status:** Accepted
- **Date:** 2026-11-18

## Context

Users want to know when the tag of an image their containers run (e.g. `nginx:1.27`) points to a
newer image in the registry (#53), before automatic updates (#54, #56) act on it. Competitors
compare digests on a schedule; Docker Hub limits pulls, so checks must not pull.

## Options considered

- **Which images:** those used by containers (few requests, what matters) or every tagged image
  (more requests, mostly irrelevant). The user chose images used by containers; any other image
  can be checked on request.
- **How:** a HEAD request of the tag's manifest returns its digest without downloading; Docker
  Hub does not count HEAD requests as pulls. Comparing against the local image's target digest
  works for multi-platform images, which nerdctl stores by their index digest.
- **What to store:** the full status, or only the registry digest and compare when reading.
- **Schedule:** a server flag, or a setting in the UI. The user chose the UI.

## Decision

- Per environment, a background checker resolves every tag used by containers of any namespace
  (digest-pinned references skipped) with nerdctl's resolver: hosts.d, the credentials of the
  user running nerdui, insecure registries with a plain-HTTP fallback, like `nerdctl pull`.
  Four tags at a time, 20 s each.
- The interval is a setting of the environment (Settings → Image updates): off, 1, 6 (default),
  12 or 24 hours. "Check for updates" runs a check at once; one check per environment runs at a
  time. After a restart the schedule continues from the newest stored check.
- Only the registry digest, the error and the time are stored (migration 6, `image_updates`, per
  environment and reference). `updateAvailable` is decided when reading, against the local
  digest at that moment, so pulling the new image clears it without another check.
- Checks only ask registries: they need `images:read` and work in read-only mode. Results are
  shown as badges in the containers, images and stacks lists; nothing is pulled or recreated.

## Consequences

- A tag that moved back to the local digest, or a pulled update, is shown correctly right away;
  a new update shows after the next check.
- Images without a registry (built or loaded locally) record an error and never show a badge.
- Automatic updates (#54) can use the same results.
