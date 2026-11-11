# Deployment

> [!WARNING]
> nerdui has root-equivalent access to the host. Bind it to localhost and put it behind an
> SSH tunnel or an authenticating TLS reverse proxy. See [SECURITY.md](../SECURITY.md).

## Requirements

- Linux `amd64` or `arm64`
- containerd 2.x (1.7 is expected to work but not tested)
- nerdctl installed on the host is recommended (CNI plugins, BuildKit), e.g. `nerdctl-full`
- root privileges

## Binary

```bash
VERSION=0.1.0
ARCH=amd64
curl -fsSLO https://github.com/pessom/nerdui/releases/download/v${VERSION}/nerdui_${VERSION}_linux_${ARCH}.tar.gz
tar -xzf nerdui_${VERSION}_linux_${ARCH}.tar.gz
sudo install -m 0755 nerdui /usr/local/bin/nerdui
sudo nerdui --listen 127.0.0.1:8080
```

Verify signatures before installing — see [SECURITY.md](../SECURITY.md#verifying-releases).

## Container image

Image: `ghcr.io/pessom/nerdui:<version>` (multi-arch, distroless, signed with cosign, SBOM attached).

nerdui inside a container manages the **host** containerd, so it needs host mounts and privileges:

| Mount / option            | Why                                                  |
| ------------------------- | ---------------------------------------------------- |
| `/run/containerd`         | containerd socket                                     |
| `/var/lib/containerd`     | snapshot and content paths referenced by containerd   |
| `/var/lib/nerdctl`        | nerdctl names, logs, volumes, network configs         |
| `/etc/cni`, `/opt/cni/bin`, `/var/lib/cni` | CNI configs, plugins, IPAM state     |
| `/run/netns` (`rshared`)  | container network namespaces                          |
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
  -e NERDUI_LISTEN=127.0.0.1:8080 \
  -v /run/containerd:/run/containerd \
  -v /var/lib/containerd:/var/lib/containerd \
  -v /var/lib/nerdctl:/var/lib/nerdctl \
  -v /opt/cni/bin:/opt/cni/bin:ro \
  -v /etc/cni:/etc/cni \
  -v /var/lib/cni:/var/lib/cni \
  -v /run/netns:/run/netns:rshared \
  ghcr.io/pessom/nerdui:latest
```

## Health check

`GET /healthz` returns `200 {"status":"ok"}`. `GET /api/v1/version` reports containerd connectivity.
