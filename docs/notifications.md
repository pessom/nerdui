# Notifications

Administrators add channels under **Settings → Notifications**. Each channel receives the event
kinds it subscribes to, from every environment. The same event about the same object is sent at
most once in 15 minutes. **Send test** delivers a test message with the settings in the form,
before they are saved. See [ADR-0034](adr/0034-notifications.md).

## Event kinds

| Kind | When |
| --- | --- |
| `image.update` | A scheduled check finds a new image for a tag used by containers |
| `autoupdate.updated` | An automatic update succeeded |
| `autoupdate.failed` | An automatic update failed (the previous container was restored) |
| `autoupdate.pending` | More automatic updates are due than the approval threshold |
| `container.died` | A container exited with a non-zero code that nobody asked for |
| `container.oom` | A container ran out of memory |
| `container.unhealthy` | A running container's healthcheck turned it `unhealthy` |
| `container.autoheal` | nerdui restarted an unhealthy container labeled `nerdui.auto-heal=true`, the restart failed, or it stopped after 3 restarts within an hour ([ADR-0043](adr/0043-health-and-auto-heal.md)) |
| `job.failed` | A background operation (pull, deploy, recreate, …) failed |
| `git.update` | A Git stack's repository has a new commit (notify mode), or the stack was updated from it |

## Channels

| Type | Settings |
| --- | --- |
| Webhook | URL; optional signing secret |
| Slack | Incoming webhook URL |
| Discord | Channel webhook URL |
| Telegram | Bot token from @BotFather, chat ID (user, `-100…` group, or `@channel`) |
| ntfy | Topic URL (`https://ntfy.sh/<topic>` or your server); optional access token |
| Gotify | Server URL, application token |
| Email | SMTP host, port, STARTTLS (default) / TLS / none, optional login, From, To (comma-separated) |

Tokens, passwords, signing secrets and the Slack and Discord webhook URLs are never shown again
after saving; leave them empty when editing to keep them.

## Webhook payload

A `POST` with `Content-Type: application/json` and the header `X-Nerdui-Event: <kind>`:

```json
{
  "kind": "container.died",
  "severity": "error",
  "title": "Container web exited with code 1",
  "message": "nginx: [emerg] invalid parameter",
  "host": "vps",
  "environment": "local",
  "namespace": "default",
  "object": "web",
  "time": "2026-11-19T10:00:00Z"
}
```

`severity` is `info`, `warning` or `error`; empty fields are omitted. With a signing secret, the
header `X-Nerdui-Signature: sha256=<hex>` holds the HMAC-SHA256 of the raw body, keyed with the
secret. Any 2xx response counts as delivered.
