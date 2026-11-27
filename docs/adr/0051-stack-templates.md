# 0051. Stack templates

- **Status:** Accepted
- **Date:** 2026-11-24

## Context

Users want to start common applications without writing compose files (#33), and to reuse
their own stacks (#34). Portainer has app templates (its own templates.json) and custom
templates; Arcane has a template registry: a registry.json listing templates, each a
compose.yaml, an .env.example and metadata.

## Options considered

- **Catalog**: only external registries, a built-in set, or Portainer's templates.json (many
  public lists, but in Portainer's container/stack model with its own fields).
- **Variables**: a form built from the compose file's `${VAR}` references, or from an
  `.env.example` whose comments describe the variables.
- **Secrets entered on deploy**: written into the stack's `.env`, or stored as variables of
  the stack (ADR-0049).

## Decision

- A **template** is a compose file, an `.env.example` (`KEY=default` lines; the comment lines
  right above a line describe it) and a name, description, categories, logo and documentation
  link. A variable whose name looks like a password, secret, token or key is proposed as a
  secret.
- Sources: a **built-in catalog** embedded in the binary (PostgreSQL, Percona Server for MySQL,
  Redis, nginx, Adminer, Uptime Kuma, Gitea, Vaultwarden, WordPress, Grafana, whoami; MariaDB
  until 0.2), **registries** by URL
  in Arcane's `registry.json` format (`compose_url`, `env_url`, `icon_url`,
  `documentation_url`, `tags`, relative to the registry) or with the files inline, and
  **templates users save** in the database, also from an existing stack (its compose file, and
  its `.env` with values of secret-looking names emptied).
- Registries are a setting (`settings:write`), at most 20 http(s) URLs. They are fetched by
  the server with a 15 s timeout and size limits (2 MiB for registry.json, 256 KiB per file)
  and cached for 10 minutes; one that cannot be read is reported, the others are listed.
- **Deploying** a template creates a managed stack in one request: the compose file, an
  `.env` from the `.env.example` with the values entered, and the secrets as encrypted
  variables of the stack, which are left out of `.env`. The project is validated with them;
  on failure nothing is left behind. It is then started (`compose up`) unless the user only
  creates it.
- Reading templates needs `stacks:read`, saving them and deploying `stacks:write`.

## Consequences

- Templates are plain compose files: a deployed stack is an ordinary stack, edited and
  updated like any other; it keeps no link to its template.
- Built-in templates change only with nerdui releases; image tags are majors or `latest`
  where the image has no major tags. A test loads each one like `nerdctl compose`.
- A registry decides where its files are fetched from; registries are trusted like the
  images they reference.
