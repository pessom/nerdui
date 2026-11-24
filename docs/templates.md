# Stack templates

**Templates** (in the menu next to Stacks) deploys applications as new stacks
([ADR-0051](adr/0051-stack-templates.md)). Templates come from three sources:

- the built-in catalog;
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

## Saving your own

On a stack's page, **Save as template** copies its compose file and its `.env`:

- The `.env` becomes the template's `.env.example`.
- Values of names that look like passwords, secrets, tokens or keys are emptied.
- Edit both before saving.
- **New template** on the Templates page starts from scratch.

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
