# 0023. Metrics history from Prometheus

- **Status:** Accepted
- **Date:** 2026-11-15

## Context

The Stats tab (#16) shows live samples while it is open and keeps five minutes in the browser.
Users who already run Prometheus want longer history (#176) without nerdui collecting and
storing metrics itself. containerd has built-in Prometheus metrics (`[metrics] address` in
`config.toml`, endpoint `/v1/metrics`): cgroup task metrics labelled with `container_id` and
`namespace`. They cover CPU, memory, block I/O and PIDs, but not network traffic (it lives in the
network namespace, not in the cgroup).

## Options considered

- **Prometheus as an optional, read-only source** — nerdui queries the environment's Prometheus
  server; nothing is collected or stored by nerdui.
- **Own sampling into SQLite** — no dependency, but a second metrics store: constant sampling of
  every running container, database growth and retention to manage.
- **Link to Grafana** — trivial, but the history stays outside nerdui.

## Decision

Prometheus as an optional, read-only source, with containerd's built-in metrics only.

- **Settings.** Per environment, in the existing `settings` table (key
  `environments/<env>/prometheus`, JSON): base URL, authentication (`none`, `basic`, `bearer`),
  username and secret. Managed on the environment's Settings page; `settings:read` /
  `settings:write` (admin). The secret is write-only in the API (`hasSecret`); an empty secret
  keeps the stored one when the mode is unchanged. Settings routes do not require a writable
  nerdctl (ADR-0011). `POST .../settings/prometheus/test` checks submitted settings without
  saving them and reports how many containers have containerd metrics.
- **Queries.** The server builds PromQL from fixed templates (`internal/prometheus`); the
  container ID and namespace are validated containerd identifiers and quoted. The browser never
  sends PromQL and never talks to Prometheus. `max()` collapses duplicate scrapes; block I/O is
  summed over devices; memory excludes inactive file cache like `nerdctl stats`; an unlimited
  memory limit is dropped.
- **Periods.** `1h`, `6h`, `24h`, `7d` with 15 s, 1 m, 5 m and 30 m steps (240–340 points) and
  `rate()` windows of at least two scrapes. The axis is aligned to the step; gaps are `null`.
- **API.** `GET .../containers/{id}/stats/history?range=` (`containers:read`): 409 without a
  connection, 503 when Prometheus is unreachable or rejects the query. Works for stopped and
  removed containers. `features.metricsHistory` in the environment status tells the UI to offer
  the periods.
- **Client limits.** 20 s timeout, 8 MiB response cap; errors name the problem without repeating
  the request URL or credentials.
- **Verification.** The integration job enables containerd metrics and checks that every metric
  the queries use is exported with the expected labels (`TestIntegrationContainerdMetrics`).

## Consequences

- No network history; a cAdvisor-style exporter would add it later as another metrics preset.
- History follows the container ID: a recreated container starts a new history.
- Resolution is bounded by the scrape interval; live stats stay on the WebSocket stream.
- The secret is stored in plain text in nerdui's database, like other settings; the data
  directory is root-only (0700).
- An administrator can make the nerdui host send GET requests to any URL through the test
  endpoint; this is the same trust as the rest of the admin role.
