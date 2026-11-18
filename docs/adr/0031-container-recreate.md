# 0031. Editing and recreating containers

- **Status:** Accepted
- **Date:** 2026-11-18

## Context

A container's settings cannot be changed in place: `nerdctl` users stop, remove and run it
again with new flags (#18). Updating a container to a newer image (#53, #54) needs the same
step. nerdctl keeps no record of the original `nerdctl run` command line.

## Options considered

- **Reconstruct the settings into the create form's spec** from `nerdctl inspect` (labels
  nerdctl records and the OCI spec) and create a new container with nerdctl, like a user would.
- **Clone the containerd container** (spec, labels) with a new image — keeps every setting, but
  bypasses nerdctl (networking hooks, name store, state directories) and would carry over the
  old image's defaults.
- **Settings the form cannot express:** warn and lose them, or block recreating. The user chose
  to extend the form with the common ones (more networks, static IP, privileged, capabilities,
  devices, DNS, extra hosts) and to block the rest with the list of reasons.
- **Replacing:** remove then create (nothing to roll back to), or rename the old container
  first. The user chose renaming, with an automatic rollback.

## Decision

- `GET /containers/{id}/spec` reconstructs a `CreateSpec`: command and entrypoint compared with
  the image's, environment variables and labels that equal the image's defaults left out,
  ports, volumes, bind and tmpfs mounts, networks and static IP, limits, restart policy,
  privileges, devices, DNS and extra hosts. Settings outside the spec (e.g. `--rm`, sysctls,
  read-only root filesystem, PID/IPC namespaces, log driver and options, a MAC address, CPU sets,
  OOM settings, PID limits) are listed as `unsupported`, and recreating is refused while any is
  present. Containers of compose stacks are recreated through their stack.
- `POST /containers/{id}/recreate` runs a job: optionally pull the image (an update), stop the
  container, rename it to `<name>-nerdui-old-<time>`, create the new container under the name
  with the host nerdctl (started when the old one ran), then remove the old one. If creating or
  starting fails, the new container is removed and the old one gets its name back and is
  started again. Named and anonymous volumes are kept, since the new container mounts them.
- The UI edits a container with the create form, pre-filled; the name cannot change.

## Consequences

- A recreated container is a normal nerdctl container, as if created with the CLI.
- If the image tag already points to a newer image, variables equal to the old image's
  defaults cannot be told apart and are kept explicitly; the UI says so.
- Automatic updates (#54) recreate containers the same way and skip blocked ones.
