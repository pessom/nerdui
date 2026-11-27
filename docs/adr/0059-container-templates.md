# 0059. Templates for single containers

- **Status:** Accepted
- **Date:** 2026-11-27

## Context

Templates (ADR-0051) deploy compose stacks. Many applications are one container (Uptime
Kuma, nginx, Redis, whoami), and many users run plain containers, not stacks. They want to
start such an application as a container from a catalog, and to keep a container they
configured by hand as a template to start again, on this or another host. Turning a
container into a compose file is also asked for on its own (#19).

## Options considered

- **A second template format** (the container create request as JSON, like Portainer's
  container templates): direct, but two formats in every catalog, registry and editor, and
  container templates could not be deployed as stacks.
- **One format, compose, converted both ways**: a template with one service can be deployed
  as a stack or as a container; a container is saved as a one-service compose file. Fields
  that do not convert are listed, not dropped silently.
- **Where to convert**: in the browser, or in Go with compose-go, the loader `nerdctl compose`
  itself uses, so variables, defaults and short syntaxes mean the same as in a stack.

## Decision

- Templates stay compose files. `GET /templates/detail` lists the template's services; with
  exactly one, the deploy dialog offers *Stack* or *Container*.
- `POST /templates/container` loads the compose file with the variables given (no
  environment of the nerdui process) and returns the create request of the container form
  (ADR-0003) and the settings it cannot express. Named volumes become `<container>-<volume>`,
  so two containers from one template do not share data; relative bind mounts, builds,
  extra networks, secrets, configs and similar settings are reported as unsupported.
- The form opens filled in: the user reviews everything, including the unsupported list, and
  creates the container as usual (`containers:write`). The values never go into the URL.
- `GET /environments/{env}/containers/{id}/compose` turns a container's settings (the same
  reconstruction as recreating, ADR-0031) into a one-service compose file and an
  `.env.example`. Environment variables whose names look like secrets become `${NAME}`
  variables without a value, so a saved template holds no passwords. The container page
  offers *Save as template*, which opens the template editor with both files.

## Consequences

- Every one-service template, built-in, from a registry or saved, can start a container;
  saved containers can also be deployed as stacks.
- The conversion covers what the container form covers; anything else is listed, so the user
  decides whether a container without it is good enough.
- Containers created from a template carry no link to it; updating them is recreating them
  (ADR-0031).
