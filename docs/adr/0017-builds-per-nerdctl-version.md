# 0017. One build per supported nerdctl minor version

- **Status:** Accepted (amends the release implications of [0011](0011-nerdctl-version-policy.md)
  and the supported set of [0012](0012-containerd-version-support.md))
- **Date:** 2026-11-12

## Context

ADR-0011 requires the host nerdctl to have the same `major.minor` as the nerdctl packages nerdui
links; otherwise nerdui is read-only. A release linked with nerdctl 2.4 is therefore read-only on
hosts that still run nerdctl 2.3 (e.g. 2.3.5 on a VPS), and users would have to upgrade nerdctl
to use nerdui at all.

nerdui builds unchanged against nerdctl 2.3.5: the packages it uses (`cmd/container`, `logging`,
`infoutil`, `labels`, `netutil`, …) have the same API, and the containerd client stays at the
version required by `go.mod` (minimal version selection keeps the newer one).

nerdctl 2.3 declares containerd v1.7–v2.3 compatible; nerdctl 2.4 supports v1.7 or later.

## Options considered

- **A. One build per nerdctl minor** — the strict check of ADR-0011 stays; every build is tested
  with its own nerdctl version.
- **B. One build with a compatibility window** (linked minor and the previous one writable) —
  one artifact, but relaxes ADR-0011 and relies on tests to catch format drift.
- **C. Require users to upgrade nerdctl** — no work, but excludes hosts on the previous minor.

## Decision

Option A, for the **latest two nerdctl minors** (today 2.4 and 2.3).

- `go.mod` links the newest minor. Each older minor has `go.nerdctl-<minor>.mod/.sum`, generated
  from `go.mod` by `make modfiles` with only the nerdctl version changed, committed and checked in
  CI (`make modfiles-check`). Builds select it with `-modfile` (`make build NERDCTL=2.3`,
  `GOFLAGS=-modfile=…`). Code differences, if they ever appear, use build tags.
- The required nerdctl minor and the nerdctl version shown in the UI come from build info, so each
  build checks against its own nerdctl version without configuration.
- **Supported containerd per build** follows the nerdctl release: the ADR-0012 set (latest LTS and
  latest release) limited to the versions that nerdctl release declares compatible. Today:
  `nerdctl2.4` → containerd 2.3 and 2.4; `nerdctl2.3` → containerd 2.3. Other versions run with
  the "unsupported" warning. `/api/v1/version` reports the set (`runtime.supportedContainerd`).
- **CI:** unit tests, `go vet` (also with the integration tag) and govulncheck run for every build;
  the integration matrix has one job per supported (nerdctl build, containerd) pair:
  `Integration (nerdctl 2.4, containerd 2.3)`, `(nerdctl 2.4, containerd 2.4)`,
  `(nerdctl 2.3, containerd 2.3)`, each a required status check. A `goreleaser check` job
  validates the release configuration.
- **Artifacts:** `nerdui_<version>_nerdctl<minor>_linux_<arch>.tar.gz` and images
  `ghcr.io/pessom/nerdui:<version>-nerdctl<minor>`; `<version>` and `latest` point to the newest
  nerdctl build, `latest-nerdctl<minor>` follows stable releases of each build.
- A new nerdctl minor moves `go.mod` forward, turns the previous minor into a variant and drops
  the oldest one (docs/release.md).

## Consequences

- Users pick the archive or image tag for their host nerdctl; the read-only banner names the
  build to use.
- Two builds per release and three integration jobs instead of two.
- Dependabot only updates `go.mod`: its PRs need `make modfiles` before CI passes.
- `third_party` licenses shipped with releases are collected from `go.mod`; the variants differ
  only in the nerdctl version (Apache-2.0). CI runs `make licenses-check` for every build.
- If the APIs diverge, older builds need build-tagged code; when that cost grows, dropping to one
  older minor or option B can be reconsidered.
