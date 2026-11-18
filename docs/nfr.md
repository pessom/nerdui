# Non-functional requirements: verification

How each NFR of the [technical specification](requirements/technical-specification.md#6-non-functional-requirements-mvp)
is verified (#129). Most are enforced by tests that run on every pull request; the performance
NFRs are measured by [`scripts/nfr-bench.sh`](../scripts/nfr-bench.sh) on clean hosts by the
[Install workflow](../.github/workflows/install.yml), which also installs each release by the
steps of [deployment.md](deployment.md).

## Verification matrix

| NFR | Requirement | Verified by | Status |
| --- | --- | --- | --- |
| NFR-001 | List latency p95 ≤ 300 ms (500 containers, 300 images) | `nfr-bench.sh` in the Install workflow (amd64, arm64) | [Measurements](#measurements) |
| NFR-002 | Initial load ≤ 1.5 s, initial JS ≤ 500 KB gzip, editor/terminal lazy | `nfr-bench.sh` (initial JS); route-level code splitting in `web/` (CodeMirror, xterm, uPlot are separate chunks) | [Measurements](#measurements) |
| NFR-003 | CLI changes visible ≤ 2 s via events | `TestIntegrationEventsFromNerdctl`, `TestEventsStreamThroughFullStack`; `nfr-bench.sh` (CLI create → event) | [Measurements](#measurements) |
| NFR-004 | Logs: first lines ≤ 1 s, 10k lines/s, slow clients dropped | `TestIntegrationLogs`, `TestContainerLogsStreamFollowAndDisconnect`, `TestSlowConsumerDoesNotBlockProducer`, `TestSlowSubscriberDropped`; `nfr-bench.sh` (throughput) | [Measurements](#measurements) |
| NFR-005 | Idle RSS ≤ 80 MB, CPU ≤ 1 % | `nfr-bench.sh` | [Measurements](#measurements) |
| NFR-006 | CLI interoperability | Install workflow: container created through the API is inspected, logged and removed with `nerdctl`; integration tests use the host `nerdctl` for stacks, networks, volumes | ✅ |
| NFR-007 | TLS by default | `TestSelfSignedGenerateAndReuse`, `TestTLSConfigHandshake`, `TestInsecureCookieWithoutTLS` (explicit opt-out) | ✅ |
| NFR-008 | argon2id, cookies, CSRF, WebSocket origin, rate limits, setup token | `TestLoginAndSessions`, `TestRehashOnLogin`, `TestHTTPFlow` (CSRF), `TestCrossOriginRejected`, `TestLoginRateLimit`, `TestSetupTokenRateLimited`, `TestSetup` | ✅ |
| NFR-009 | Every endpoint declares an action; default deny | `TestRouteAuthorization` (complete route table), `TestProtectUnknownActionPanics`, `TestCatalog` | ✅ |
| NFR-010 | Secrets never logged or returned | Registry passwords and API keys are write-only in the API (`TestRegistries`, API key tests return the key only on creation and store a hash); `TestSecurityHeaders`; internal errors answer a generic message | ✅ |
| NFR-011 | `nerdctl` argument vectors, no shell | Typed specs build argv (`exec.Command(nerdctl, args...)`), gosec in CI; the only shell is the web terminal inside the container | ✅ |
| NFR-012 | Restarting nerdui never changes containers | Install workflow: a running container keeps its start time across `systemctl restart nerdui` | ✅ |
| NFR-013 | Transactional migrations with backup | `TestBackupBeforeMigration`, `TestOpenMigratesAndIsIdempotent` | ✅ |
| NFR-014 | Degraded mode without containerd/nerdctl | `TestVersionsWithoutSocket`, `TestVersionPublicWithoutRuntime`, `TestReadOnlyMode`, `TestContainerActionsReadOnly` | ✅ |
| NFR-015 | JSON logs, `/healthz`, runtime status in `/api/v1/version` | `slog.NewJSONHandler` in `nerdui serve`, `TestHealthz`, `TestVersion` | ✅ |
| NFR-016 | Keyboard navigation, themes, confirmations | Mantine components (focus management, ARIA); light/dark/auto themes saved per user (#90); every destructive action opens a confirmation dialog (UI tests click through them) | ✅ |
| NFR-017 | CI quality gates | Required checks: Web, Go (lint, unit), Integration (nerdctl × containerd matrix), CodeQL, govulncheck, Licenses, GoReleaser config | ✅ |
| NFR-018 | Static binaries amd64/arm64, signed multi-arch image | GoReleaser (`CGO_ENABLED=0`), cosign signatures and SBOMs ([release.md](release.md)); the Install workflow verifies the signature before installing | ✅ |

## Measurements

GitHub-hosted runners (Ubuntu 24.04, nerdctl-full 2.4.2), filled from the Install workflow.
A Raspberry Pi 4-class host is the reference of NFR-001; results from such hosts are added
below when measured.

**v0.1.0-beta.1**, 2026-11-18 ([run](https://github.com/pessom/nerdui/actions/runs/44332499089)),
4 vCPU, 16 GB:

| NFR | Measure | amd64 | arm64 | Target | |
| --- | --- | --- | --- | --- | --- |
| NFR-001 | `GET /containers`, 500 containers, p95 | 105 ms | 124 ms | ≤ 300 ms | ✅ |
| NFR-001 | `GET /images`, 300 images, p95 | 888 ms | 1199 ms | ≤ 300 ms | ❌ → ADR-0029 |
| NFR-002 | Initial JavaScript, gzip | 140 KB | 140 KB | ≤ 500 KB | ✅ |
| NFR-003 | `nerdctl create` → event in nerdui | 407 ms | 1336 ms | ≤ 2 s | ✅ |
| NFR-004 | Logs: first byte | 12 ms | 13 ms | ≤ 1 s | ✅ |
| NFR-004 | Logs: throughput (100k lines) | 1.3 M lines/s | 1.1 M lines/s | ≥ 10k lines/s | ✅ |
| NFR-005 | Idle RSS | 56 MB | 54 MB | ≤ 80 MB | ✅ |
| NFR-005 | Idle CPU | 0.02 % | 0.03 % | ≤ 1 % | ✅ |

The image list described every image's platforms and sizes on every request; they are now
cached per digest ([ADR-0029](adr/0029-image-details-cache.md)). The next release is measured
the same way and replaces this table.

## Measuring on your own host

The script creates 500 stopped containers and 300 image tags of a small image in a temporary
namespace (`nfr-bench-<pid>`), measures and removes everything. It needs root, `curl`, `jq` and
an administrator's API key (user menu → API keys; read-only is enough):

```bash
curl -fsSLO https://raw.githubusercontent.com/pessom/nerdui/main/scripts/nfr-bench.sh
chmod +x nfr-bench.sh
sudo env NERDUI_KEY=nerdui_... ./nfr-bench.sh            # --help for options
```

It loads the host for a few minutes (image pull, 500 `nerdctl create`); on a busy production
host use smaller numbers, e.g. `--containers 100 --images 50`.
