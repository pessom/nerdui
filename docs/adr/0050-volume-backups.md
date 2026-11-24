# 0050. Backups of volumes

- **Status:** Accepted
- **Date:** 2026-11-24

## Context

Volumes hold the data of containers: databases, uploads, configuration (#59). nerdui could
browse and download them (ADR-0045) but not back them up on a schedule or restore them.
Arcane backs volumes up to S3 with selective restore; Dockhand uses restic and SFTP.

## Options considered

- **How a volume is read**: a temporary container that tars the volume (works with any
  volume driver, but pulls an image and needs a writable runtime for every backup), or reading
  the volume's directory on the host like the volume browser (no image, same confinement with
  `os.Root`).
- **Where archives go**: a host directory, S3 (one protocol for AWS, MinIO, Backblaze B2,
  Cloudflare R2, Wasabi…), restic (deduplication and many backends, but an external binary and
  its own repository format), SFTP.
- **Restore**: unpack into the live directory, or next to it and swap.
- **Consistency**: copy a running database as it is, or stop the containers using the volume.

## Decision

- An archive is a gzip-compressed tar of the volume's directory read on the host, the same
  format as Download of the whole volume: one top directory, owners, modes, times and symlinks
  kept. It is named `<namespace>/<volume>/<volume>-<YYYYMMDD-HHMMSS>.tar.gz` in its target.
- **Backup targets** are a setting: a directory on the host, or an S3 bucket (endpoint, region,
  bucket, prefix, access key, path-style addressing) through `minio-go`. The S3 secret key is
  encrypted with nerdui's key (ADR-0049) and never returned. A target can be tested (write,
  list, delete a small object) and cannot be deleted while schedules use it; its archives stay.
- Backups run as jobs, from the volume's Backups dialog or from a `volume.backup` schedule
  (ADR-0048) with a target and the number of archives to keep (default 7): after a backup the
  oldest archives of the volume beyond that number are removed. Archives stream to S3 in
  multipart uploads; nothing is buffered on disk.
- Optionally the running containers that use the volume are **stopped** during the backup
  and started again afterwards, also when the backup fails.
- **Restore** replaces a volume's contents with an archive, or creates a new volume with
  another name. The archive is unpacked next to the volume's directory (same file system) and
  swapped in, so a failed restore leaves the volume unchanged. Running containers using the
  volume are refused unless they are to be stopped and started again.
- Listing, backing up and restoring need `volumes:write`, like reading volume contents.

## Consequences

- Volumes of other drivers than nerdctl's local one are not supported, as with the browser.
- Without stopping containers, a database's files may be copied mid-write; the dialog and the
  schedule offer stopping for consistent copies, at the cost of a short downtime.
- Archives are not encrypted by nerdui; S3 server-side encryption and bucket policies apply.
  Restic and SFTP may be added later as other kinds of targets.
- A schedule migration rebuilds the `schedules` tables to allow the new kind; runs are kept.
