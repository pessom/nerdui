# 0025. Stacks: loading projects with compose-go, adoption in place or by copy

- **Status:** Accepted
- **Date:** 2026-11-16

## Context

ADR-0008 planned to use nerdctl's `pkg/composer` for stacks. Importing it breaks
`go mod tidy`: the test dependencies of `pkg/composer/serviceparser` reference
`github.com/containerd/nerdctl/mod/tigron v0.0.0`, which only exists as a `replace` inside
nerdctl's repository. Listing (#24) and adopting (#25) stacks need to load compose projects,
and adoption needs rules for directories outside the stacks directory.

## Decision

- **Loading.** Projects are loaded with compose-go and the project options of nerdctl's
  `composer.New`, in the project directory without `-f`, as `nerdctl compose` run there: the
  default compose file and `compose.override.yaml`, includes, extends, profiles and `.env`.
  `make modfiles` pins compose-go to the version each nerdctl release requires (ADR-0017), so a
  build reads compose files like the nerdctl it is built for.
- **Discovery.** `--stacks-scan-dirs` are scanned two levels deep for directories with a compose
  file that are not stacks yet; hidden directories and the stacks directory are skipped.
  `GET /environments/{env}/stack-candidates` (`stacks:read`).
- **Adoption** (`POST /environments/{env}/stacks/adopt`, `stacks:write`):
  - `in-place` registers the directory as an external stack (table `stacks`); `DELETE
    …/stacks/{name}/registration` forgets it and keeps files and containers.
  - `copy` copies the compose files and `.env` into `<stacks-dir>/<name>` through a temporary
    directory renamed at the end. It is refused (409) when the project uses other files of its
    directory (bind mounts, build contexts, env files, configs, secrets) or compose files outside
    it, because those relative paths would no longer resolve.
  - The stack name must be a valid compose project name and unused; a directory can be one stack
    only; directories inside the stacks directory are stacks already.
- Adoption and forgetting touch files and nerdui's database only, so they do not require a
  writable nerdctl (ADR-0011).

## Consequences

- Administrators can register any directory of the host as a stack; this is the trust of the
  administrator role. CodeQL's path-injection findings for these paths were dismissed with this
  justification.
- A stack whose compose files change on disk is reloaded on each request; there is no cache.
- Deploying stacks (#28) cannot use `pkg/composer` either; it is decided separately.
