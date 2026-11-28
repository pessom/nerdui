# 0061. Building images

- **Status:** Accepted
- **Date:** 2026-11-28

## Context

Users want to build an image where it runs: from a Dockerfile they write, from a Git
repository, or from a context they upload, with build arguments and tags, and to push it to a
registry afterwards (#45). nerdctl builds with BuildKit: `nerdctl build` runs `buildctl`
against a `buildkitd` and loads the result into containerd.

## Options considered

- **nerdctl's builder package in-process** (`pkg/cmd/builder.Build`): the same as the CLI, but
  it starts `buildctl` without a context, so a cancelled build keeps running.
- **The BuildKit client library**: full control, but a second copy of what nerdctl does
  (finding the daemon, exporting, loading into the namespace), per nerdctl version.
- **The `nerdctl build` binary** (as compose, ADR-0003, ADR-0026): the exact behaviour of the
  host's nerdctl; the process can be stopped.
- **Context sources**: a Dockerfile only; a Git URL given to BuildKit; a clone made by nerdui
  with the Git credentials of Git stacks (ADR-0039); an uploaded archive.

## Decision

- `POST /environments/{env}/images/build` (`images:write`) starts a job (ADR-0018) that runs
  `nerdctl build --progress=plain` in a temporary context directory under
  `<data-dir>/builds`, removed when the job ends. Tags, build arguments, target, platform, no
  cache and pull are validated and passed as single arguments after which the context follows
  `--`; the log shows build arguments without their values. With `push`, every tag is pushed
  afterwards like `nerdctl push`.
- The context comes from:
  - a **Dockerfile** given as text, built with an empty context;
  - a **Git repository**, cloned by nerdui with the same URL checks, credentials and known
    hosts as Git stacks, optionally from a subdirectory;
  - an uploaded **tar or tar.gz archive** (multipart: the request, then the archive),
    extracted while it arrives through an `os.Root`, so no entry, link or `..` can write
    outside the context; devices are skipped, the size and the number of entries are bounded.
- The context and the Dockerfile must stay inside the source after symbolic links, so a build
  cannot read a file of the host as its Dockerfile.
- `nerdctl build` runs in its own process group; cancelling the job kills the group, including
  `buildctl`, and BuildKit stops the build.
- `GET /environments/{env}/images/builder` (`images:read`) tells whether a BuildKit daemon
  answers at the addresses nerdctl tries, so the UI can explain a missing `buildkitd` before a
  build fails.

## Consequences

- Building needs `buildctl` and a running `buildkitd` on the host (nerdctl-full ships both;
  `systemctl enable --now buildkit`). nerdui does not start it.
- Builds run with BuildKit's defaults: no extra entitlements (`--allow`), no secrets or SSH
  forwarding yet.
- Build arguments end up in the image history; secrets belong in BuildKit secrets, a later
  step.
