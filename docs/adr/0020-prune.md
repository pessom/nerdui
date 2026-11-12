# 0020. Prune: nerdctl's prunes with an estimated preview, no volumes

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

#52 asks to remove unused containers, images, networks and volumes, showing beforehand what
goes and how much space is freed. nerdctl's prune functions (the same in 2.3 and 2.4) have no
dry run and print no reclaimed space. Volumes hold data; nerdui has no volume list yet.

## Options considered

- **A. nerdctl's prunes as a job, estimated preview** — the CLI's rules decide; the preview uses
  what nerdui can compute: stopped containers, and images selected by nerdctl's own exported
  `GetDanglingImages` / `GetUnusedImages`.
- **B. Own prune with an exact dry run** — preview and removal match, but prune rules are
  reimplemented and kept in sync with nerdctl; volumes and networks need lists nerdui lacks.
- **C. nerdctl's prunes without preview** — simplest, but the issue's preview is missing.

## Decision

Option A, without volumes.

- `GET /environments/{env}/system/prune?namespace=&allImages=` (`system:read`) returns stopped
  containers, the images the prune selects with their unpacked sizes (an upper bound: layers are
  shared) and whether a BuildKit daemon is available.
- `POST /environments/{env}/system/prune` (`system:prune`) runs a job with nerdctl's container,
  network, image and build cache prunes in the order of `nerdctl system prune`. Images are
  dangling ones, or all images no container uses with `allImages` (which also prunes all build
  cache, like nerdctl). Networks keep `bridge`, `host` and `none`. A missing BuildKit daemon skips
  the build cache with a note. At the end the job reports how much the image disk usage dropped.
- One prune per namespace at a time; the job is visible only with `system:prune`.
- **No volume pruning:** volumes hold data and cannot be reviewed in nerdui yet; it comes with
  the volume pages.
- The UI offers "Clean up…" on the overview with a namespace choice, category checkboxes, the
  estimate, and opens the job.

## Consequences

- The preview can differ from the result when things change in between, and networks and build
  cache are not previewed; the dialog says so.
- Scheduled prunes (from the issue) come with the scheduler (#82).
