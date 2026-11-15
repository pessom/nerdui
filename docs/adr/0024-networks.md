# 0024. Networks: host nerdctl for creation, nerdctl's rules for removal

- **Status:** Accepted
- **Date:** 2026-11-15

## Context

The Networks page (#60) lists CNI networks with their containers and addresses, creates bridge,
macvlan and ipvlan networks with IPAM (subnet, gateway, IP range) and removes networks. nerdui
links nerdctl's packages and ships one build per nerdctl minor (ADR-0017). Listing and removal
(`netutil.NewCNIEnv`, `NetworkList`, `RemoveNetwork`, `UsedNetworks`) have the same API in
nerdctl 2.3 and 2.4, but `types.NetworkCreateOptions` does not: `IPRange` is a string in 2.3 and
a list in 2.4, which also adds `AuxAddresses` and `IPv4`.

## Options considered

- **Host `nerdctl network create`** — like creating containers (ADR-0003): typed, validated
  arguments, no shell; the network is exactly what the host CLI would create.
- **Library with build tags** — one file per nerdctl minor; the first build-tag split, requiring
  changes to the Makefile, CI and GoReleaser.
- **Library with the common fields only** — no IP range.

## Decision

- **Creation** runs the host nerdctl (`nerdctl network create`) with the global flags of the
  environment (address, namespace, data root, CNI paths). `NetworkCreateArgs` validates the spec
  (Docker-compatible name, not `bridge`/`host`/`none`; driver bridge, macvlan or ipvlan; parent
  interface required for macvlan/ipvlan; subnet, gateway and IP range inside the subnet; labels
  outside the `nerdctl/` prefix), uses `--flag=value` and puts the name after `--`. The created
  network is read back with netutil.
- **Listing** uses netutil. Containers are matched by the `nerdctl/networks` label; addresses of
  running containers come from their network namespace (as `nerdctl inspect`), interface
  `eth<i>` being the i-th network of the label — the same in both nerdctl releases.
- **Removal** follows `nerdctl network rm`: refused (409) while a running container in any
  namespace uses the network, and for the default network, pre-defined networks (no file) and
  networks without a nerdctl ID. Stopped containers do not block removal; the UI warns that
  they will not start until reconnected.
- API: `GET/POST /environments/{env}/networks`, `DELETE /environments/{env}/networks/{name}`
  (`networks:read` / `networks:write`); mutations need a writable nerdctl (ADR-0011).

## Consequences

- Creation needs the host nerdctl binary, like container creation; errors are nerdctl's
  messages.
- Connecting a running container to a network is not offered: nerdctl has no `network connect`.
- A build-tag mechanism is still unused; it remains the option for library-only differences.
