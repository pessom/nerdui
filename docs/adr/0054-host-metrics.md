# 0054. Host metrics and alert thresholds

- **Status:** Accepted
- **Date:** 2026-11-24

## Context

Users want to see how loaded the host is and to be told when CPU, memory or a disk runs high
(#64). Container metrics history comes from an optional Prometheus (ADR-0045); the host's
usage should work without one.

## Options considered

- **Prometheus / node_exporter only**: rich history, but another component to install.
- **Read /proc and statfs in nerdui**: no dependency; history limited to what nerdui keeps.
- **History storage**: SQLite (survives restarts, writes every few seconds) or memory.

## Decision

- nerdui samples the host it runs on every 15 s from `/proc/stat`, `/proc/meminfo`,
  `/proc/loadavg`, `/proc/net/dev` and `statfs`:
  - CPU is the busy share of all CPUs between samples;
  - memory used is `MemTotal − MemAvailable`;
  - network is the bytes per second of physical interfaces (loopback, veth, CNI and bridge
    interfaces are skipped);
  - disks are the filesystems of `/`, the data directory, containerd's root and nerdctl's
    data root, once per filesystem.
- One day is kept in memory (about 5,760 samples). `GET …/host/metrics?range=1h|6h|24h`
  returns the current sample and the history averaged to at most 360 points.
- Thresholds are a setting (`host.alerts`): CPU and memory with a percent and minutes the
  usage must stay above, disks with a percent applied to each filesystem. Off by default.
- Crossing a threshold sends a `host.alert` notification (warning); falling 5 points below
  it sends a recovery (info). Notifications keep their 15-minute dedupe.

## Consequences

- A restart of nerdui starts the history anew; longer history belongs in Prometheus.
- When nerdui runs in a container, it reads the container's view of `/proc` unless the host's
  is mounted; the supported deployment runs nerdui on the host as a systemd service.
- Only the local environment has host metrics until agents (#70) report their own.
