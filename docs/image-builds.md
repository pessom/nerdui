# Image builds

nerdui builds images with BuildKit, the same way `nerdctl build` does, and can push them to a
registry afterwards. See [ADR-0061](adr/0061-image-builds.md).

## Requirements

Building needs two BuildKit programs on the host:

- `buildctl`, the client. `nerdctl build` runs it, so it must be on the `PATH` of the nerdui
  service.
- `buildkitd`, the daemon, which must be running.

nerdui does not install or start them. When no daemon answers, the **Build** dialog shows
"BuildKit is not available" with the reason, and the build is not started.

### With nerdctl-full

The nerdctl-full archive contains both programs and a systemd unit:

```bash
sudo systemctl enable --now buildkit
```

### Without nerdctl-full

Install BuildKit from its [releases](https://github.com/moby/buildkit/releases), take the
programs from the archive's `bin/` and put them, for example, into `/usr/local/bin`. Then run
`buildkitd` as a service with the containerd worker, so builds share images and cache with
containerd:

```ini
# /etc/systemd/system/buildkit.service
[Unit]
Description=BuildKit
After=containerd.service
Requires=containerd.service

[Service]
ExecStart=/usr/local/bin/buildkitd --oci-worker=false --containerd-worker=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now buildkit
```

### Where nerdui looks for buildkitd

nerdui checks the same addresses as nerdctl, in this order, and uses the first one that answers:

1. `BUILDKIT_HOST` from the nerdui service environment, when set (for example in
   `/etc/default/nerdui`);
2. `unix:///run/buildkit-<namespace>/buildkitd.sock`, a daemon for this containerd namespace;
3. `unix:///run/buildkit-default/buildkitd.sock`;
4. `unix:///run/buildkit/buildkitd.sock`, the default of `buildkitd`.

To check that the daemon works:

```bash
sudo buildctl --addr unix:///run/buildkit/buildkitd.sock debug workers
```

The list should show a worker for your platform. One daemon serves every namespace: when its
worker belongs to another containerd namespace (`default` unless `buildkitd` was started with
`--containerd-worker-namespace`), nerdctl exports the result and loads it into the namespace
chosen in nerdui. A daemon per namespace on `/run/buildkit-<namespace>/buildkitd.sock` skips
that copy.

## Building an image

On the **Images** page choose **Build**. You need the `images:write` permission. Choose a
source:

- **Dockerfile**: paste a Dockerfile. It is built with an empty context, so `COPY` and `ADD`
  can only take URLs.
- **Git repository**: nerdui clones the repository (newest commit of one branch or tag) and
  builds from it.
  - **Branch or tag**: empty for the default branch.
  - **Context**: a directory in the repository; empty for its root.
  - **Credential**: needed for private repositories. HTTPS URLs use token credentials, SSH URLs
    use SSH key credentials, as for [Git stacks](git-stacks.md).
- **Archive**: upload a `.tar` or `.tar.gz` of the build context. Limits:
  - at most 2 GiB unpacked and 100,000 entries;
  - device files are skipped;
  - symbolic links may not lead outside the context.

For Git and archives, **Dockerfile path** is the Dockerfile's path in the context; empty means
`Dockerfile`.

Common options:

- **Tags**: one or more image names, separated by spaces or commas, e.g.
  `registry.example.com/app:1.0 app:latest`.
- **Build arguments**: `NAME=value` per line. The log hides their values, but they stay in the
  image history. Do not pass passwords or tokens this way.
- **Target**: the stage of a multi-stage Dockerfile to build; empty for the last one.
- **No cache**: build every step again.
- **Pull base images**: check for newer base images even when they are already present.
- **Push after the build**: push every tag. The registry must accept the logins saved on the
  **Registries** page (the same ones as `nerdctl login`).

The API also accepts `platform` (for example `linux/arm64`) to build for one other platform.
That needs QEMU emulation (binfmt) on the host.

## Log and cancelling

The build runs as a job. Its log opens in the Jobs panel and stays there after the build ends.
Cancelling the job stops `buildctl`, and BuildKit drops the unfinished build.

Temporary contexts live in `<data-dir>/builds` and are deleted when the build ends.

## Troubleshooting

- **"BuildKit is not available"**: `buildkitd` is not running or listens elsewhere. Check
  `systemctl status buildkit` and the addresses above.
- **`buildctl: executable file not found`**: `buildctl` is not on the `PATH` of the nerdui
  service. Install it into `/usr/local/bin`.
- **Push fails with `unauthorized`**: log in to the registry on the **Registries** page first.
