# nerdui — Technical Specification

- **Status:** Draft
- **Version:** 0.3
- **Last updated:** 2026-11-11
- **Owner:** @pessom

## 1. Purpose and goals

nerdui is a web UI for managing containers on a single Linux host running containerd and nerdctl —
a lightweight, self-hosted alternative to Portainer for home labs and small servers.

Success criteria for MVP (`v0.1`):

- Everyday container, stack (compose), image, volume and network management without the CLI.
- Everything created via nerdui is fully manageable with the `nerdctl` CLI and vice versa.
- Safe to run on a home network: authentication and TLS by default.

## 2. Users and roles

| Role | Description | Permissions (MVP) |
| ---- | ----------- | ----------------- |
| Admin | Owner of the host, created at first run | All actions |
| User (v2+) | Additional household / team members | Defined by RBAC (#78, Later) |

## 3. Scope

### 3.1 In scope (MVP)

The 28 issues in milestone [`v0.1 (MVP)`](https://github.com/pessom/nerdui/milestone/1):
containers (#10–#17), stacks (#24–#26, #28, #30, #31), images (#42–#44, #51, #52), volumes and
networks (#57, #60, #62), host (#63, #65, #66), local users (#74), REST API (#87), dark theme (#90).

### 3.2 Out of scope (MVP)

Everything in milestone [`v0.2`](https://github.com/pessom/nerdui/milestone/2), open issues without a
milestone (Later) and issues closed as `wontfix`; rootless containerd; Windows/macOS
hosts; Docker Engine, Podman, Swarm, Kubernetes.

## 4. Environment and constraints

- **Host OS:** Linux `amd64`, `arm64`; nerdui runs as root on the containerd host.
- **containerd:** officially supported **2.3 (LTS)** and **2.4**, both covered by CI; other
  versions work with an "unsupported" warning ([ADR-0012](../adr/0012-containerd-version-support.md)).
- **nerdctl:** binary installed on the host (rootful) with the **same `major.minor` as the nerdctl
  packages linked into nerdui** (2.4.x for the current `go.mod`), CNI plugins — e.g.
  `nerdctl-full` ([ADR-0003](../adr/0003-nerdctl-binary-runtime-dependency.md),
  [ADR-0011](../adr/0011-nerdctl-version-policy.md)).
- **Network:** HTTPS on port `9443` by default, configurable with `--listen`.
- **Browsers:** last two versions of Chrome, Firefox, Safari, Edge.
- **Deployment:** single static binary or container image ([deployment](../deployment.md)).
- **Architecture:** [docs/architecture.md](../architecture.md), [ADRs](../adr/README.md).

## 5. Functional requirements

Functional requirements are maintained as GitHub issues (one per feature), grouped by milestone
and `area/*` labels (maintainers also use the private project board "nerdui roadmap"). Each MVP issue contains an
**Architecture** section linking to the relevant ADRs and listing API endpoints.

| Area | MVP issues |
| ---- | ---------- |
| Containers | #10, #11, #12, #13, #14, #15, #16, #17 |
| Stacks (Compose) | #24, #25, #26, #28, #30, #31 |
| Images | #42, #43, #44, #51, #52 |
| Volumes & Networks | #57, #60, #62 |
| Host & Monitoring | #63, #65, #66 |
| Access & Security | #74 |
| Platform | #87, #90 |

## 6. Non-functional requirements (MVP)

| ID | Category | Requirement | Target / metric |
| -- | -------- | ----------- | --------------- |
| NFR-001 | Performance | API latency for list endpoints | p95 ≤ 300 ms with 500 containers / 300 images on a Raspberry Pi 4-class host |
| NFR-002 | Performance | Initial UI load on LAN | ≤ 1.5 s; initial JS ≤ 500 KB gzip, editor/terminal lazy-loaded |
| NFR-003 | Freshness | Changes made via nerdctl CLI visible in the UI | ≤ 2 s, driven by containerd events (no polling) |
| NFR-004 | Streaming | Log streaming | first lines ≤ 1 s; sustain 10k lines/s without blocking containerd; slow clients disconnected |
| NFR-005 | Resource usage | nerdui process at idle | RSS ≤ 80 MB, CPU ≤ 1 % of one core |
| NFR-006 | Compatibility | CLI interoperability | objects created by nerdui pass `nerdctl inspect/logs/rm/compose` checks in integration tests |
| NFR-007 | Security | Transport | TLS enabled by default (self-signed generated), opt-out only explicitly |
| NFR-008 | Security | Authentication | argon2id passwords; HttpOnly/Secure/SameSite=Strict session cookie; CSRF token; same-origin check for WebSocket; login rate limiting; first-run setup token |
| NFR-009 | Security | Authorization | every endpoint declares an action; default deny; enforced by tests |
| NFR-010 | Security | Secrets hygiene | passwords, tokens and registry credentials never logged or returned by the API |
| NFR-011 | Security | Process execution | `nerdctl` executed with an argument vector built from typed input; no shell |
| NFR-012 | Reliability | Restart safety | restarting/upgrading nerdui never stops or changes containers |
| NFR-013 | Reliability | Data safety | DB migrations transactional with automatic backup before migrating |
| NFR-014 | Reliability | Degraded mode | nerdui starts without containerd/nerdctl and shows the problem; read-only when nerdctl is missing or its `major.minor` differs from the linked packages |
| NFR-015 | Observability | Logs and health | structured JSON logs (`slog`); `/healthz`; runtime connectivity in `/api/v1/version` |
| NFR-016 | Usability / a11y | Accessibility | keyboard navigation for main flows; light/dark/auto themes; destructive actions require confirmation |
| NFR-017 | Maintainability | Quality gates | CI: lint, unit, integration, CodeQL, govulncheck, license checks must pass |
| NFR-018 | Portability | Distribution | static binary for linux/amd64 and linux/arm64; multi-arch signed image |

## 7. API

- Contract: [`api/openapi.yaml`](../../api/openapi.yaml) (source of truth).
- Model, conventions and long-running jobs: [ADR-0006](../adr/0006-api-model-environments-jobs.md).
- Streaming endpoints: [ADR-0007](../adr/0007-streaming-websocket.md).

## 8. UI / UX

- Stack: [ADR-0010](../adr/0010-frontend-stack.md).
- Navigation: environment selector (only `local` in MVP) → Dashboard, Containers, Stacks, Images,
  Volumes, Networks, Events, Host, Settings (users, registries).
- Every list: search, sort, column visibility, live updates; every destructive action: confirmation
  dialog showing affected objects.

## 9. Security

- Threat model and reporting: [SECURITY.md](../../SECURITY.md).
- Authentication, sessions, TLS: [ADR-0005](../adr/0005-authentication-sessions-tls.md).
- Authorization: [ADR-0009](../adr/0009-authorization.md).

## 10. Acceptance criteria (MVP release)

- All `v0.1 (MVP)` issues closed with tests.
- NFR-001 … NFR-018 verified (performance measured on an arm64 SBC and an amd64 host).
- Integration test suite covers create → inspect → logs → stop → remove for containers and
  up → status → down for stacks, cross-checked with the `nerdctl` CLI.
- Installation docs verified on a clean Debian/Ubuntu host with `nerdctl-full`.

## 11. Milestones

| Milestone | Scope | Target date |
| --------- | ----- | ----------- |
| `v0.1 (MVP)` | 28 issues listed above | TBD |
| `v0.2` | 33 issues: updates, GitOps, templates, notifications, multi-host, SSO/MFA | TBD |

## 12. Open questions

_None at the moment._ Resolved on 2026-11-11:

| Question | Decision |
| -------- | -------- |
| Supported nerdctl versions and enforcement | Same `major.minor` as the linked packages; otherwise read-only mode ([ADR-0011](../adr/0011-nerdctl-version-policy.md)) |
| Default listen port | `9443` (HTTPS), configurable via `--listen` / `NERDUI_LISTEN` |
| First-run protection | One-time setup token printed to the log ([ADR-0005](../adr/0005-authentication-sessions-tls.md)) |
| Supported containerd versions | 2.3 (LTS) and 2.4 in one build, CI matrix; others with a warning ([ADR-0012](../adr/0012-containerd-version-support.md)) |

## 13. Glossary

| Term | Definition |
| ---- | ---------- |
| Environment | A managed containerd host; `local` in MVP, remote agents in v2 |
| Namespace | containerd namespace (`default`, `k8s.io`, …) isolating containers/images |
| Stack | A compose project managed by nerdui: a directory with `compose.yaml` and `.env` |
| Job | A long-running operation (pull, deploy, prune) with streamed output |
