# Changelog

All notable user-facing changes are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Detailed per-release notes are generated from Conventional Commits on GitHub Releases.

## [Unreleased]

### Added

- Builds for nerdctl 2.4 and 2.3: release archives and images carry the nerdctl minor in their
  name (`nerdui_<version>_nerdctl2.3_linux_amd64.tar.gz`, `:<version>-nerdctl2.3`); each build
  reports the containerd versions it supports (ADR-0017).

- Project skeleton: Go backend with containerd client, embedded React UI, `/healthz` and
  `/api/v1/version` endpoints.
- Build, release (GoReleaser, multi-arch image, SBOM, cosign), CI and documentation.
- Licensed under AGPL-3.0-only; "Source code" link in the UI and `source`/`license` fields in
  `/api/v1/version`; third-party licenses shipped with releases.
