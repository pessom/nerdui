# 0015. Host and runtime information via nerdctl infoutil

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The environment overview shows host and runtime information (#66): OS, kernel, CPUs, memory,
cgroup version and driver, snapshotters, versions of containerd, runc, BuildKit and the CNI
plugins, `nerdctl info` warnings and the containerd plugin list. ADR-0003 reads runtime state
with nerdctl packages; ADR-0006 groups host-level calls under `runtime.System`.

## Options considered

- **A. nerdctl `pkg/infoutil` plus own version probes** — `infoutil.Info` is what
  `nerdctl info` prints (host fields, cgroup capabilities, warnings); the containerd
  introspection service gives plugins and deprecation warnings. runc, buildctl and the CNI
  bridge plugin are asked for their versions by nerdui with a timeout, because nerdctl's
  helpers run them without one and only log failures.
- **B. `nerdctl info` / `nerdctl version --format json` subprocesses** — identical to the CLI,
  but a departure from ADR-0003, two processes per request and no plugin list.
- **C. Collect everything ourselves** (`/proc`, `uname`, cgroupfs) — full control over fields,
  but repeats nerdctl's code, including the moby sysinfo checks.

## Decision

Option A.

- `GET /api/v1/environments/{env}/info` (`system:read`) returns `runtime.Info`: host,
  features, components (`containerd`, `runc`, `buildctl`, each with an `error` when unknown),
  CNI (directory, plugin binaries, bridge version), warnings, deprecations and plugins with
  status `ok`, `skip` or `error` as `ctr plugins ls` classifies them.
- Host binaries are probed concurrently, 5 s each. The bridge plugin prints its version when
  run without `CNI_COMMAND`.
- `GET /api/v1/version` keeps the short version summary; the overview page adds Host and
  Runtime cards, warnings and a filterable plugin table. The client caches the info for a
  minute.

## Consequences

- Host fields (OS, kernel, CPUs, memory, hostname) describe the machine nerdui runs on. That
  is the containerd host in the supported deployment (ADR-0003); when nerdui runs in a
  container, the distribution and hostname are the container's. A remote agent (v2) reports
  its own host.
- runc is looked up in `PATH`; a containerd configured with another runtime binary shows that
  runc's version or none.
- `nerdctl info` warnings are shown as they are; their wording changes with nerdctl.
