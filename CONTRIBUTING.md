# Contributing to nerdui

## Prerequisites

- Go (version from `go.mod`)
- Node.js 24+ (26 recommended) (see `.nvmrc`)
- [golangci-lint v2](https://golangci-lint.run/welcome/install/)
- [lefthook](https://github.com/evilmartians/lefthook) for git hooks: `lefthook install`
- Optional: [GoReleaser](https://goreleaser.com) for local release snapshots
- A Linux host with containerd + nerdctl for integration testing (see [docs/development.md](docs/development.md))

## License and CLA

nerdui is licensed under [AGPL-3.0-only](LICENSE). Before your first pull request can be
merged, you must sign the [Contributor License Agreement](CLA.md): the CLA bot will comment on
your PR — reply with

```
I have read the CLA Document and I hereby sign the CLA
```

You keep the copyright of your contribution; the CLA allows the maintainer to also distribute
the project under other licenses (e.g. commercial), which funds development.

Every source file must start with an SPDX header (checked by `make spdx-check` and CI):

```go
// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only
```

New dependencies must use a license compatible with AGPL-3.0-only (Apache-2.0, MIT, BSD,
ISC, MPL-2.0). `make licenses-check` and CI enforce this.

## Workflow

1. Open or pick an issue. For larger changes, discuss the approach first; architectural
   decisions are recorded as ADRs in [`docs/adr`](docs/adr).
2. Create a branch from `main`: `feat/containers-restart`, `fix/logs-stream`, …
3. Make changes with tests. Run `make lint test` before pushing.
4. Open a PR using the template. PRs are **squash-merged**.

## Commit messages and PR titles

We use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). This is enforced
by a `commit-msg` hook (lefthook) and by CI on PR titles. Release notes are generated from them.

```
<type>(<optional scope>)!: <description>
```

| Type       | When                                        | In release notes |
| ---------- | ------------------------------------------- | ---------------- |
| `feat`     | New user-facing feature                     | Features         |
| `fix`      | Bug fix                                     | Bug fixes        |
| `perf`     | Performance improvement                     | Performance      |
| `refactor` | Code change without behavior change         | Other            |
| `docs`     | Documentation only                          | —                |
| `test`     | Tests only                                  | —                |
| `build`    | Build system, dependencies                  | Other            |
| `ci`       | CI configuration                            | —                |
| `chore`    | Maintenance                                 | —                |
| `revert`   | Reverts a previous commit                   | Other            |

Add `!` after the type/scope for breaking changes: `feat(api)!: rename /containers endpoint`.

Suggested scopes: `api`, `runtime`, `containers`, `images`, `volumes`, `networks`, `web`, `auth`,
`deps`, `release`.

## Code guidelines

### Go

- Code is formatted with `gofmt`/`goimports` and must pass `golangci-lint` (`make lint-go`).
- All containerd/nerdctl access goes through `internal/runtime`. HTTP handlers depend on
  interfaces, so they can be tested with fakes.
- Prefer nerdctl packages over re-implementing behavior: objects created by nerdui must be
  manageable with the `nerdctl` CLI.
- Wrap errors with context: `fmt.Errorf("pull image %s: %w", ref, err)`.
- Use `log/slog` for logging.

### API

- `api/openapi.yaml` is the source of truth. Update it together with handlers and run
  `make api-gen` to regenerate frontend types. CI fails if generated types are stale.

### Frontend

- TypeScript strict mode, ESLint and Prettier (`make lint-web`).
- Use the typed client from `web/src/api/client.ts`; do not call `fetch` directly.

## Tests

| Command                 | What                                                         |
| ----------------------- | ------------------------------------------------------------ |
| `make test-go`          | Go unit tests with `-race`                                    |
| `make test-web`         | Vitest + Testing Library                                      |
| `make test-integration` | Tests tagged `integration` against a real containerd (root)   |

Integration tests live next to the code in `*_integration_test.go` files with
`//go:build integration`. They run in CI on every PR using `nerdctl-full`.
