# Changelog

All notable user-facing changes are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Detailed per-release notes are generated from Conventional Commits on GitHub Releases.

## [Unreleased]

### Added

- **Events:** feed of containerd events (container and task changes, exits with status, OOM,
  images, containerd connection) with filters by kind, object and namespace; recorded in the
  database for `--events-retention` (default 7 days) (#65, ADR-0021).
- **Images:** list of the namespace's images with the number of containers using each, unpacked
  and downloaded size, platforms and creation time; search, used/unused filter and sorting
  (#42).
- **Pull images** from the Images page for the host platform, another one or all platforms,
  with live progress as in the terminal (#43).
- **Image actions:** tag, push (as a background job, with the host's registry credentials) and
  remove, one by one or selected images at once, with a force option for images used by
  containers (#44).
- **Registries:** log in to and out of registries from the UI; logins are verified by the
  registry and stored in the host's docker config, shared with `nerdctl login`, and used for
  pulls and pushes (#51, ADR-0019).
- **Background jobs:** long operations run in the background; the header shows running jobs and
  the Jobs panel lists recent ones with their output and a Cancel button (#126, ADR-0018).

## [0.1.0-alpha.1.1] - 2026-11-12

### Fixed

- Choosing a namespace no longer switches to the Overview page: it stays on the current page,
  the Containers menu link keeps it, and the last choice is remembered per environment in the
  browser. The host-wide Overview has no namespace selector (#160).

## [0.1.0-alpha.1] - 2026-11-12

First preview release: manage containers on one Linux host with containerd and nerdctl.

### Added

- **Containers:** list with live updates from containerd events, search (name, image, compose
  project), state filter, sorting, grouping by compose project and namespace selection (#10).
- **Container actions:** start, stop, restart, kill, pause, unpause and remove, also as bulk
  actions on selected containers, with confirmation for destructive ones (#11, #12).
- **Container page:** overview (command, state, restart policy, ports, environment, mounts,
  networks, labels) and the full `nerdctl inspect` JSON with folding, search and copy (#13).
- **Logs:** live stream with follow, last N lines, timestamps, stdout/stderr selection, ANSI
  colors, text or regex filter with highlighted matches, and download (#14).
- **Overview:** containerd, nerdctl and nerdui versions; host (OS, kernel, CPUs, memory, cgroup),
  runtime components (runc, BuildKit, CNI), snapshotters, warnings and containerd plugins (#66).
- **Users:** first administrator created with a one-time setup token from the log; more users
  with temporary passwords that must be changed at the first sign-in; disable, enable and delete
  accounts; change your own password (#74, #121).
- **Security:** HTTPS on port 9443 with a generated self-signed certificate or your own;
  server-side sessions, CSRF protection, login rate limiting; per-action authorization
  (#120–#122).
- **Compatibility checks:** nerdui is read-only when the host nerdctl does not match its build;
  unsupported containerd versions show a warning (#127).
- **Builds for nerdctl 2.4 and 2.3:** release archives and images carry the nerdctl minor in
  their name (`nerdui_<version>_nerdctl2.3_linux_amd64.tar.gz`, `:<version>-nerdctl2.3`). The
  2.4 build supports containerd 2.3 and 2.4, the 2.3 build containerd 2.3 (ADR-0017).
- **Operations:** single static binary with the UI embedded, systemd unit, configuration by flags
  or `NERDUI_*` variables, SQLite state in the data directory with automatic backups before
  migrations, `nerdui healthcheck` and `nerdui admin reset-password` (#118, #119, #137).
- Licensed under AGPL-3.0-only; "Source code" link in the UI, third-party licenses shipped with
  releases; archives and images signed with cosign, with SBOMs.

### Known limitations

- Only containers: images, volumes, networks, stacks and the web terminal come in later
  releases.
- Every user is an administrator; roles are planned (#78).
- The container image is experimental (#145); install the binary with systemd.

[Unreleased]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.1.1...HEAD
[0.1.0-alpha.1.1]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.1...v0.1.0-alpha.1.1
[0.1.0-alpha.1]: https://github.com/pessom/nerdui/releases/tag/v0.1.0-alpha.1
