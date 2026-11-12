# Release process

Releases are fully automated with [GoReleaser](https://goreleaser.com) in
[`.github/workflows/release.yml`](../.github/workflows/release.yml), triggered by a `v*` tag.

## Versioning

[Semantic Versioning](https://semver.org). Before 1.0, minor versions may contain breaking
changes. Tags with a suffix (`v0.1.0-alpha.1`, `v0.1.0-beta.1`, `v0.2.0-rc.1`) are published as
pre-releases and do not update the
`latest` image tag.

## Steps

1. Make sure `main` is green (CI, CodeQL).
2. Update `CHANGELOG.md`: move _Unreleased_ entries under a new version heading.
3. Commit: `chore(release): v0.1.0`.
4. Tag and push:

   ```bash
   git tag -a v0.1.0 -m "v0.1.0"
   git push origin v0.1.0
   ```

5. The workflow builds and publishes:
   - `nerdui_<version>_nerdctl<minor>_linux_{amd64,arm64}.tar.gz` for every nerdctl build
     (binary, README, CHANGELOG, deploy examples)
   - `checksums.txt` + cosign Sigstore bundle `checksums.txt.sigstore.json` (signature and certificate)
   - SBOMs (SPDX) per archive
   - multi-arch images `ghcr.io/pessom/nerdui:<version>-nerdctl<minor>`, plus `<version>` and
     (stable releases) `latest` for the newest nerdctl build and `latest-nerdctl<minor>` for each;
     signed, with SBOM
   - GitHub Release with notes grouped by Conventional Commit type

## nerdctl builds

Every release is built once per supported nerdctl minor ([ADR-0017](adr/0017-builds-per-nerdctl-version.md)):
`go.mod` links the newest, each older minor has `go.nerdctl-<minor>.mod/.sum`, generated from
`go.mod` with only the nerdctl version changed. The list lives in `NERDCTL_VARIANTS` (Makefile),
the CI integration matrix, `.goreleaser.yaml` and `supportedContainerd` in
`internal/runtime/compat.go`.

- **Any `go.mod` change** (including Dependabot PRs): run `make modfiles` and commit the result;
  CI fails with "run make modfiles" otherwise.
- **Work on an older build locally:** `make build NERDCTL=2.3`, or
  `GOFLAGS=-modfile=go.nerdctl-2.3.mod go test ./...`.

## Runtime version updates

- **nerdctl patch release of an older build:** update its version in `NERDCTL_VARIANTS` and the
  CI matrix, run `make modfiles`.
- **New nerdctl minor** ([ADR-0011](adr/0011-nerdctl-version-policy.md), ADR-0017): one PR that
  - bumps `go.mod` to the new minor;
  - turns the previous minor into a variant (`NERDCTL_VARIANTS`, `go.nerdctl-<minor>.mod`, a
    GoReleaser build, archive and image) and drops the oldest one, so two builds remain;
  - updates the CI matrix (`NERDCTL_VERSION`), `supportedContainerd` with the containerd versions
    each nerdctl release supports, the required status checks of the `main` ruleset and the docs;
  - is followed by a release.
- **containerd matrix** ([ADR-0012](adr/0012-containerd-version-support.md)): when containerd
  releases a new minor or designates a new LTS, update the `integration` matrix in
  `.github/workflows/ci.yml`, ADR-0012, the technical specification, and the required status
  checks of the `main` ruleset (`Integration (containerd X.Y)`).

## Local dry run

```bash
make release-check      # validate .goreleaser.yaml
make release-snapshot   # build everything into ./dist without publishing
```

Snapshot builds of the image require Docker with buildx; signing is skipped locally.

## Repository settings required once

- Actions → General → Workflow permissions: _Read and write_.
- Packages: after the first release, make `ghcr.io/pessom/nerdui` public (if desired) and link it
  to the repository.
- Branch protection on `main`: require CI, PR title, and CodeQL checks; squash-merge only.
