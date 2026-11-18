# 0029. Image details cache

- **Status:** Accepted
- **Date:** 2026-11-18

## Context

The image list (#42) reports, for every image, the platforms whose content is in the store and
the content and unpacked sizes. Computing them reads the index, the manifests of every platform
and the snapshot usage of every layer from containerd: dozens of gRPC calls per image. The
Install workflow (#129) measured `GET /images` with 300 images at about 0.9 s (amd64) and 1.2 s
(arm64) per request, against NFR-001 (p95 ≤ 300 ms). The list is fetched on every visit and
after every containerd event.

These details depend only on the image's target digest and on what of it is stored; the name,
labels and creation time come from the image record, which is cheap to read.

## Options considered

- **Compute once per digest within a request** — exact, but helps only tags of one image.
- **Cache per namespace and digest with a short lifetime, reset by nerdui's own changes** —
  fast repeated lists; changes made with the CLI that alter stored content of an existing
  digest (pruning, unpacking another platform) show after the lifetime.
- **Invalidate from containerd events (`/content/*`, `/snapshot/*`, `/images/*`)** — exact, but
  ties the runtime client to the event subscription and its reconnects.
- **Load sizes on demand, like volume sizes** — fast, but the image list loses its main column.

## Decision

- Image details (platforms, content size, unpacked size) are cached per namespace and digest for
  5 minutes; each digest is described once per request, in parallel.
- Pull, remove and prune through nerdui reset the namespace's entries, so their results show at
  once. A newly pulled digest is never cached before it exists.
- Changes made outside nerdui to the content of an image that stays (e.g. `nerdctl image prune`
  removing unpacked snapshots) may show with up to 5 minutes delay.

## Consequences

- Repeated lists cost one image service call, one container listing and the cached details.
- The first list after start or after a change still describes every new digest.
- Event-based invalidation can replace the lifetime later without changing the API.
