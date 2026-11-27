# Architecture Decision Records

We record significant architectural decisions as ADRs
([Michael Nygard format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions)).

- Copy [`0000-template.md`](0000-template.md) to `NNNN-short-title.md` with the next number.
- An ADR is immutable once accepted; to change a decision, write a new ADR that supersedes or
  amends it.

| #    | Title                                                                                   | Status                |
| ---- | --------------------------------------------------------------------------------------- | --------------------- |
| 0001 | [Go backend over the containerd socket](0001-go-containerd-socket.md)                   | Accepted, amended by 0003 |
| 0002 | [License: AGPL-3.0-only with CLA](0002-license-agpl-cla.md)                             | Accepted              |
| 0003 | [nerdctl binary is a required runtime dependency](0003-nerdctl-binary-runtime-dependency.md) | Accepted         |
| 0004 | [State storage: SQLite in /var/lib/nerdui](0004-state-storage-sqlite.md)                | Accepted              |
| 0005 | [Authentication, sessions and transport security](0005-authentication-sessions-tls.md)  | Accepted              |
| 0006 | [API model: environments, conventions and jobs](0006-api-model-environments-jobs.md)    | Accepted              |
| 0007 | [Streaming over WebSocket](0007-streaming-websocket.md)                                 | Accepted              |
| 0008 | [Stacks: compose files on disk, nerdctl composer](0008-stacks-compose.md)               | Accepted              |
| 0009 | [Authorization layer with per-action permissions](0009-authorization.md)                | Accepted              |
| 0010 | [Frontend stack](0010-frontend-stack.md)                                                | Accepted              |
| 0011 | [nerdctl version compatibility policy](0011-nerdctl-version-policy.md)                  | Accepted              |
| 0012 | [Supported containerd versions](0012-containerd-version-support.md)                     | Accepted              |
| 0013 | [Container inspect via nerdctl's container.Inspect](0013-container-inspect-via-nerdctl.md) | Accepted |
| 0014 | [Container logs: nerdctl log viewers and xterm.js](0014-container-logs.md) | Accepted |
| 0015 | [Host and runtime information via nerdctl infoutil](0015-host-and-runtime-info.md) | Accepted |
| 0016 | [User management: temporary passwords, disabling, last-admin guard](0016-user-management.md) | Accepted |
| 0017 | [One build per supported nerdctl minor version](0017-builds-per-nerdctl-version.md) | Accepted |
| 0018 | [Background jobs: terminal output, retention, cancellation](0018-jobs-terminal-output.md) | Accepted |
| 0019 | [Registry credentials in the shared docker config](0019-registry-credentials.md) | Accepted |
| 0020 | [Prune: nerdctl's prunes with an estimated preview, no volumes](0020-prune.md) | Accepted |
| 0021 | [Event history in SQLite](0021-event-history.md) | Accepted |
| 0022 | [Web terminal: containerd exec over WebSocket](0022-web-terminal.md) | Accepted |
| 0023 | [Metrics history from Prometheus](0023-metrics-history-prometheus.md) | Accepted |
| 0024 | [Networks: host nerdctl for creation, nerdctl's rules for removal](0024-networks.md) | Accepted |
| 0025 | [Stacks: loading projects with compose-go, adoption in place or by copy](0025-stack-adoption.md) | Accepted |
| 0026 | [Stack operations with the host's nerdctl compose](0026-stack-operations.md) | Accepted |
| 0027 | [Stack file editor: server validation, schema completion, versioned saves](0027-stack-file-editor.md) | Accepted |
| 0028 | [API keys](0028-api-keys.md) | Accepted |
| 0029 | [Image details cache](0029-image-details-cache.md) | Accepted |
| 0030 | [Image update checks](0030-image-update-checks.md) | Accepted |
| 0031 | [Editing and recreating containers](0031-container-recreate.md) | Accepted |
| 0032 | [Automatic updates](0032-automatic-updates.md) | Accepted |
| 0033 | [Delay and approval of automatic updates](0033-automatic-update-guards.md) | Accepted |
| 0034 | [Notifications](0034-notifications.md) | Accepted |
| 0035 | [Running behind a reverse proxy](0035-reverse-proxy.md) | Accepted |
| 0036 | [Audit log](0036-audit-log.md) | Accepted |
| 0037 | [Two-factor authentication (TOTP)](0037-two-factor-authentication.md) | Accepted |
| 0038 | [Certificates from Let's Encrypt (ACME)](0038-acme-certificates.md) | Accepted |
| 0039 | [Git stacks and saved Git credentials](0039-git-stacks.md) | Accepted |
| 0040 | [Automatic updates of Git stacks: polling and webhooks](0040-git-stack-auto-updates.md) | Accepted |
| 0041 | [Incoming webhooks](0041-incoming-webhooks.md) | Accepted |
| 0042 | [Redeploying stacks with new images, and operations on one service](0042-stack-redeploy-and-service-operations.md) | Accepted |
| 0043 | [Container health reports and restarts of unhealthy containers](0043-health-and-auto-heal.md) | Accepted |
| 0044 | [Saving and loading images as tar archives](0044-image-save-and-load.md) | Accepted |
| 0045 | [Browsing and downloading volume contents on the host](0045-volume-browser.md) | Accepted |
| 0046 | [Serving nerdui under a path prefix](0046-base-path.md) | Accepted |
| 0047 | [Backups of nerdui's own data](0047-application-backup.md) | Accepted |
| 0048 | [Scheduled jobs](0048-scheduler.md) | Accepted |
| 0049 | [Encrypted variables of stacks](0049-stack-variables.md) | Accepted |
| 0050 | [Backups of volumes](0050-volume-backups.md) | Accepted |
| 0051 | [Stack templates](0051-stack-templates.md) | Accepted |
| 0052 | [Deleting stacks created in nerdui](0052-delete-managed-stacks.md) | Accepted |
| 0053 | [API keys limited to chosen permissions](0053-api-key-permissions.md) | Accepted |
| 0054 | [Host metrics and alert thresholds](0054-host-metrics.md) | Accepted |
| 0055 | [Editing all files of a stack's directory](0055-stack-files.md) | Accepted |
| 0056 | [Sign-in with an OpenID Connect provider](0056-oidc-sign-in.md) | Accepted |
| 0057 | [Passkeys (WebAuthn)](0057-passkeys.md) | Accepted |
| 0058 | [Roles: built-in and custom](0058-roles.md) | Accepted |
