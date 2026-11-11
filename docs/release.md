# Release process

Releases are fully automated with [GoReleaser](https://goreleaser.com) in
[`.github/workflows/release.yml`](../.github/workflows/release.yml), triggered by a `v*` tag.

## Versioning

[Semantic Versioning](https://semver.org). Before 1.0, minor versions may contain breaking
changes. Tags with a suffix (`v0.2.0-rc.1`) are published as pre-releases and do not update the
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
   - `nerdui_<version>_linux_{amd64,arm64}.tar.gz` (binary, README, CHANGELOG, deploy examples)
   - `checksums.txt` + cosign signature/certificate
   - SBOMs (SPDX) per archive
   - multi-arch image `ghcr.io/pessom/nerdui:<version>` (and `latest`), signed, with SBOM
   - GitHub Release with notes grouped by Conventional Commit type

## Runtime version updates

- **nerdctl minor bump** ([ADR-0011](adr/0011-nerdctl-version-policy.md)): one PR that updates
  `go.mod`, `NERDCTL_VERSION` in `.github/workflows/ci.yml` and the docs; followed by a release.
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
