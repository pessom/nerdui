# 0045. Browsing and downloading volume contents on the host

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

Users want to look into a volume and download files or the whole volume (#58), e.g. to check a
configuration or take a copy of data. Dockhand does it through a temporary container that
mounts the volume. nerdui runs on the host as root, and nerdctl keeps a named volume's data in
`<data-root>/<address hash>/volumes/<namespace>/<name>/_data`.

## Options considered

- **Temporary container** with the volume mounted (`busybox`, `tar` inside): needs an image on
  the host (not on air-gapped hosts until loaded), is slower and leaves containers behind when
  nerdui stops at the wrong moment.
- **Read the data directory on the host**, confined to it.
- **Scope:** read-only (list, preview, download), or also upload and delete.

## Decision

- nerdui reads the volume's data directory directly, through Go's `os.Root`: every path is
  resolved inside the directory, and `..` or symlinks that leave it are refused (400). Absolute
  symlinks point into a container's file system, not the host's, so they are not followed.
- Read-only: directory listings (directories first, at most 5000 entries), a preview of the
  first 256 KiB of a text file, downloads of a file as is and of a directory or the whole
  volume as `.tar.gz` (symlinks kept as links, devices and sockets skipped).
- The endpoints need `volumes:write`, not `volumes:read`: a volume's files may hold secrets
  (database files, keys), which a future read-only role should not see.
- Errors after a download started abort the connection, so the client sees a failed download.

## Consequences

- Works on air-gapped hosts and without starting containers.
- Files are read while containers may be writing them: a copy of a running database may be
  inconsistent; stop the container for a consistent copy (noted in the UI help).
- Uploads and deletes are not offered; they would need a separate decision.
