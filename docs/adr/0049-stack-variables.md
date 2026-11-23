# 0049. Encrypted variables of stacks

- **Status:** Accepted
- **Date:** 2026-11-23

## Context

Stacks need settings and secrets (#35): passwords, tokens, image tags. Until now they lived in
each stack's `.env` file in plain text, edited like the compose file and shown in full, and
values shared by several stacks had to be copied into each. Other tools keep variables in
their database, encrypt secrets (Dockhand: AES-256-GCM) and substitute them at deploy time
(Komodo: `[[NAME]]`; Arcane: global variables).

## Options considered

- **How variables reach compose**: write them into `.env` before each deploy (secrets on disk,
  the user's `.env` overwritten or merged), pass `--env-file` (replaces `.env` in nerdctl), or
  set them in the environment of `nerdctl compose`, which compose prefers over `.env`.
- **Syntax**: compose's own `${NAME}` interpolation, or a nerdui syntax such as `[[NAME]]`
  rewritten in the compose file before deploying (files on disk would no longer work with
  plain `nerdctl compose`).
- **Key**: a key file generated next to the database (works out of the box, travels with
  backups) or a key that must be given by flag or environment (safer against a copied data
  directory, but nerdui does not start without it).

## Decision

- Variables are **global** (shared by all stacks) or **per stack** (by stack name). Each has a
  name (`[A-Za-z_][A-Za-z0-9_]*`), a value (UTF-8, up to 64 KiB) and a *secret* flag. The API
  never returns a secret value; saving without a value keeps the stored one, and a secret
  becomes visible only with a new value.
- They are passed to `nerdctl compose` (deploys, redeploys, restarts, Git deploys, webhooks,
  schedules) as **environment variables**, and used when nerdui loads compose files (service
  list, validation in the editor). Compose files use them as `${NAME}`, also in
  `environment:` entries without a value. Precedence: stack variable > global variable >
  `.env` > nerdui's own environment. Nothing is written to disk.
- Names that change how nerdctl, compose or the system work are refused: `PATH`, `HOME`,
  `USER`, `SHELL`, `TMPDIR`…, and names starting with `LD_`, `XDG_`, `CONTAINERD_`,
  `NERDCTL_`, `DOCKER_`, `COMPOSE_`, `BUILDKIT_`, `CNI_`, `NERDUI_`, `ROOTLESSKIT_`.
- All values are encrypted with **AES-256-GCM** (`internal/secretbox`), bound to their scope
  and name as associated data. The key is a random 32-byte hex file, `<data-dir>/secret.key`
  (mode 0600), created at the first start; `--secret-key-file` moves it. Backups of nerdui
  (ADR-0047) include the key, and `nerdui admin restore` puts it back.
- Forgetting a stack or removing a Git stack deletes its variables, so a later stack of the
  same name does not get them.
- Global variables are a setting (`settings:read`/`settings:write`); a stack's variables are
  edited like its files (`stacks:read`/`stacks:write`).

## Consequences

- Secrets no longer need to be in `.env`; the editor's validation sees variables, so a
  required `${NAME:?}` set in nerdui is not reported as missing.
- Variables apply on the next deploy; running containers keep the values they were created
  with. Values used in `environment:` are visible in the container's configuration (inspect),
  as with any compose deployment.
- Someone with the data directory has the key as well: encryption protects copies of the
  database alone (e.g. a stolen snapshot), not a copied host. Losing the key file makes the
  stored values unreadable; nerdui then refuses deploys of stacks with variables until they
  are deleted or the key is restored.
- Other secrets in the database (Git credentials, notification channels, backup passwords of
  schedules) are still stored as before; they can move to the same key later.
