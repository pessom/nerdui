# 0041. Incoming webhooks

- **Status:** Accepted
- **Date:** 2026-11-21

## Context

CI pipelines publish images and then need the running stack or container updated (#91). More
generally, external systems want to trigger an action in nerdui with a plain HTTP call (#85).
Git stacks have their own webhook (ADR-0040), but stacks from the stacks directory and single
containers have none.

## Options considered

- **Scope:** a webhook per stack or container, managed on its page; or one list of webhooks,
  each naming its target and action, managed in Settings. The list shows every URL that can
  change something in one place, and it covers containers and stacks alike.
- **Authentication:** a secret token in the URL, as for Git stacks (ADR-0040), or an API key
  (ADR-0028) with a narrow role. API keys need a header and a role per caller; CI tools and
  registries (Docker Hub, GHCR, Harbor) can only call a URL.
- **Actions:**
  - *redeploy*: pull images and recreate;
  - *restart*.
  Running arbitrary commands is out of scope.
- **Response:** wait for the job (CI would see the result, but deploys outlast HTTP timeouts) or
  answer when it has started.

## Decision

- A table of webhooks. Each has a name, environment, namespace, target kind (`stack` or
  `container`), target name, action (`redeploy` or `restart`) and an enabled flag.
- Managing them needs `settings:write`, reading them `settings:read`: a webhook lets anyone with
  its URL act on the target.
- The token has 32 random bytes; only its SHA-256 is stored. The URL `/api/v1/hooks/{token}` is
  shown when the webhook is created or its token is replaced, and never again. The request log
  replaces the token with `<token>`.
- `POST /api/v1/hooks/{token}` is public and ignores the body. It starts a job of the user
  `webhook <name>` and answers 202 with the job ID:
  - *redeploy* of a stack: `compose pull`, then `compose up`;
  - *redeploy* of a container: pull its image and recreate it with the same settings. Like
    automatic updates, it refuses containers whose settings recreating would lose;
  - *restart*: `compose restart` or a container restart.
- Errors:
  - 404 for unknown or disabled webhooks;
  - 409 for a missing target or a job already running on it;
  - 503 while the environment is read-only.
  The newest call and its job or error are shown in the list.
- Containers are found by name, which survives recreation; IDs do not.

## Consequences

- Database migration 14 adds `webhooks`.
- CI can redeploy with `curl -fsS -X POST <url>`; the job's output is in the Jobs panel, and
  failures are sent as `job.failed` notifications.
- A leaked URL lets anyone redeploy or restart its target, but not change what runs. Replacing
  the token or disabling the webhook stops it.
- Containers of compose stacks should be redeployed through their stack, so that compose stays
  in charge of them.
