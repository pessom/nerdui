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
sudo ./nerdui --listen 127.0.0.1:8080
```

### Container image

```bash
sudo nerdctl compose -f deploy/docker/compose.yaml up -d
```

See [docs/deployment.md](docs/deployment.md) for required mounts and privileges.

## Configuration

| Flag          | Env                | Default                           | Description                    |
| ------------- | ------------------ | --------------------------------- | ------------------------------ |
| `--listen`    | `NERDUI_LISTEN`    | `:8080`                           | HTTP listen address            |
| `--address`   | `NERDUI_ADDRESS`   | `/run/containerd/containerd.sock` | containerd socket              |
| `--namespace` | `NERDUI_NAMESPACE` | `default`                         | Default containerd namespace   |
| `--log-level` | `NERDUI_LOG_LEVEL` | `info`                            | `debug`, `info`, `warn`, `error` |
| `--version`   |                    |                                   | Print version and exit         |

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

TBD.
