# 0034. Notifications

- **Status:** Accepted
- **Date:** 2026-11-19

## Context

nerdui now acts on its own (automatic updates, #54) and can wait for a person (#56), but people
only learn about it by opening the UI. Users ask for notifications about new images (#96),
crashed containers and failed operations, sent where they already look (#86). Competitors
support many services: Arcane (email, Discord, Gotify, Matrix, ntfy, Pushover, Slack, Telegram,
webhooks), Dockhand (SMTP, webhooks, MQTT).

## Options considered

- **Library or own senders:** shoutrrr-style libraries cover dozens of services, but add a large
  dependency tree for a few HTTP calls each. Each service nerdui supports is a short request.
- **Which services first:** a generic webhook (anything else can be built on it), Slack, Discord,
  Telegram, ntfy, Gotify and SMTP email cover most self-hosters. MQTT needs a client library and
  is left for later.
- **Where events are routed:** per-environment rules, or channels that subscribe to event kinds
  of all environments. With one environment today, kinds per channel are enough; the
  environment is part of every message.
- **Secrets:** encrypted at rest (#35, not built yet) or stored like the Prometheus secret:
  plaintext in nerdui's database (mode 0600), never returned by the API.

## Decision

- Channels (migration 9, `notification_channels`) have a name, a type, settings, the event kinds
  they receive and an enabled flag; administrators manage them under Settings → Notifications
  (`/notifications/...`, `settings:read` / `settings:write`). Secrets — webhook signing keys,
  tokens, the SMTP password, and Slack and Discord webhook URLs, which contain credentials — are
  write-only; saving without them keeps the stored values while the type is unchanged.
- Event kinds: a new image for a tag in use, automatic update done, failed or waiting for
  approval, a container exiting with an error or running out of memory, a failed operation.
- Events are queued without blocking the code that raises them and sent by one background
  worker, 20 s per delivery. The same kind about the same object is sent at most once in 15
  minutes, so a crash loop does not flood a chat. Each channel shows its newest delivery result.
- "Send test" sends through unsaved settings and reports the service's answer.
- Webhooks receive the event as JSON; with a secret, `X-Nerdui-Signature: sha256=<hex HMAC>` of
  the body. SMTP uses STARTTLS by default, implicit TLS or none, and never sends a password
  without TLS. Errors never repeat URLs or tokens that carry credentials.

## Consequences

- Administrators can make nerdui send requests to any URL they enter; this is the same trust as
  the Prometheus URL.
- More services (Matrix, Pushover, MQTT) are new senders without changes elsewhere.
- Events that happen while nerdui is down, or beyond a full queue (256), are not sent.
