# 0042. Redeploying stacks with new images, and operations on one service

- **Status:** Accepted
- **Date:** 2026-11-21

## Context

Several paths pull a stack's images and then run `nerdctl compose up`: "Pull images and
deploy", automatic updates of stacks (ADR-0032), stack webhooks (ADR-0041) and Git stacks
pulled with images.

`nerdctl compose up` (2.3 and 2.4) recreates a container only when the hash of the service's
configuration changed. A tag that now points to a newer image does not change the hash, so the
container keeps running the old image. Docker Compose compares image digests; nerdctl does not.

Users also want to act on one service without touching the others (#29). But
`nerdctl compose up|restart|stop|rm <service>` also includes the service's dependencies (the
compose-go default), and `--force-recreate` would recreate those dependencies as well.

## Options considered

- **`up --force-recreate`** after a pull: simple, but it recreates every container in scope,
  dependencies included, even when nothing changed.
- **Compare images in nerdui** and remove only the containers whose image changed; `up` then
  creates them again and starts the rest as they are. Two sources tell nerdui whether an image
  changed:
  - nerdctl 2.4+ records a container's image digest in the `nerdctl/image-digest` label;
  - for nerdctl 2.3, the digest of each image name before and after the pull.
- **Single-service start, stop and restart:** through compose (dependencies included), or on
  the service's containers directly.

## Decision

- `runtime.Redeploy`:
  1. records image digests and runs `compose pull [services]`;
  2. removes the containers in scope whose image digest changed with the pull, or whose
     recorded digest differs from the current one;
  3. runs `compose up [services]`.
  Every path that pulls and deploys a stack uses it: `up` with `pull`, automatic stack updates,
  stack webhooks, and Git stack pulls with image pulls.
- `POST /stacks/{name}/services/{service}/{op}` (`stacks:deploy`) runs as a job on the stack's
  lock:
  - `start`, `stop` and `restart` act on the service's containers. A service without
    containers is created with `compose up <service>`, which starts its dependencies if they
    are stopped.
  - `redeploy` is `Redeploy` for that service.
- The stack page has a menu per service: start or stop, restart, pull image and recreate.

## Consequences

- Pulling and deploying applies new images, and only the services whose image changed are
  recreated.
- On nerdctl 2.3, an image pulled earlier (e.g. with `nerdctl pull`) without recreating is not
  detected; recreating needs a compose change or a down and up. nerdctl 2.4 labels close this
  gap for containers it creates.
- Removed containers lose their writable layer, exactly as with `compose up` recreating them.
