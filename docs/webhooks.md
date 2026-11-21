# Incoming webhooks

An incoming webhook is a secret URL that redeploys or restarts one stack or container. Call it
from CI after an image was published, or from any system that can send an HTTP request. See
[ADR-0041](adr/0041-incoming-webhooks.md). Git stacks also have their own push webhook; see
[Git stacks](git-stacks.md).

## Creating a webhook

Administrators add webhooks under **Settings → Incoming webhooks**. Each one has:

- a **name**;
- a **namespace** and **target**: a stack, or a container by name;
- an **action**:
  - **Pull images and redeploy**: `compose pull` then `compose up` for a stack. For a
    container, its image is pulled and it is recreated with the same settings.
  - **Restart**: restarts the stack or container as it is.
- **Enabled**: a disabled webhook answers 404.

The URL is shown once, right after saving. Store it as a secret in your CI. **New URL** (the key
icon) replaces it, and the old URL stops working.

## Calling it

```sh
curl -fsS -X POST https://nerdui.example.com/api/v1/hooks/<token>
```

The body is ignored, so registries and CI systems can post their own payloads. The answer is `202`
with the job ID as soon as the job has started; its output is in the **Jobs** panel. A failed
job sends a `job.failed` notification.

| Status | Meaning |
| --- | --- |
| 202 | The job started |
| 404 | Unknown or disabled webhook |
| 409 | The target does not exist, or a job runs for it |
| 503 | The environment is in read-only mode |

### GitHub Actions

```yaml
- name: Redeploy
  run: curl -fsS -X POST "${{ secrets.NERDUI_WEBHOOK }}"
```

### GitLab CI

```yaml
deploy:
  stage: deploy
  script:
    - curl -fsS -X POST "$NERDUI_WEBHOOK"
```

## Notes

- Recreating a container keeps the settings nerdui can express. A container with other settings
  is refused (the error is shown in the list); redeploy it through its stack, or edit it once in
  nerdui.
- Containers that belong to a compose stack are better redeployed through the stack.
- nerdui must be reachable from the caller. Proxies in front of nerdui may log the URL; replace
  it if it leaks.
