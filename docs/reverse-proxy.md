# Running behind a reverse proxy

A reverse proxy (Caddy, Traefik, nginx, Nginx Proxy Manager) can terminate TLS with a public
certificate and forward to nerdui over plain HTTP on the same host. See
[ADR-0035](adr/0035-reverse-proxy.md).

## nerdui settings

```bash
nerdui serve --tls=false --listen 127.0.0.1:9443 --trusted-proxies 127.0.0.1,::1
```

or in `/etc/default/nerdui` for the systemd service:

```bash
NERDUI_TLS=false
NERDUI_LISTEN=127.0.0.1:9443
NERDUI_TRUSTED_PROXIES=127.0.0.1,::1
```

- `--listen 127.0.0.1:9443` keeps plain HTTP off the network. When the proxy runs in a container,
  listen on the address of the bridge it reaches nerdui by (for the default nerdctl bridge:
  `--listen 10.4.0.1:9443`) and trust that network (`--trusted-proxies 10.4.0.0/24`).
- `--trusted-proxies` lists the IPs or CIDRs of the proxy. Only requests from these addresses may
  set `X-Forwarded-For` (the client address in logs, sessions and login rate limiting),
  `X-Forwarded-Proto` (HTTPS → session cookies are marked `Secure`) and `X-Forwarded-Host` (the
  host that same-origin checks compare with). Anything else ignores the headers. Never trust a
  range that untrusted clients can connect from.
- The terminal, logs and live updates use WebSockets: the proxy must pass `Upgrade` and keep
  connections open for at least an hour.
- To serve nerdui under a path such as `https://example.com/nerdui/`, see
  [Sub-path](#sub-path).

## Caddy

Caddy sets `X-Forwarded-For`, `-Proto` and `-Host` and proxies WebSockets by default:

```caddyfile
nerdui.example.com {
	reverse_proxy 127.0.0.1:9443
}
```

## nginx

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    http2 on;
    server_name nerdui.example.com;
    ssl_certificate     /etc/letsencrypt/live/nerdui.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nerdui.example.com/privkey.pem;

    client_max_body_size 10g;   # image archives (Images → Load); 0 = no limit

    location / {
        proxy_pass http://127.0.0.1:9443;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_read_timeout 1h;
        proxy_buffering off;    # streamed logs and pull progress
        proxy_request_buffering off;  # stream image archives to nerdui as they upload
    }
}
```

Nginx Proxy Manager: add a proxy host for `127.0.0.1:9443` (scheme `http`) with **Websockets
Support** on, and an SSL certificate with **Force SSL**.

## Traefik

With nerdui on the host and Traefik in a container, use the file provider:

```yaml
# dynamic configuration
http:
  routers:
    nerdui:
      rule: Host(`nerdui.example.com`)
      entryPoints: [websecure]
      service: nerdui
      tls:
        certResolver: letsencrypt
  services:
    nerdui:
      loadBalancer:
        servers:
          - url: http://10.4.0.1:9443
```

Traefik forwards WebSockets and the `X-Forwarded-*` headers by default. Trust the network its
container is on, e.g. `--trusted-proxies 10.4.0.0/24` with `--listen 10.4.0.1:9443`.

## Sub-path

`--base-path /nerdui` (`NERDUI_BASE_PATH=/nerdui`) serves the UI, the API and WebSockets under
`/nerdui/` and redirects `/nerdui` to `/nerdui/`; the session cookie is limited to the path. The
proxy may pass the path as is or strip the prefix: nerdui accepts both. See
[ADR-0046](adr/0046-base-path.md).

```caddyfile
example.com {
	handle_path /nerdui/* {
		reverse_proxy 127.0.0.1:9443
	}
	redir /nerdui /nerdui/
}
```

```nginx
location /nerdui/ {
    proxy_pass http://127.0.0.1:9443;    # no path: /nerdui/… is passed as is
    # the proxy_set_header, proxy_read_timeout and buffering lines from the nginx example
}
```

```yaml
# Traefik: the prefix is passed as is
http:
  routers:
    nerdui:
      rule: Host(`example.com`) && PathPrefix(`/nerdui`)
      service: nerdui
```

Webhook URLs shown in the UI (Git stacks, incoming webhooks) include the prefix.

## Checking

After logging in through the proxy:

- the browser's cookie `nerdui_session` is marked `Secure`;
- a wrong password logs your public address, not the proxy's (`journalctl -u nerdui | grep
  "failed login"`);
- the terminal of a running container opens (WebSockets work).
