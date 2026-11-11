# nerdui

Web UI for [nerdctl](https://github.com/containerd/nerdctl) / [containerd](https://containerd.io).

> **Status:** early development. Features and requirements are being defined in [`docs/requirements`](docs/requirements).

nerdui talks to containerd **directly over its gRPC socket** and uses nerdctl's Go packages, so
containers, images, networks and volumes managed from the UI stay fully compatible with the
`nerdctl` CLI. It ships as a **single static binary** with the web UI embedded.

> [!WARNING]
> Access to the containerd socket is equivalent to **root on the host**.
> Never expose nerdui to untrusted networks. See [SECURITY.md](SECURITY.md).

## Quick start

### Binary

```bash
# on a Linux host with containerd + nerdctl
curl -fsSLO https://github.com/pessom/nerdui/releases/latest/download/nerdui_<version>_linux_amd64.tar.gz
tar -xzf nerdui_*_linux_amd64.tar.gz
sudo ./nerdui --listen 127.0.0.1:9443   # then open https://localhost:9443
```

### Container image

```bash
sudo nerdctl compose -f deploy/docker/compose.yaml up -d
```

See [docs/deployment.md](docs/deployment.md) for required mounts and privileges.

## First run

On first start nerdui prints a one-time **setup token** to its log. Open the UI, enter the token and
create the admin account — this prevents anyone else on your network from claiming a fresh install.

```bash
journalctl -u nerdui | grep setup_token        # binary as a service
sudo nerdctl logs nerdui | grep setup_token    # container
```

Lost the admin password? On the host: `sudo nerdui admin reset-password admin` (add
`--data-dir` if you changed it). All sessions of that user are revoked.

## Configuration

Every flag has an environment variable `NERDUI_<FLAG>`; command-line flags take precedence.

| Flag | Env | Default | Description |
| ---- | --- | ------- | ----------- |
| `--listen` | `NERDUI_LISTEN` | `:9443` | Listen address |
| `--address` | `NERDUI_ADDRESS` | `/run/containerd/containerd.sock` | containerd socket |
| `--namespace` | `NERDUI_NAMESPACE` | `default` | Default containerd namespace |
| `--data-dir` | `NERDUI_DATA_DIR` | `/var/lib/nerdui` | nerdui state (database, TLS, stacks); created with mode `0700` |
| `--stacks-dir` | `NERDUI_STACKS_DIR` | `<data-dir>/stacks` | Compose stacks managed by nerdui |
| `--stacks-scan-dirs` | `NERDUI_STACKS_SCAN_DIRS` | — | Comma-separated directories scanned for existing compose projects |
| `--nerdctl-path` | `NERDUI_NERDCTL_PATH` | `nerdctl` | Host nerdctl binary |
| `--skip-nerdctl-version-check` | `NERDUI_SKIP_NERDCTL_VERSION_CHECK` | `false` | Allow an unsupported nerdctl version |
| `--tls` | `NERDUI_TLS` | `true` | Serve HTTPS; `--tls=false` only behind a TLS-terminating reverse proxy |
| `--tls-cert` / `--tls-key` | `NERDUI_TLS_CERT` / `NERDUI_TLS_KEY` | self-signed in `<data-dir>/tls` | Your own PEM certificate and key (reloaded when the files change) |
| `--log-level` | `NERDUI_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Subcommands: `nerdui serve` (default), `nerdui version`, `nerdui healthcheck`, `nerdui admin reset-password <user>`.

## Development

```bash
make deps          # install Go + npm dependencies
make build         # build web UI and binary -> bin/nerdui
make test          # unit tests (Go + web)
make lint          # golangci-lint, eslint, prettier, tsc
make help          # all targets
```

Details: [docs/development.md](docs/development.md).

## Documentation

- [Architecture](docs/architecture.md)
- [Development](docs/development.md)
- [Deployment](docs/deployment.md)
- [Release process](docs/release.md)
- [API (OpenAPI)](api/openapi.yaml)
- [Architecture decision records](docs/adr)
- [Requirements & technical specification](docs/requirements)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

nerdui is licensed under the [GNU Affero General Public License v3.0 only](LICENSE)
(`AGPL-3.0-only`).

- Using nerdui at home or in your organization, unmodified or modified, is free.
- If you run a **modified** version and let others use it over a network, you must offer them
  its source code (AGPL-3.0 section 13). The "Source code" link in the UI serves this purpose.
- A commercial license without AGPL obligations is available — contact the maintainer.

Contributions require signing the [CLA](CLA.md). Third-party licenses are listed in
[NOTICE](NOTICE) and shipped in release artifacts.
