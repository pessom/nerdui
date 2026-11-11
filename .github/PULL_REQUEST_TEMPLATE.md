<!--
PR title must follow Conventional Commits, e.g. `feat(containers): add restart action`.
It becomes the squash-merge commit and the release-notes entry.
-->

## Summary

<!-- What does this PR change and why? -->

Closes #

## Type of change

- [ ] `feat` — new feature
- [ ] `fix` — bug fix
- [ ] `refactor` / `perf` — no behavior change / performance
- [ ] `docs` / `test` / `ci` / `build` / `chore`
- [ ] Breaking change (`!` in title, described below)

## How was this tested?

- [ ] Unit tests (`make test`)
- [ ] Integration tests against containerd (`make test-integration`)
- [ ] Manually on a host (containerd / nerdctl versions: ...)

## Checklist

- [ ] I have signed the [CLA](../CLA.md) (the bot will ask on the first PR)
- [ ] `make lint` passes (includes SPDX header check)
- [ ] New dependencies have AGPL-compatible licenses (`make licenses-check`)
- [ ] API changes are reflected in `api/openapi.yaml` and types regenerated (`make api-gen`)
- [ ] Containers/images created by this change are visible and manageable via `nerdctl` CLI
- [ ] Documentation updated (`README.md`, `docs/`)
- [ ] `CHANGELOG.md` updated under _Unreleased_ (user-facing changes)
- [ ] Security impact considered (nerdui has root-equivalent access to the host)

## Screenshots

<!-- For UI changes. -->
