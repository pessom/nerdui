# Deployment

> [!WARNING]
> nerdui has root-equivalent access to the host. Bind it to localhost and put it behind an
> SSH tunnel or an authenticating TLS reverse proxy. See [SECURITY.md](../SECURITY.md).

## Requirements

- Linux `amd64` or `arm64`
- **nerdctl 2.4 or 2.3 installed on the host (required)** with CNI plugins, e.g. `nerdctl-full`.
  Every release has one build per nerdctl minor; the build's `major.minor` must match the host
  nerdctl, otherwise nerdui runs read-only ([ADR-0011](adr/0011-nerdctl-version-policy.md),
  [ADR-0017](adr/0017-builds-per-nerdctl-version.md)).
- containerd, supported and tested in CI per build; other versions work with an "unsupported"
  warning ([ADR-0012](adr/0012-containerd-version-support.md)):

  | nerdui build | containerd |
  | --- | --- |
  | `nerdctl2.4` | 2.3 (LTS), 2.4 |
  | `nerdctl2.3` | 2.3 (LTS) — nerdctl 2.3 itself supports containerd up to 2.3 |

  Containers created by nerdui use the host `nerdctl` binary for networking hooks and logging,
  exactly like CLI-created ones ([ADR-0003](adr/0003-nerdctl-binary-runtime-dependency.md))
- root privileges (rootful containerd/nerdctl; rootless is not supported yet)

## Binary with systemd (recommended)

Release archives contain the binary and `deploy/systemd/` (unit and settings file). Archives
are named `nerdui_<version>_nerdctl<minor>_linux_<arch>.tar.gz`; pick the nerdctl minor of the
host (`nerdctl --version`).

```bash
VERSION=0.1.0
ARCH=amd64   # or arm64
NERDCTL=$(nerdctl --version | grep -oE '[0-9]+\.[0-9]+' | head -1)   # build for the host nerdctl: 2.4 or 2.3
curl -fsSLO https://github.com/pessom/nerdui/releases/download/v${VERSION}/nerdui_${VERSION}_nerdctl${NERDCTL}_linux_${ARCH}.tar.gz
tar -xzf nerdui_${VERSION}_nerdctl${NERDCTL}_linux_${ARCH}.tar.gz

sudo install -m 0755 nerdui /usr/local/bin/nerdui
sudo install -m 0644 deploy/systemd/nerdui.service /etc/systemd/system/nerdui.service
sudo install -m 0644 deploy/systemd/nerdui.env /etc/default/nerdui   # edit settings here
sudo systemctl daemon-reload
sudo systemctl enable --now nerdui

systemctl status nerdui
journalctl -u nerdui | grep setup_token   # one-time token for the first-run setup
```

Verify signatures before installing — see [SECURITY.md](../SECURITY.md#verifying-releases).

The unit runs nerdui as root after `containerd.service` (`Wants=`, so nerdui keeps running and
reconnects while containerd restarts), keeps state in `/var/lib/nerdui` (`StateDirectory`, mode
0700) and restarts it on failure. Settings live in `/etc/default/nerdui` as `NERDUI_*`
variables (see `nerdui --help`).

**Upgrade:** replace `/usr/local/bin/nerdui` and `sudo systemctl restart nerdui`. Containers are not
affected by restarting nerdui. When you upgrade the host nerdctl to another minor version, switch to
the matching nerdui build as well (ADR-0011, ADR-0017).

**Uninstall:** `sudo systemctl disable --now nerdui`, remove the unit, `/etc/default/nerdui`,
the binary and — to delete all nerdui data — `/var/lib/nerdui`.

## Container image (experimental)

> [!NOTE]
> The container image is not yet tested end to end; making it a first-class installation method is
> tracked in [#145](https://github.com/pessom/nerdui/issues/145). Use the binary with systemd.


Image: `ghcr.io/pessom/nerdui:<version>-nerdctl<minor>` (multi-arch, distroless, signed with
cosign, SBOM attached), e.g. `0.1.0-nerdctl2.3`; `<version>` and `latest` are the build for the
newest nerdctl, `latest-nerdctl<minor>` follows stable releases of each build. Use the tag that
matches the host nerdctl mounted into the container.

nerdui inside a container manages the **host** containerd, so it needs host mounts and privileges:

| Mount / option            | Why                                                  |
| ------------------------- | ---------------------------------------------------- |
| `/run/containerd`         | containerd socket                                     |
| `/var/lib/containerd`     | snapshot and content paths referenced by containerd   |
| `/var/lib/nerdctl`        | nerdctl names, logs, volumes, network configs         |
| `/etc/cni`, `/opt/cni/bin`, `/var/lib/cni` | CNI configs, plugins, IPAM state     |
| `/run/netns` (`rshared`)  | container network namespaces                          |
| `/usr/local/bin/nerdctl` (same path as on host) | required: OCI hooks, log driver, compose ([ADR-0003](adr/0003-nerdctl-binary-runtime-dependency.md)) |
| `/etc/nerdctl` (ro)       | nerdctl configuration (`nerdctl.toml`)                |
| `/root/.docker`           | registry logins shared with `nerdctl login`           |
| `/var/lib/nerdui`         | nerdui database, TLS certificate, stacks              |
| `--privileged`            | CNI setup, mounts                                     |
| `--net=host`, `--pid=host`| CNI and port forwarding operate in host namespaces    |

Ready-to-use example: [`deploy/docker/compose.yaml`](../deploy/docker/compose.yaml).

```bash
sudo nerdctl compose -f deploy/docker/compose.yaml up -d
```

Equivalent `nerdctl run`:

```bash
sudo nerdctl run -d --name nerdui --restart unless-stopped \
  --privileged --net host --pid host \
  -e NERDUI_LISTEN=127.0.0.1:9443 \
  -v /run/containerd:/run/containerd \
  -v /var/lib/containerd:/var/lib/containerd \
  -v /var/lib/nerdctl:/var/lib/nerdctl \
  -v /opt/cni/bin:/opt/cni/bin:ro \
  -v /etc/cni:/etc/cni \
  -v /var/lib/cni:/var/lib/cni \
  -v /run/netns:/run/netns:rshared \
  -v /usr/local/bin/nerdctl:/usr/local/bin/nerdctl:ro \
  -v /etc/nerdctl:/etc/nerdctl:ro \
  -v /root/.docker:/root/.docker \
  -v /var/lib/nerdui:/var/lib/nerdui \
  ghcr.io/pessom/nerdui:latest
```

## HTTPS and certificates

nerdui serves **HTTPS on port 9443** by default ([ADR-0005](adr/0005-authentication-sessions-tls.md)).

- **Self-signed (default).** On first start nerdui generates an ECDSA certificate in
  `<data-dir>/tls/` for `localhost`, the hostname and all local IP addresses (valid 825 days,
  renewed automatically 30 days before expiry). Browsers show a warning on the first visit.
  The startup log prints the certificate's SHA-256 fingerprint — compare it with the one your
  browser shows before accepting:

  ```bash
  journalctl -u nerdui | grep sha256        # binary as a service
  sudo nerdctl logs nerdui | grep sha256    # container
  ```

- **Your own certificate.** `--tls-cert /path/fullchain.pem --tls-key /path/privkey.pem`. Files are
  re-read when they change (e.g. certbot renewals), no restart needed.
- **Let's Encrypt (ACME).** `--tls-acme-domains nerdui.example.com` (optionally
  `--tls-acme-email you@example.com`) gets and renews a certificate automatically
  ([ADR-0038](adr/0038-acme-certificates.md)). The domain must point to the host, and the CA must
  reach nerdui: either on **port 443** (TLS-ALPN-01; run nerdui with `--listen :443` or forward 443
  to it), or on **port 80** with `--tls-acme-http :80` (HTTP-01; this port also redirects to
  HTTPS). Try `--tls-acme-ca staging` first: Let's Encrypt limits failed attempts on production.
  Certificates and the account key are kept in `<data-dir>/tls/acme`. Other names (IP addresses,
  `localhost`) and the time until the first certificate arrives get the self-signed certificate;
  the UI shows a warning while issuance fails. Hosts without inbound ports (home networks) need
  your own certificate or a reverse proxy with DNS-01 instead.
- **Behind a reverse proxy** that terminates TLS (Caddy, Traefik, nginx): `--tls=false`, bind
  to localhost (`--listen 127.0.0.1:9443`) and trust the proxy (`--trusted-proxies 127.0.0.1,::1`).
  Never expose plain HTTP to the network. Examples: [reverse-proxy.md](reverse-proxy.md).

## First run and account recovery

- **Setup token.** Until the first admin exists, nerdui logs a one-time token
  (`"setup_token": "..."`, level WARN). The setup page asks for it, so only someone with access to
  the host's logs can create the admin. A new token is generated on every restart until setup is
  completed.
- **Sessions.** Server-side, cookie `nerdui_session` (HttpOnly, SameSite=Strict, Secure with TLS);
  idle timeout 24 h, absolute lifetime 30 days. Failed logins are rate limited per IP and per
  username (5 per minute, then blocks of 1–15 minutes) and logged.
- **Event history.** Containerd events are recorded in the database for the Events page; keep
  them for `--events-retention` (default `168h`, `0` disables recording).
- **Web terminal.** Any user can open a shell in running containers (Terminal tab); sessions are
  logged. Start nerdui with `--disable-exec` (`NERDUI_DISABLE_EXEC=true`) to turn it off.
- **Users.** Administrators add users, set temporary passwords, disable and delete accounts on
  the Users page (ADR-0016). A temporary password must be changed at the next sign-in. Every
  user is an administrator until roles are available.
- **Password reset.** `nerdui admin reset-password <user>` on the host prompts for a new password
  (or reads it with `--password-stdin`), enables the account if it was disabled and revokes that
  user's sessions. For the container image:
  `sudo nerdctl exec -it nerdui nerdui admin reset-password admin`.
- **Two-factor authentication.** Each user can turn on a TOTP authenticator app and get ten
  one-time backup codes (account menu → Two-factor authentication). Administrators can require
  it for every user under Settings → Sign-in security, and reset it for a user on the Users page.
  If the only administrator lost both the phone and the backup codes, run
  `nerdui admin reset-mfa <user>` on the host; it also revokes that user's sessions.
- **Audit log.** Changes, terminal sessions, denied requests and sign-ins are recorded with the
  user and client address (Audit log page); kept for `--audit-retention` (default `2160h`, `0`
  keeps everything).

## Backup and moving to another server

Settings → Backup downloads nerdui's own data — users, settings, credentials, notification
channels, webhooks, stack variables with their key (`secret.key`), and the files of managed and
Git stacks — as an archive encrypted with a
password (at least 12 characters). Containers, images and volumes are not included
([ADR-0047](adr/0047-application-backup.md)).

Under Schedules, a *Backup of nerdui* schedule writes such archives to a directory on the host
(default `<data-dir>/backups`) and keeps the newest ones (7 by default). The systemd unit gives
nerdui a private `/tmp`: choose a directory elsewhere, and copy the archives off the host.

To restore, on the same or a new host with nerdui installed:

```bash
systemctl stop nerdui
nerdui admin restore nerdui-backup-20270301-100000.tar.gz.age   # asks for the password
systemctl start nerdui
```

- Pass `--data-dir` and `--stacks-dir` when they differ from the defaults; stack paths are moved
  to them. With `--secret-key-file`, pass it too.
- An existing database or stacks directory is only replaced with `--force`; the previous ones are
  kept as `<name>.before-restore-<time>`.
- Without nerdui, `age -d backup.tar.gz.age | tar tz` lists the archive.

## Volume backups

Settings → Volume backup targets adds a directory on the host or an S3 bucket (for MinIO and
other self-hosted servers, enable path-style addressing). The volumes page's Backups button
backs a volume up now, lists its archives and restores one; a *Backup of a volume* schedule
backs it up regularly and keeps the newest archives ([ADR-0050](adr/0050-volume-backups.md)).
Archives are `<namespace>/<volume>/<volume>-<time>.tar.gz`, plain tar.gz files that `tar xzf`
reads too. Choose *Stop containers* for databases, so their files are copied at rest.

## Stack variables and the secret key

Variables of stacks (Settings → Global variables, a stack's Variables tab) are encrypted in the
database with the key in `<data-dir>/secret.key`, created at the first start
([ADR-0049](adr/0049-stack-variables.md)). Keep it with the database: without it, stored
variables cannot be read and stacks that use them do not deploy. Backups of nerdui include it.
`--secret-key-file` (`NERDUI_SECRET_KEY_FILE`) keeps the key elsewhere, e.g. on another disk.

## Metrics history (Prometheus, optional)

The Stats tab of a container can show CPU, memory, block I/O and PIDs for the last hour up to a
week when a Prometheus server scrapes containerd's built-in metrics (#176, ADR-0023). nerdui only
reads from Prometheus; network traffic is not part of containerd's metrics.

1. Enable the metrics endpoint in `/etc/containerd/config.toml` and restart containerd (running
   containers keep running):

   ```toml
   [metrics]
     address = "127.0.0.1:1338"
   ```

   Check: `curl -s 127.0.0.1:1338/v1/metrics | grep -c '^container_cpu_usage_usec'`.

2. Add a scrape job to `prometheus.yml` and reload Prometheus:

   ```yaml
   scrape_configs:
     - job_name: containerd
       metrics_path: /v1/metrics
       static_configs:
         - targets: ["127.0.0.1:1338"]
   ```

3. In nerdui, open **Settings** of the environment, enter the Prometheus base URL (for example
   `http://127.0.0.1:9090`) and, if the server requires it, basic authentication or a bearer
   token. **Test connection** reports how many containers have metrics; **Save** turns on the
   periods on the Stats tab.

## Health check

- `GET /healthz` returns `200 {"status":"ok"}` without authentication. With the default TLS it is
  served over **HTTPS** only (plain HTTP to the TLS port gets `400`):

  ```bash
  curl -k https://127.0.0.1:9443/healthz   # -k: self-signed certificate
  ```

- `nerdui healthcheck` performs the same probe and exits `0`/`1`; it reads the same `--listen`,
  `--tls`, `--data-dir`, `--tls-cert` and `NERDUI_*` settings as the server, so it works inside the
  (distroless) container without arguments — the compose example uses it as the container
  healthcheck. With TLS it trusts only the certificate nerdui serves from disk (pinned).
- containerd connectivity is reported by `GET /api/v1/version` (`runtime`, `error`) only for a
  signed-in user with the `system:read` permission; it is not meant for anonymous monitoring.
