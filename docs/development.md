# Development

## Setup

```bash
brew install go node@26 golangci-lint lefthook goreleaser   # macOS
make deps
lefthook install
```

## Common tasks

```bash
make build            # web + binary for host -> bin/nerdui
make build-linux      # linux binary for host arch -> bin/nerdui-linux-<arch>
GOARCH=amd64 make build-linux
make test             # unit tests
make lint             # all linters
make vuln             # govulncheck
make api-gen          # regenerate web/src/api/schema.d.ts from api/openapi.yaml
make sqlc             # regenerate internal/store/db from internal/store/queries/*.sql
make release-snapshot # local GoReleaser build into dist/
```

## Working against a remote Linux server

containerd and the nerdctl state live on a Linux host, and nerdui must run on that host
(see [architecture](architecture.md#runtime-constraints)). Typical loop from macOS:

```bash
# 1. Build and copy the binary (choose the server arch)
GOARCH=amd64 make build-linux
scp bin/nerdui-linux-amd64 server:/tmp/nerdui

# 2. Run it on the server, bound to localhost
ssh server 'sudo /tmp/nerdui --listen 127.0.0.1:9443 --log-level debug'

# 3. In another terminal: tunnel the port
ssh -N -L 9443:127.0.0.1:9443 server

# 4. Open https://localhost:9443 (accept the self-signed certificate)
```

### Frontend hot reload

Run the backend on the server (steps 2–3 above), then locally:

```bash
make web-dev   # Vite on http://localhost:5173, proxies /api to https://localhost:9443
# or point to another backend:
NERDUI_API_URL=http://localhost:9090 make web-dev
```

The dev proxy rewrites the `Origin` header of API and WebSocket requests to the backend's origin:
nerdui rejects cross-origin unsafe requests and WebSocket upgrades (CSRF protection), and the Vite
dev server is a different origin. Production builds are served by nerdui itself and need no rewrite.

### Backend without a server

`make run ARGS="--address /nonexistent.sock"` starts nerdui without containerd (data in `./.data`): `/healthz`
and the UI work, runtime calls return errors. Useful for UI and API work with fakes.

Forwarding just the socket (`ssh -L /tmp/containerd.sock:/run/containerd/containerd.sock`) works
for pure containerd APIs, but **not** for nerdctl features (networks, logs, volumes) — they need the
host filesystem.

## Integration tests

On a Linux host with containerd + nerdctl:

```bash
sudo -E make test-integration
# custom socket:
sudo -E NERDUI_TEST_ADDRESS=/run/k3s/containerd/containerd.sock make test-integration
```

## Dependency notes

- `typescript` is pinned to `~5.9` because `openapi-typescript` and `typescript-eslint` do not yet
  support TypeScript 6/7.
- containerd and nerdctl Go modules are updated together (Dependabot group `containerd`).
  nerdctl's packages are not a stable public API — review changes on every bump.
