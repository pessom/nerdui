# Git stacks

A Git stack is a compose project kept in a Git repository. nerdui clones the repository, deploys
the compose file from it and updates it by pulling. See
[ADR-0039](adr/0039-git-stacks.md).

## Creating a stack

On the **Stacks** page choose **From Git** and enter:

- **Repository URL**: `https://…`, `http://…`, `ssh://…` or `git@host:owner/repo.git`. Do not put
  a password or token in the URL; save it as a credential instead.
- **Name**: the compose project name, suggested from the URL.
- **Branch or tag**: empty for the repository's default branch.
- **Compose file**: the path of the compose file or of its directory in the repository, e.g.
  `deploy` or `deploy/compose.prod.yaml`; empty for the root.
- **Credential**: needed for private repositories. HTTPS URLs use token credentials, SSH URLs
  use SSH key credentials.

The clone runs as a job in the Jobs panel, followed by `compose up` when **Deploy after cloning**
is checked. Repositories are cloned shallowly (newest commit of one branch or tag, without
submodules) into `<data-dir>/git/<name>`.

## Working with a Git stack

- The compose file is read-only in nerdui: change it in the repository.
- `.env` next to the compose file stays local. It is not in the repository (add it to
  `.gitignore`), and pulls keep it.
- **Pull and redeploy** fetches the newest commit and runs `compose up --remove-orphans` when the
  commit changed. The menu next to it has **Pull only**, a redeploy even when nothing changed,
  and one that pulls images first.
- A failed pull (unreachable server, rejected credential, missing compose file) keeps the
  previous commit and shows the error on the stack page.
- **Remove** deletes the clone and its `.env`; bring the stack down first. The repository is not
  touched.

Permissions: creating and removing need `stacks:write`; pulling, deploying and automatic
updates need `stacks:deploy`.

## Automatic updates

Under **Automatic updates** on the stack page (see [ADR-0040](adr/0040-git-stack-auto-updates.md)):

- **On a new commit**:
  - *Off*;
  - *Notify about new commits* sends a `git.update` notification, and the stack page shows the
    new commit;
  - *Pull and redeploy* pulls and runs `compose up --remove-orphans` in every namespace where
    the stack has containers. A stack that is down is only pulled. Updates run as jobs of the
    user `nerdui (Git)`, and failures are sent as `job.failed`.
- **Check the repository**: how often nerdui asks the repository for new commits, or *No
  polling* to rely on the webhook. A check transfers no files.

### Webhook

**Create webhook** shows a URL with a secret token, only once. Add it to the repository as a
push webhook:

- GitHub: Settings → Webhooks → Add webhook, content type `application/json`, event *Just the
  push event*.
- GitLab: Settings → Webhooks, trigger *Push events* (and *Tag push events* for a tag).
- Gitea / Forgejo: Settings → Webhooks → Gitea, trigger *Push*.
- Anything else, e.g. a CI job: `curl -X POST <url>`.

The webhook only makes nerdui check the stack's branch or tag: pushes to other refs are ignored,
and the action is the one chosen under **On a new commit**. Calls are refused while it is *Off*.
nerdui must be reachable from the Git host. Behind a firewall, use polling. **New URL**
replaces the token, for example when it leaked; **Remove** turns the webhook off.

## Credentials

Administrators manage credentials under **Settings → Git credentials**. One credential can
serve any number of stacks; changing it takes effect on their next pull. Secrets are never shown
again after saving; leave the field empty when editing to keep them.

- **Token (HTTPS)**: an access token with read access to the repository (GitHub: a fine-grained
  token with *Contents: read*; GitLab: a deploy or project token with `read_repository`). The
  user name defaults to `git`, which GitHub and GitLab accept for tokens.
- **SSH key**: paste a private key (OpenSSH or PEM; with its passphrase if encrypted) or let
  nerdui generate an ed25519 key. Copy its public key and add it to the repository as a
  read-only deploy key.

Secrets are stored in nerdui's database in the data directory, readable only by the user running
nerdui; keep backups of the data directory as safe as the secrets.

## SSH host keys

The first key an SSH server presents is trusted and stored. If it changes later, pulls fail with
an error about a changed host key. When the change is expected (the server was reinstalled or
rotated its keys), forget the host under **Settings → Git credentials → Trusted SSH hosts**; the
next connection trusts the new key.
