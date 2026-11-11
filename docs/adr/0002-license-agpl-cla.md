# 0002. License: AGPL-3.0-only with a Contributor License Agreement

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

nerdui primarily targets home and self-hosted use. The monetization model is not decided yet
(sponsorship, paid features, commercial licenses for vendors are all possible). The license
chosen now must keep these options open and prevent closed-source clones, while staying
compatible with Apache-2.0 dependencies (containerd, nerdctl) that are linked into the binary.

Key facts:

- The copyright holder can always relicense their own code; contributions from others without a
  CLA would block that.
- Already released versions keep their license forever. Moving to a more permissive license later
  is easy; moving to a more restrictive one is hard and damages trust.

## Options considered

| Option            | Monetization options                             | Adoption                                  |
| ----------------- | ------------------------------------------------ | ----------------------------------------- |
| Apache-2.0 + CLA  | Open core; anyone may sell closed forks          | Highest; CNCF standard                    |
| MIT               | Same as Apache, no patent grant                  | Highest                                   |
| AGPL-3.0 + CLA    | Dual licensing, open core; no closed clones/SaaS | Lower in companies — irrelevant for home users |
| FSL / BSL         | Strong protection                                | Not open source; community skepticism     |

## Decision

- License: **AGPL-3.0-only** (not "-or-later", so future license versions are not applied
  automatically).
- **CLA** for all external contributions, enforced by `contributor-assistant/github-action`;
  signatures are stored in the `cla-signatures` branch.
- SPDX headers in all source files; CI rejects dependencies with AGPL-incompatible licenses.
- AGPL-3.0 §13 compliance: the UI and `/api/v1/version` expose the source URL of the running
  build (`internal/version.Source`, overridable via `-ldflags`).
- GitHub Sponsors as the first funding channel.
  _Update 2026-11-11: the Sponsor button is deferred until the v0.1 release (#116)._

## Consequences

- Home users are unaffected; anyone hosting a modified nerdui must publish their changes.
- The maintainer can sell commercial licenses or proprietary add-ons thanks to the CLA.
- Some companies will not adopt AGPL software; acceptable for the target audience.
- The license can later be relaxed (e.g. to Apache-2.0) for future releases if adoption matters
  more than protection.
- The CLA text is a template and must be reviewed by a lawyer before commercialization.
