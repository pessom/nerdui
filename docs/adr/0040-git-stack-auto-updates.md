# 0040. Automatic updates of Git stacks: polling and webhooks

- **Status:** Accepted
- **Date:** 2026-11-20

## Context

Git stacks (ADR-0039) are updated by a manual pull. Users want them to follow their repository:
a push should redeploy the stack (#38), or nerdui should look for new commits on a schedule and
redeploy or tell them (#39). Polling also works behind a firewall, where a Git host cannot call
nerdui.

## Options considered

- **Webhook authentication:** a secret token in the URL (Portainer's model; works with every
  Git host and with plain `curl` from CI), or the hosts' signatures (`X-Hub-Signature-256` of
  GitHub and Gitea, `X-Gitlab-Token`), which need code per host and an HMAC for generic callers.
  A 256-bit token in the path is as strong as the signature secret; its weakness is that the URL
  can end up in proxy logs.
- **Finding new commits:** fetch the repository at every check, or ask for the ref only
  (`git ls-remote`), which transfers no objects.
- **Where to redeploy:** a Git stack is not bound to a namespace. Redeploy in a namespace stored
  with the stack, or wherever the compose project has containers. The latter follows what the
  user actually deployed and leaves a stack that is down alone.
- **Webhook work:** pull and deploy inside the request (Git hosts time out after about 10
  seconds) or in the background.

## Decision

- Each Git stack has `autoUpdate` (`off`, `notify`, `deploy`) and `pollInterval` (0, 1 minute,
  5 minutes, 15 minutes, 1 hour, 6 hours, 1 day). They apply to polling and the webhook alike.
- Polling looks every 30 seconds for stacks whose interval elapsed and runs `ls-remote` for
  their ref. The result is stored: `checkedAt`, and `remoteCommit` while the newest remote
  commit is not checked out. A failed check is stored in `lastError`; the next good check
  clears it.
- `notify` sends a `git.update` notification once per new remote commit. `deploy` pulls as a
  job of the user `nerdui (Git)`. Then `compose up --remove-orphans` runs in every namespace of
  every writable environment where the project has containers. A stack without containers is
  only pulled. A successful update sends `git.update`; a failed job sends `job.failed`.
- Webhook: `POST /api/v1/hooks/git/{token}` is public. The token has 32 random bytes; only its
  SHA-256 is stored, so the URL is shown once, and creating a new one invalidates the old one.
  The handler:
  - answers GitHub's `ping` with `pong`;
  - ignores other events and pushes to other refs (a JSON `ref` in the body);
  - refuses calls with 409 while `autoUpdate` is `off`;
  - otherwise starts the check in the background and answers 202.
- The request log replaces the token with `<token>`.
- Pulls of one clone are serialized, so a webhook, polling and a manual pull cannot run
  `git` on the same work tree at once.
- Changing the settings and the webhook needs `stacks:deploy`. These calls work in read-only
  mode; deploys are skipped while an environment is read-only.

## Consequences

- Database migration 13 adds `auto_update`, `poll_interval`, `webhook_hash`, `checked_at` and
  `remote_hash` to `git_stacks`.
- Pushes deploy within seconds of the webhook; polling costs one small request per stack and
  interval.
- Whoever knows the webhook URL can make nerdui check the repository, but can only deploy what
  is committed to the stack's ref.
- Proxies in front of nerdui may log the token; regenerate the URL if it leaks.
- Only pushes to the stack's own ref act; other branches and tags are ignored.
