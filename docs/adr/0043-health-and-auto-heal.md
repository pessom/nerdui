# 0043. Container health reports and restarts of unhealthy containers

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

nerdctl runs healthchecks (`--health-cmd`, compose `healthcheck:`) from systemd timers and keeps
the state in the container's labels: `nerdctl/healthstate` holds the status and the failing
streak, a `health.json` file in the container's state directory the last results, which
`nerdctl inspect` shows under `State.Health`. Neither nerdctl nor containerd restarts an
unhealthy container, and no containerd event announces a change of health (only the label
update, after every check) (#22).

Unlike Docker, nerdctl 2.3 and 2.4 keep the failing streak and the `unhealthy` status across a
restart until a check passes: a restarted container stays `unhealthy` until its next successful
check.

nerdui reports a crash when a container exits with a non-zero code without containerd's
explicitly-stopped label. A restart sets the label for the stop and clears it on the start, so
restarts made by nerdui itself (the Restart action, webhooks) were reported as crashes with
code 137 or 143.

## Options considered

- **Detection:** subscribe to `/containers/update` events (one per check of every container,
  each requiring a container lookup), or read the container list on an interval.
- **Opt-in:** restart every unhealthy container, or only those labeled, as automatic updates
  are opted in with `nerdui.auto-update` (ADR-0032).
- **Restart loops:** restart on every reading of `unhealthy`; wait for new failed checks after
  each restart; limit restarts per time window.

## Decision

- A healer reads the containers of every environment and namespace every 30 seconds.
- A running container whose status turns `unhealthy` is reported once with the
  `container.unhealthy` notification, whether it opted in or not.
- Containers labeled `nerdui.auto-heal=true` are restarted (the same action as Restart). After
  a restart, the next one waits until the failing streak grew by the healthcheck's retries
  (3 by default) since the restart, as Docker would need after resetting the streak.
- At most 3 restarts per container in an hour. Beyond that the container is left unhealthy
  with one `container.autoheal` error notification; it is restarted again once it was healthy,
  or the hour passed. Each restart, and a failed one, sends `container.autoheal`.
- The healer keeps its state in memory: after nerdui restarts, a container that is still
  unhealthy is reported and restarted again.
- nerdui remembers the containers and compose projects it is stopping, restarting or removing
  (`runtime.ExpectedExit`), and the crash watcher skips their exits. Restarts with the nerdctl
  CLI are still reported as before.

## Consequences

- A container is restarted up to 30 seconds after it turns unhealthy.
- Reading the container list every 30 seconds is the same work as the containers page polling
  it, independent of how often checks run.
- The container page shows the healthcheck settings, status, failing streak and the last
  results from `nerdctl inspect`.
- The create form does not set healthchecks yet, and editing or recreating a container loses a
  healthcheck set with `--health-cmd`; both are follow-ups (#237).
