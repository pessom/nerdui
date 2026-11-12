# 0012. Supported containerd versions

- **Status:** Accepted (supported versions per nerdctl build: [0017](0017-builds-per-nerdctl-version.md))
- **Date:** 2026-11-11

## Context

containerd publishes regular releases (supported ~8 months) and one LTS release per year
(see [containerd RELEASES.md](https://github.com/containerd/containerd/blob/main/RELEASES.md)).
As of November 2026:

| containerd | Status upstream | End of life |
| --- | --- | --- |
| 1.7 | End of life | 2026-09-30 |
| 2.0 | LTS | March 2027 |
| 2.2 | Active | 2026-11-06 |
| **2.3** | **LTS** | **2028-04-30** |
| **2.4** | **Active** (current) | 2027-05-16 |

nerdctl 2.4.x (linked into nerdui, see [0011](0011-nerdctl-version-policy.md)) declares
compatibility with containerd 1.7 and later. Home users either stay on an LTS release for
stability or follow the latest release for new features.

## Options considered

1. **One nerdui build, CI matrix over containerd 2.3 and 2.4** — LTS hosts keep containerd 2.3 and
   install nerdctl 2.4.x.
2. **Two nerdui release lines** (`release/lts` on nerdctl/containerd 2.3, `main` on 2.4) — no
   changes to LTS hosts, but double maintenance; nerdctl itself has no LTS and its 2.3 branch is
   barely updated.
3. **Same as 1, LTS line after nerdui 1.0.**

## Decision

Option 1.

- **Officially supported:** the **latest containerd LTS** and the **latest containerd release** —
  currently **2.3** and **2.4**. A single nerdui build supports both; the host runs the nerdctl
  minor required by [ADR-0011](0011-nerdctl-version-policy.md) (2.4.x today).
- **Everything else** (2.0, 2.2, 1.7, …): nerdui works without restrictions but shows a
  persistent "unsupported containerd version" warning in the UI and in the startup log;
  no CI coverage, issues are handled best effort.
- **CI:** the integration job runs as a matrix — containerd 2.3 (LTS, installed over
  nerdctl-full) and 2.4 (bundled with nerdctl-full). Each matrix entry is a required status
  check (`Integration (containerd 2.3)`, `Integration (containerd 2.4)`), and the tests assert the
  daemon version (`NERDUI_TEST_EXPECT_CONTAINERD`).
- `/api/v1/version` reports `runtime.containerdSupported` (bool) and the host info page (#66)
  shows the supported versions.

### Updating the matrix

- When containerd ships a new minor release, add it to the matrix and drop the previous
  non-LTS one.
- When a new LTS is designated, it replaces the previous LTS once nerdui has a release tested
  against it.
- Matrix changes update this ADR's table, the technical specification, deployment docs and the
  required status checks of the `main` ruleset.

## Consequences

- LTS users can stay on containerd 2.3 until April 2028; they only need nerdctl 2.4.x.
- CI time grows by one integration job.
- The support policy is visible to users (UI, API, docs) instead of being implicit.
