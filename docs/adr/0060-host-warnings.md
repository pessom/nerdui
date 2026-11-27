# 0060. Warnings about the host

- **Status:** Accepted
- **Date:** 2026-11-27

## Context

On Linux 7 with AppArmor and `kernel.apparmor_restrict_unprivileged_unconfined=1` (Ubuntu
26.04), the profile of a container is stacked with `unconfined`, and AppArmor denies signals
between the container's own processes. Go programs that change user (`gosu`, in the entrypoint
of MySQL, MariaDB, Percona, PostgreSQL and many other images) signal every thread and wait for
them: they spin at 100% CPU forever. The container is *running*, its log ends early, and neither
containerd nor nerdctl reports anything; only the kernel log shows the denial. Users take it
for a broken image or a broken nerdui.

## Options considered

- **Documentation only**: nobody reads it before the problem, and the symptom does not point to
  AppArmor.
- **A probe**: start a container that changes user and see whether it hangs. Exact, but needs
  an image on every host, takes seconds, and a hung probe can only be killed from outside.
- **Reading the host settings**: AppArmor enabled, the sysctl set, kernel 7 or later. Instant and
  without side effects; it may warn on a kernel that has since been fixed.
- **Changing the settings, or running containers unconfined, automatically**: weakens the host
  or the containers without the administrator deciding it.

## Decision

- `internal/hostcheck` reads `/sys/module/apparmor/parameters/enabled`,
  `/proc/sys/kernel/apparmor_restrict_unprivileged_unconfined` and
  `/proc/sys/kernel/osrelease`, and returns warning codes (`apparmor_signals`).
- `GET /environments/{env}` returns them as `warnings` for the local host; the UI shows a banner
  on the host's pages with the symptom, the check and the ways out, described in
  docs/deployment.md.
- nerdui changes nothing on the host and in containers by itself.

## Consequences

- The warning may outlive the kernel bug; the text says *may* and tells how to check.
- Other host problems can be added as new codes without changing the API shape.
- Hosts behind agents (#70) will report their own warnings.
