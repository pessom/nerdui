# Templates

**Templates** (in the menu next to Stacks) deploys applications as new stacks
([ADR-0051](adr/0051-stack-templates.md)), or as single containers
([ADR-0059](adr/0059-container-templates.md)). Templates come from three sources:

- the built-in catalog: about 130 applications by category (databases, caches,
  messaging, web servers and proxies, CMS, productivity, monitoring, logging, search,
  development, communication) and *sandbox* templates;
- registries you add under **Registries**;
- templates you saved, shown as *Mine*.

## Deploying

Choose **Deploy**, enter a stack name and the values of the template's variables, then
**Create and deploy**. A stack deployed this way is an ordinary managed stack, with no link
back to its template:

- Plain values are written to the stack's `.env`.
- Variables marked **Secret** become encrypted variables of the stack (its Variables tab,
  [ADR-0049](adr/0049-stack-variables.md)). They are left out of `.env` and never shown again.
- Leave a secret empty when the application does not need it.

## Sandboxes

The *sandbox* category holds base images (Alpine, Ubuntu, Debian, BusyBox), languages (Python,
Node.js, Go, Rust, Ruby, PHP, Java and others) and build tools (Maven, Gradle, Composer). Such
images exit at once when started as they are; a sandbox template keeps the container running
instead, so you work in it from the container's **Terminal** tab. Files in `/work` are kept in a
volume. *Hello World* is the exception: it prints a greeting and exits, to check that the host
can pull and run containers.

Images that need AppArmor to allow signals inside containers (MySQL, MongoDB and others that
switch user with `gosu`, Elasticsearch, InfluxDB) may hang or fail on Linux 7: see
[AppArmor on Linux 7](deployment.md#apparmor-on-linux-7).

## Deploying as a container

A template with one service (Uptime Kuma, nginx, Redis, whoami, …) can also start a plain
container: choose **Container** in the deploy dialog, enter its name and the variables, then
**Continue**. The new container form opens filled in, so you review every setting before
**Create**:

- Named volumes become `<container>-<volume>`, so each container keeps its own data.
- Settings the form cannot express (other networks, `sysctls`, relative bind mounts such as
  `./config`, …) are listed above the form; the container is created without them.
- The values stay in the browser's memory; they are not put into the address bar.

## Saving your own

On a stack's page, **Save as template** copies its compose file and its `.env`:

- The `.env` becomes the template's `.env.example`.
- Values of names that look like passwords, secrets, tokens or keys are emptied.
- Edit both before saving.
- **New template** on the Templates page starts from scratch.

On a container's page, **Save as template** turns its settings into a one-service compose
file (#19). Environment variables whose names look like passwords, secrets, tokens or keys
become variables without a value in the `.env.example`; the template holds no secrets. The
template can then start containers or stacks, also on other hosts.

## Template format

A template is a compose file and an `.env.example`. In the `.env.example`, each variable is a
`KEY=default` line. The comment lines right above a variable describe it in the deploy form:

```dotenv
# Superuser password
POSTGRES_PASSWORD=
# Port on the host
PORT=5432
```

Use the variables in the compose file as `${PORT:-5432}`. Use `${POSTGRES_PASSWORD:?set it}`
when a value is required.

## Registries

A registry is a `registry.json` served over HTTP(S), in the format of
[Arcane](https://getarcane.app) template registries:

```json
{
  "name": "My templates",
  "templates": [
    {
      "id": "whoami",
      "name": "whoami",
      "description": "Echoes HTTP requests",
      "tags": ["tools"],
      "icon_url": "icons/whoami.svg",
      "documentation_url": "https://github.com/traefik/whoami",
      "compose_url": "whoami/compose.yaml",
      "env_url": "whoami/.env.example"
    }
  ]
}
```

- Relative URLs are resolved against the registry's URL.
- Instead of `compose_url` and `env_url`, a template may carry its files inline as `compose`
  and `env`.
- `categories`, `logo` and `documentation` are accepted as well.

nerdui fetches registries with limits:

| What | Limit |
| --- | --- |
| Request timeout | 15 seconds |
| Size of `registry.json` | 2 MiB |
| Size of each file | 256 KiB |
| Cache | 10 minutes |

A registry that cannot be read is reported above the catalog, and the other templates are
still listed.
