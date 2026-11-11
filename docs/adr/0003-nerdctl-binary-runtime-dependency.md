# 0003. nerdctl binary is a required runtime dependency

- **Status:** Accepted (amends [0001](0001-go-containerd-socket.md))
- **Date:** 2026-11-11

## Context

ADR-0001 chose the containerd socket plus nerdctl Go packages, assuming no process spawning.
Reviewing nerdctl v2.4 sources shows that nerdctl-managed containers depend on the `nerdctl`
binary itself, not only on its packages:

- **Networking.** Every container created by nerdctl gets an OCI hook
  `nerdctl internal oci-hook` (createRuntime / postStop) that sets up CNI networking and port
  forwarding. The hook path comes from the `NerdctlCmd` option
  (`pkg/cmd/container/create.go`).
- **Logging.** Container stdout/stderr is shipped by containerd to a `binary://` log URI that
  points to nerdctl's internal log driver, which writes the files read by `nerdctl logs`.
  `container.Create` builds this URI from `os.Executable()` — **the calling process** — with no
  option to override it. Called from nerdui, it would record the nerdui binary as the log driver.
- **Compose.** `pkg/composer` creates containers, volumes and networks by executing
  `nerdctl create`, `nerdctl volume create`, `nerdctl network rm`, etc.
- **Lifecycle.** `containerutil.Start` / `Unpause` also take `NerdctlCmd`.

## Options considered

1. **Require nerdctl on the host**; operations that record binary paths into containers are
   delegated to the `nerdctl` binary.
2. **nerdui as its own hook and log driver** (`pkg/ocihook`, `logging.Main`): containers would
   depend on the nerdui binary — removing or moving nerdui breaks their networking and logs;
   compose still needs nerdctl.
3. **Library create + rewrite `nerdctl/log-uri` / `containerd.io/restart.loguri` labels** before
   start: no process spawning, but a hack on top of nerdctl internals that may break on upgrades.
4. **All mutations through the nerdctl CLI**, reads via the socket: output parsing, slower,
   partially reverts ADR-0001.

## Decision

Option 1. nerdui requires the `nerdctl` binary on the host (rootful mode).

`--nerdctl-path` (default: `nerdctl` resolved via `PATH`, typically `/usr/local/bin/nerdctl`)
is used as follows:

| Through containerd socket + nerdctl packages | Through the `nerdctl` binary |
| --- | --- |
| list, inspect, events, stats, exec, reading logs | container create / recreate (`nerdctl create …`, #17, #18) |
| start, stop, restart, kill, pause, unpause, remove (`NerdctlCmd` passed where required) | compose operations (`pkg/composer` executes `nerdctl` internally, #28) |
| images: list, pull, tag, push, remove, prune | |
| volumes, networks, system info, df | |

- Container create arguments are built from a typed request (never from raw user strings), passed
  as an argument vector (no shell), and the new container ID is read from stdout.
- At startup nerdui checks that the binary exists and reports its version in `/api/v1/version`
  and on the host info page. Without nerdctl, nerdui runs read-only with a clear banner.
- The container image mounts the host binary at the **same path**: hooks and the log driver run
  in the host mount namespace, so the path recorded in the container must be valid on the host.

## Consequences

- Containers created via nerdui are indistinguishable from CLI-created ones and keep working if
  nerdui is removed.
- nerdctl and nerdui versions should be compatible; we test against the nerdctl version pinned in
  `go.mod` and document the supported range.
- Rootless containerd/nerdctl is out of scope for MVP (different hook wrapping via
  `rootlessutil`).
