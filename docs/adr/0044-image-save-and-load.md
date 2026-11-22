# 0044. Saving and loading images as tar archives

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

Hosts without registry access get images as tar archives: `nerdctl save` on a connected host,
`nerdctl load` on the target (#46). nerdui should offer both without shell access.

nerdctl's `image.Save` writes progress to the process's stderr and may fetch missing content
from the registry. Its `load.FromArchive` checks the process's stdin and fails when nerdui runs
as a service, whose stdin is empty.

## Options considered

- **Run the nerdctl CLI** with the archive piped through: another process per request and
  output parsing.
- **nerdctl packages:** see above; `FromArchive` cannot read from a stream in a service.
- **containerd's transfer service directly**, with the same options as nerdctl: an export
  stream for the host's platform, an import stream that unpacks into the configured snapshotter
  and names unnamed images like nerdctl (`import-<date>@<digest>`).
- **Load as a background job** after uploading to a temporary file, or **in the request** while
  the upload streams.

## Decision

- `GET /images/save?ref=…` streams one archive of 1 to 50 images for the host's platform. It
  checks that every image exists first, so a missing one is a 404; an error after the archive
  started aborts the connection, so the browser reports a failed download, not a truncated
  file. It needs `images:write` (like push: the content leaves the host) and works in read-only
  mode, as it only reads containerd.
- `POST /images/load` imports the request body (tar, optionally gzip) while it uploads, through
  the transfer service, and answers with the images created or updated. No temporary file and
  no size limit beyond the host's disk; an archive that cannot be read is a 400. It needs
  `images:write` and a compatible nerdctl, like the other image changes.
- The images page has Load (upload with progress) and Save as tar in the image menu and for
  selected images.

## Consequences

- Archives are compatible with `nerdctl load` and `docker load`, and nerdui loads archives from
  `docker save`.
- Like `nerdctl load`, an archive with an unnamed top-level index also adds an
  `import@sha256:…` reference.
- A reverse proxy in front of nerdui must allow large request bodies for loads (nginx
  `client_max_body_size`).
