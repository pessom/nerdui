# 0019. Registry credentials in the shared docker config

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

Pulls and pushes of private images (#43, #44) and compose deploys need registry credentials
(#51). nerdctl reads them from the docker config of the user (`$DOCKER_CONFIG/config.json`,
`~/.docker/config.json` of root for nerdui), written by `nerdctl login`; credential helpers
configured there are honored. nerdui calls nerdctl's pull, push and login packages, which only
know this file. The issue asked for credentials encrypted in nerdui's database.

## Options considered

- **A. Shared docker config, via nerdctl's login/logout** — the UI and `sudo nerdctl login` on
  the host see the same registries; pull, push and compose need nothing else. Storage is what
  Docker does: base64 in a 0600 file, or a credential helper if configured.
- **B. Encrypted in SQLite** — nerdctl cannot read them: every operation would need a temporary
  `DOCKER_CONFIG`, which is process-wide (concurrent jobs race, and docker/cli caches the
  location per process). The CLI on the host would not see the logins.
- **C. SQLite plus sync to the docker config** — the file still holds the credentials, and two
  sources have to be reconciled.

## Decision

Option A.

- Registries are per environment (`/environments/{env}/registries`), because each host has its
  own docker config; for the local environment that is the config of the user running nerdui.
- `GET` lists the stored servers with usernames (never passwords); `POST {server, username,
  password}` runs nerdctl's `login.Login`, which verifies the credentials with the registry
  before storing them (400 when rejected, 503 when unreachable) and honors nerdctl's settings
  (insecure registries, hosts.d); `DELETE ?server=` runs `logout.Logout` (404 when not logged in).
  Both username and password are required, so nerdctl never prompts on a terminal.
- The warning that the password is stored unencrypted is returned and shown in the UI.
- nerdui serializes its own changes of the file; a concurrent `nerdctl login` on the host can
  still race, as with the docker CLI.
- `registries:read` / `registries:write`; changes are blocked in read-only mode (ADR-0011) like
  other changes of the environment.

## Consequences

- Credentials are as protected as the host's docker config: root-only file (or a credential
  helper). Anyone with root on the host can read them — as with nerdctl itself.
- A "default registry" (from the issue) is not part of this: references without a host resolve
  to Docker Hub as in nerdctl.
- Stored credentials cannot be re-tested without the password; a login is the test.
