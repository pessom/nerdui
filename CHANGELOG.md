# Changelog

All notable user-facing changes are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Detailed per-release notes are generated from Conventional Commits on GitHub Releases.

## [Unreleased]

### Added

- Container health: the container page shows the healthcheck, its status, failing streak and
  last results; a `container.unhealthy` notification reports containers that turn unhealthy,
  and containers labeled `nerdui.auto-heal=true` are restarted, at most 3 times an hour, with a
  `container.autoheal` notification (#22, ADR-0043).
- Healthcheck in the container form: the image's, a custom command with interval, timeout,
  start period and retries, or none, and a "Restart when unhealthy" switch (#237).
- Image archives for hosts without registry access: Save as tar downloads one or more images
  for the host's platform, Load on the images page uploads an archive from `nerdctl save` or
  `docker save` (also gzip-compressed); like `nerdctl save` and `nerdctl load` (#46,
  ADR-0044). Behind nginx, raise `client_max_body_size` (docs/reverse-proxy.md).
- Volume browser: Browse on the volumes page lists a volume's files, previews text files and
  downloads a file, a folder or the whole volume as `.tar.gz`; read-only, confined to the volume
  (#58, ADR-0045).

### Fixed

- Restarting a container from nerdui (also from webhooks and stack operations) no longer sends
  a "container exited" notification.
- Editing a container, automatic updates and webhooks that recreate it keep a healthcheck set
  with `--health-cmd`; before, the new container had none (#237).

## [0.2.0-alpha.1] - 2026-11-22

First preview of 0.2: automatic updates with notifications, Git stacks with push webhooks and
polling, incoming webhooks, audit log, two-factor authentication, Let's Encrypt certificates,
operations on one stack service, container processes and image history.

### Added

- Automatic updates: containers and compose services labeled `nerdui.auto-update=true` ("Update
  automatically" in the container form) are pulled and recreated after a scheduled image check
  finds a newer image; a log of updates is under Settings → Image updates (#54).
- Guards for automatic updates: a minimum time since a new image appeared, and approval when more
  updates than a threshold (5 by default) are due at once (#56).
- Notifications: webhook, Slack, Discord, Telegram, ntfy, Gotify and email channels under
  Settings → Notifications, each receiving the events it subscribes to: new images, automatic
  updates, crashed containers and failed operations (#86, #96).
- Reverse proxy mode: `--trusted-proxies` honors `X-Forwarded-For`, `-Proto` and `-Host` from the
  given addresses, so client addresses, login rate limiting and `Secure` cookies work behind
  Caddy, Traefik or nginx; setup examples in docs/reverse-proxy.md (#112).
- Audit log: every change, terminal session, denied request and login with user, client address
  and result, filterable and exportable as CSV under Audit log; kept for `--audit-retention`
  (90 days by default) (#79).
- Two-factor authentication: TOTP authenticator apps with QR setup and ten backup codes, an
  option to require it for every user, reset by administrators, and `nerdui admin reset-mfa` for
  the last administrator (#75).
- Let's Encrypt certificates: `--tls-acme-domains` gets and renews a trusted certificate over
  TLS-ALPN-01 (port 443) or HTTP-01 (`--tls-acme-http :80`), with the self-signed certificate as
  fallback and a warning in the UI while issuance fails (#113).
- Git stacks: create a stack from a Git repository ("From Git" on the Stacks page), pull it with
  an optional redeploy, and keep `.env` local; tokens and SSH keys for private repositories,
  including generated deploy keys, are saved under Settings → Git credentials (#37, #93).
- Automatic updates of Git stacks: a push webhook for GitHub, GitLab, Gitea or CI, and polling
  of the repository, either announce new commits (`git.update` notification) or pull and
  redeploy the stack where it runs (#38, #39).
- Incoming webhooks: secret URLs under Settings → Incoming webhooks that pull images and
  redeploy, or restart, a stack or container, e.g. from CI after an image was published (#85,
  #91).
- Operations on one stack service: start or stop, restart, and pull its image and recreate it,
  from the service's menu on the stack page, without touching the other services or its
  dependencies (#29).
- Container processes: a Processes tab on the container page lists PID, user, state, CPU,
  memory, start time and command, read from the host's /proc (#20).
- Image history: History in an image's menu lists its build steps with creation time and layer
  sizes, like `nerdctl image history`; it also works in read-only mode (#47).

### Fixed

- Pulling images and deploying a stack (also automatic updates and webhooks) now recreates the
  containers whose image changed: `nerdctl compose up` alone kept containers on the old image
  when only the tag's target changed (ADR-0042).

### Upgrade notes

- Database migrations 7 (automatic update log), 8 (when new images were first seen), 9
  (notification channels), 10 (audit log), 11 (two-factor authentication), 12 (Git stacks), 13
  (automatic updates of Git stacks) and 14 (incoming webhooks) are applied on start.

## [0.1.1] - 2026-11-19

A fix for published ports of stopped containers, container editing and updating, and image
update checks.

### Added

- **Image updates:** nerdui checks whether the tags of images used by containers point to a newer
  image in the registry (one HEAD request per tag, not counted as a pull by Docker Hub) and marks
  them in the containers, images and stacks lists. Checks run on a schedule set in Settings →
  Image updates (off, 1, 6, 12 or 24 hours; every 6 hours by default) and on request with
  "Check for updates". Nothing is pulled or recreated automatically (#53, ADR-0030).
- **Container creation:** an Advanced section sets more networks, a static IPv4 address,
  privileged mode, added and dropped capabilities, devices, DNS servers and extra `/etc/hosts`
  entries, each validated and passed as its own `nerdctl create` argument (#18).
- **Edit and update containers:** Edit on a container's page opens the create form with its
  current settings; saving recreates it under the same name, optionally pulling the image first
  (an update). The old container is renamed and removed only after the new one is created and
  started; otherwise it is restored. Containers of stacks and containers with settings the form
  cannot carry over are not recreated, with the reasons shown (#18, ADR-0031).

### Fixed

- Stopping or restarting a container from nerdui now releases its published host ports, as
  `nerdctl stop` does; before, a stopped container kept them until it was removed.

### Upgrade notes

- Database migration 6 (image update results) runs automatically at start, with a backup.
- A container stopped from nerdui 0.1.0 may still hold its published ports: press Stop on it
  again after upgrading (or run `nerdctl stop` on it) to release them.

## [0.1.0] - 2026-11-18

First stable release (MVP): a web UI for one host's containerd and nerdctl. It contains everything
from the pre-releases below; no functional changes since 0.1.0-rc.1:

- containers with logs, stats (with Prometheus history), web terminal and creation form;
- images, registries, volumes, networks, ports and prune;
- compose stacks: list, adopt, edit, deploy, logs;
- overview dashboard, namespaces, events with history, system information;
- users, API keys, OpenAPI documentation, per-user preferences.

Every release is installed on clean amd64 and arm64 hosts and measured against the
non-functional requirements ([docs/nfr.md](docs/nfr.md)).

### Known limitations

- One host per nerdui instance; multiple environments and remote agents are planned (#69, #70).
- Every user is an administrator; roles are planned (#78).
- Rootless containerd is not supported.
- The container image is experimental (#145); install the binary with systemd.

### Upgrade notes

- From 0.1.0-rc.1: no changes. From earlier pre-releases, read their upgrade notes; database
  migrations run automatically at start, with a backup.

## [0.1.0-rc.1] - 2026-11-18

Release candidate: namespace management, a faster image list and release-readiness checks.

### Added

- **Namespaces:** a Namespaces page lists the environment's containerd namespaces with their
  containers, images and volumes, creates namespaces and removes empty ones. Removal is refused
  while a namespace holds containers, images, volumes, user-defined networks or containerd data;
  well-known namespaces (`default`, `k8s.io`, `moby`, `buildkit`) and the environment's default
  need the name typed. New permissions `namespaces:read` and `namespaces:write` (#195).

### Changed

- The image list is faster with many images: platforms and sizes are computed once per image
  digest and reused for 5 minutes; pulls, removals and prunes through nerdui refresh them at
  once (ADR-0029).

### Upgrade notes

- No database migration.
- Every release is now installed on clean amd64 and arm64 hosts and measured against the NFRs
  ([docs/nfr.md](docs/nfr.md)); `scripts/nfr-bench.sh` measures your own host.

## [0.1.0-beta.1] - 2026-11-16

Overview dashboard, API keys with API documentation, and per-user preferences.

### Added

- **API documentation:** an API page (also at `/api/docs`) documents every endpoint from the
  server's own specification: grouped by area, with the permission it needs, parameters, body and
  responses, and a curl example. The specification is served at `/api/v1/openapi.yaml` and
  `/api/v1/openapi.json` for Postman, Insomnia or client generators (#87).
- **Dashboard:** the Overview page shows the host's containers by state (running, paused,
  stopped, unhealthy), stacks, images, volumes and networks across namespaces (with a breakdown
  per namespace), the size of the images (volume sizes on request) and the free space of the
  filesystems holding containerd's and nerdctl's data, the running containers using most CPU and
  memory (refreshed every 5 s while the page is open) and the recent events (#63).
- **API keys:** scripts and integrations call the API with `Authorization: Bearer <key>`. Keys
  are created from the user menu (API keys), act as their user, can be read-only and expire; the
  key is shown once and only its hash is stored. Keys cannot change the password or manage keys.
  Administrators see and revoke any user's keys on the Users page; a disabled user's keys stop
  working (#87, ADR-0028).
- **Preferences:** each user's interface settings are saved to their account and follow them
  across browsers: theme, date format (relative, local or ISO; the other forms show on hover),
  auto-refresh of lists every 5–60 s in addition to live events, and the visible columns of the
  containers and images tables (a Columns button above them). Open them from the user menu
  (Preferences) (#90).

### Changed

- The theme switch saves the choice to the user's preferences instead of the browser. The
  containers table shows creation times in the chosen date format (relative by default) (#90).

### Upgrade notes

- Two database migrations (API keys, user preferences) run automatically at start; back up the
  data directory before upgrading, as usual.
- The theme was kept in the browser; after the upgrade it starts at "System" until chosen again
  (it is then saved to the account).
- API keys travel as bearer tokens: call the API over HTTPS when using keys from other hosts.

## [0.1.0-alpha.5] - 2026-11-16

Compose stacks: list, adopt, edit, deploy and logs.

### Added

- **Stacks:** a Stacks page lists compose projects of the selected namespace with their status
  (running, partial, stopped, unknown): directories with a compose file in the stacks directory,
  and projects started with `nerdctl compose` elsewhere (shown as unmanaged). A stack page shows
  its services and containers, including containers of services no longer in the compose file
  (#24).
- **Adopt existing projects:** compose projects in `--stacks-scan-dirs` (two levels deep) are
  offered for adoption, and any directory can be entered. A project is adopted in place (its
  directory is registered; "Forget" removes the registration and keeps the files) or by copying
  its compose and .env files into the stacks directory — refused when the project uses other
  files of its directory, such as `./data` bind mounts. Projects are loaded like
  `nerdctl compose` without `-f`, including `compose.override.yaml` (#25).
- **Stack operations:** Deploy (optionally pulling images first or removing orphan containers),
  Stop, Start, Restart, Pull and Down run the host's `nerdctl compose` in the stack directory as
  background jobs; their output streams live in the Jobs panel and the job shows nerdctl's exit
  code. One operation per stack runs at a time. Down can also remove the stack's volumes after
  typing the stack name (#28, #31, ADR-0026).
- **Stack editor:** the stack page has tabs for its compose file and `.env`, edited with YAML
  highlighting, search, folding and completion of keys and values from the Compose schema of
  this build. The project is checked as `nerdctl compose` loads it while typing, with the error at
  its line; saving shows the diff, keeps the previous file as `.bak`, refuses to overwrite a file
  changed on disk or to save during an operation, and can deploy right away. **New stack**
  creates a stack in the stacks directory with a starter compose file (#26, ADR-0027).
- **Stack logs:** a Logs tab on every stack merges the logs of its containers by time, with a
  colored service label per line; services can be hidden, lines filtered or searched (text or
  regular expression), with timestamps, stdout/stderr selection and live follow. Reconnecting
  resumes each container where it stopped; containers created by a deploy are picked up (#30).

### Upgrade notes

- No database migration.
- Stack operations run the host's `nerdctl compose` (ADR-0026); the environment must not be in
  read-only mode to deploy. Editing and adopting stacks only need the stacks directory
  (`--stacks-dir`, default `<data-dir>/stacks`).
- `--stacks-scan-dirs` (comma-separated) makes existing compose projects on the host appear in
  the "Adopt existing" dialog.

## [0.1.0-alpha.4] - 2026-11-16

Volumes, networks and ports.

### Added

- **Volumes:** a Volumes page lists nerdctl's volumes with the containers that use them, labels
  and creation time; sizes are calculated on request. Volumes can be created with labels and
  removed when no container (running or stopped) uses them; removal asks to type the volume
  name, because the data is deleted (#57).
- **Networks:** a Networks page lists CNI networks with driver, subnet, gateway and the
  containers attached to them with their addresses. Bridge (optionally internal), macvlan and
  ipvlan networks are created with the host's `nerdctl network create` (subnet, gateway, IP range,
  parent interface, labels); a network is removed like `nerdctl network rm`, with a warning about
  stopped containers that use it (#60, ADR-0024).
- **Ports:** a Ports page lists the published and exposed ports of the containers of all
  namespaces and highlights conflicts: a host port used by several containers (a stopped one
  cannot start) or already bound by a host process (#62).

### Upgrade notes

- No database migration.
- Creating networks runs the host's `nerdctl network create`, like creating containers; the
  environment must not be in read-only mode.
- The Ports page compares host sockets only when nerdui runs as a binary on the host; in a
  container it shows conflicts between containers only.

## [0.1.0-alpha.3] - 2026-11-15

Metrics history from Prometheus and fixes found on the first real host.

### Added

- **Metrics history:** with a Prometheus server that scrapes containerd's built-in metrics, the
  Stats tab shows CPU, memory, block I/O and PIDs for the last 1 h, 6 h, 24 h or 7 days, also
  for stopped containers. The connection (URL, basic or bearer authentication, connection test)
  is set per environment on the new Settings page; see the deployment guide (#176).

### Fixed

- Registries: a login nerdctl stores under several keys (for port 443 with and without the
  port) is shown as one registry with its aliases; logging out says that all of them are
  removed, as `nerdctl logout` does.
- Jobs: the expanded job's output fills the free height of the panel instead of half of it, and
  can be opened full screen; Escape closes only the full-screen view, not the panel.
- Navigation: Overview is highlighted in the menu when the URL carries a namespace (`?ns=`).
- Container stats: charts always show a five-minute window ending now instead of stretching
  while the history fills up; the time axis is a 24-hour clock (HH:MM) with ticks on whole
  minutes, and the legend shows the time of the point as HH:MM:SS.

### Upgrade notes

- No database migration; the Prometheus connection is stored in the existing settings table.
- The metrics history is optional: enable containerd's `[metrics]` endpoint, scrape it with
  Prometheus and configure the connection on the environment's Settings page (see the
  deployment guide).

## [0.1.0-alpha.2] - 2026-11-13

Images, registries, background jobs, events, stats, terminal and creating containers.

### Added

- **New container:** a form for image, name, command, ports, environment (secret values masked),
  volumes and mounts, network, limits, restart policy, labels and more; it shows the equivalent
  `nerdctl run` command and creates the container with the host's nerdctl as a background job,
  pulling the image first when needed (#17).
- **Container stats:** a Stats tab with live CPU, memory (used / limit), network and block I/O
  rates and PIDs, sampled like `nerdctl stats` every 1–5 s while the tab is open, with charts of
  the last five minutes (#16).
- **Terminal:** a Terminal tab on the container page opens a shell (bash or sh, or any command)
  in a running container as a chosen user and directory, with resize; the process ends with
  the session. `--disable-exec` turns it off (#15, ADR-0022).
- **Events:** feed of containerd events (container and task changes, exits with status, OOM,
  images, containerd connection) with filters by kind, object and namespace; recorded in the
  database for `--events-retention` (default 7 days) (#65, ADR-0021).
- **Images:** list of the namespace's images with the number of containers using each, unpacked
  and downloaded size, platforms and creation time; search, used/unused filter and sorting
  (#42).
- **Pull images** from the Images page for the host platform, another one or all platforms,
  with live progress as in the terminal (#43).
- **Image actions:** tag, push (as a background job, with the host's registry credentials) and
  remove, one by one or selected images at once, with a force option for images used by
  containers (#44).
- **Registries:** log in to and out of registries from the UI; logins are verified by the
  registry and stored in the host's docker config, shared with `nerdctl login`, and used for
  pulls and pushes (#51, ADR-0019).
- **Clean up:** remove stopped containers, dangling or all unused images, unused networks and
  build cache of a namespace as a background job, with an estimate of what goes; volumes are not
  touched (#52, ADR-0020).
- **Background jobs:** long operations run in the background; the header shows running jobs and
  the Jobs panel lists recent ones with their output and a Cancel button (#126, ADR-0018).

### Fixed

- A client subscribing while the containerd event subscription (re)connects no longer receives
  the "connected" state twice.

### Upgrade notes

- The database gets migration 3 (event history); nerdui backs it up before migrating.
- New settings: `--events-retention` (default `168h`, `0` disables the event history) and
  `--disable-exec` (turns off the web terminal).
- Every user can open a terminal in running containers until roles exist (#78); use
  `--disable-exec` if that is not wanted.

## [0.1.0-alpha.1.1] - 2026-11-12

### Fixed

- Choosing a namespace no longer switches to the Overview page: it stays on the current page,
  the Containers menu link keeps it, and the last choice is remembered per environment in the
  browser. The host-wide Overview has no namespace selector (#160).

## [0.1.0-alpha.1] - 2026-11-12

First preview release: manage containers on one Linux host with containerd and nerdctl.

### Added

- **Containers:** list with live updates from containerd events, search (name, image, compose
  project), state filter, sorting, grouping by compose project and namespace selection (#10).
- **Container actions:** start, stop, restart, kill, pause, unpause and remove, also as bulk
  actions on selected containers, with confirmation for destructive ones (#11, #12).
- **Container page:** overview (command, state, restart policy, ports, environment, mounts,
  networks, labels) and the full `nerdctl inspect` JSON with folding, search and copy (#13).
- **Logs:** live stream with follow, last N lines, timestamps, stdout/stderr selection, ANSI
  colors, text or regex filter with highlighted matches, and download (#14).
- **Overview:** containerd, nerdctl and nerdui versions; host (OS, kernel, CPUs, memory, cgroup),
  runtime components (runc, BuildKit, CNI), snapshotters, warnings and containerd plugins (#66).
- **Users:** first administrator created with a one-time setup token from the log; more users
  with temporary passwords that must be changed at the first sign-in; disable, enable and delete
  accounts; change your own password (#74, #121).
- **Security:** HTTPS on port 9443 with a generated self-signed certificate or your own;
  server-side sessions, CSRF protection, login rate limiting; per-action authorization
  (#120–#122).
- **Compatibility checks:** nerdui is read-only when the host nerdctl does not match its build;
  unsupported containerd versions show a warning (#127).
- **Builds for nerdctl 2.4 and 2.3:** release archives and images carry the nerdctl minor in
  their name (`nerdui_<version>_nerdctl2.3_linux_amd64.tar.gz`, `:<version>-nerdctl2.3`). The
  2.4 build supports containerd 2.3 and 2.4, the 2.3 build containerd 2.3 (ADR-0017).
- **Operations:** single static binary with the UI embedded, systemd unit, configuration by flags
  or `NERDUI_*` variables, SQLite state in the data directory with automatic backups before
  migrations, `nerdui healthcheck` and `nerdui admin reset-password` (#118, #119, #137).
- Licensed under AGPL-3.0-only; "Source code" link in the UI, third-party licenses shipped with
  releases; archives and images signed with cosign, with SBOMs.

### Known limitations

- Only containers: images, volumes, networks, stacks and the web terminal come in later
  releases.
- Every user is an administrator; roles are planned (#78).
- The container image is experimental (#145); install the binary with systemd.

[Unreleased]: https://github.com/pessom/nerdui/compare/v0.2.0-alpha.1...HEAD
[0.2.0-alpha.1]: https://github.com/pessom/nerdui/compare/v0.1.1...v0.2.0-alpha.1
[0.1.1]: https://github.com/pessom/nerdui/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/pessom/nerdui/compare/v0.1.0-rc.1...v0.1.0
[0.1.0-rc.1]: https://github.com/pessom/nerdui/compare/v0.1.0-beta.1...v0.1.0-rc.1
[0.1.0-beta.1]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.5...v0.1.0-beta.1
[0.1.0-alpha.5]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.4...v0.1.0-alpha.5
[0.1.0-alpha.4]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.3...v0.1.0-alpha.4
[0.1.0-alpha.3]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.2...v0.1.0-alpha.3
[0.1.0-alpha.2]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.1.1...v0.1.0-alpha.2
[0.1.0-alpha.1.1]: https://github.com/pessom/nerdui/compare/v0.1.0-alpha.1...v0.1.0-alpha.1.1
[0.1.0-alpha.1]: https://github.com/pessom/nerdui/releases/tag/v0.1.0-alpha.1
