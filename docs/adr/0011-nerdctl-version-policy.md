# 0011. nerdctl version compatibility policy

- **Status:** Accepted (complements [0003](0003-nerdctl-binary-runtime-dependency.md))
- **Date:** 2026-11-11

## Context

nerdui links nerdctl Go packages (version pinned in `go.mod`) and, per ADR-0003, delegates
container creation and compose to the host `nerdctl` binary. nerdui then reads files and labels
written by that binary: log files and log config, `nerdctl/*` labels, network and volume
configuration in nerdctl's data root. nerdctl does not guarantee these internal formats across
minor versions.

## Options considered

1. **Soft:** require 2.x, warn when the minor version differs.
2. **Strict:** require the same `major.minor` as the linked packages.
3. **No checks:** only display the version.

## Decision

Strict policy (option 2).

- On startup nerdui runs `nerdctl version --format json` (via `--nerdctl-path`) and compares the
  client version with the linked nerdctl module version from build info.
- **Same `major.minor`** (patch may differ, e.g. host 2.4.0 with linked 2.4.2): normal mode.
- **Different `major.minor`, missing binary or unparsable version:** **read-only mode** — all
  mutating endpoints return `503 unavailable` with code `nerdctl_incompatible`, and the UI shows a
  banner with the detected and required versions.
- The supported nerdctl version is published in release notes, `README.md`,
  `/api/v1/version` (`runtime.nerdctlRequired`) and on the host info page (#66).
- `--skip-nerdctl-version-check` exists for advanced users and testing; when set, nerdui logs a
  warning at startup and the UI shows a persistent "unsupported configuration" notice.

### Release process implications

- Every nerdctl minor bump (Dependabot group `containerd`) is a dedicated PR that updates
  `go.mod`, the CI integration job (`NERDCTL_VERSION`) and the docs, and results in a new nerdui
  release.
- Integration tests always run against the same nerdctl-full minor version as `go.mod`.

## Consequences

- No silent corruption or misreads caused by format drift between nerdctl versions.
- Users upgrading nerdctl to a new minor must upgrade nerdui as well (or wait for a release);
  documented in the deployment guide.
- Release cadence follows nerdctl minor releases (roughly every few months).
