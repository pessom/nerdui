# 0013. Container inspect via nerdctl's container.Inspect

- **Status:** Accepted
- **Date:** 2026-11-12

## Context

The container page shows the full configuration (#13): `GET
/api/v1/environments/{env}/containers/{id}/inspect` returns the same JSON as `nerdctl inspect`
(dockercompat mode). ADR-0003 already links nerdctl packages for read operations, and
`pkg/cmd/container` is a dependency since #11.

nerdctl offers two levels to build on:

- `pkg/cmd/container.Inspect` — what the CLI command runs: resolves the reference with
  `containerwalker` (full ID, ID prefix or name), adds port mappings from the data store,
  converts to dockercompat, optionally computes sizes.
- The parts it is made of: `containerinspector.Inspect`, `portutil.LoadPortMappings`,
  `dockercompat.ContainerFromNative`, `imgutil.ResourceUsage`.

## Options considered

### A. Call `container.Inspect` as is

- Pros: least code; output stays identical to `nerdctl inspect` as nerdctl evolves (new
  fields, port sources, size logic) without changes in nerdui.
- Cons: errors are plain strings (`WalkAll` joins them with `fmt.Errorf("%d errors: …")`), so
  error classes are lost: a missing container is recognized by the text "no such container",
  and a containerd outage becomes 500 instead of 503. The reference is matched as an ID
  prefix or a name, so a short reference can match several containers. A 5-second timeout per
  container is fixed inside nerdctl.

### B. Assemble inspect from the nerdctl parts

- Pros: lookup by exact ID with typed errors (404, 503), the same validation as container
  actions, control over timeouts.
- Cons: about 40 more lines that copy the sequence of `containerInspector.Handler`; nerdui must
  follow changes in that sequence when updating nerdctl.

## Decision

Option A. nerdui calls `container.Inspect` with mode `dockercompat` and maps its results:

- the reference is validated with containerd's `identifiers.Validate` (400 otherwise);
- an error containing "no such container", or no entries, → `errdefs.ErrNotFound` (404);
- more than one entry (e.g. a name equal to another container's ID prefix) →
  `errdefs.ErrInvalidArgument` (400) asking for the full ID;
- other errors pass through `classify`, which keeps gRPC classes when nerdctl did not wrap
  them as text.

The UI always uses the full container ID, so prefix and name matching only matters for API
clients, which get the same lookup rules as the CLI.

The UI shows the JSON in CodeMirror 6 (read-only, MIT): highlighting, folding and search come
with it. A hand-written tree viewer (no dependency) and `react-json-view-lite` (no search) were
considered. The editor is loaded lazily on the Inspect tab (about 110 kB gzip), so the main
bundle does not grow.

## Consequences

- The integration test (`internal/runtime/inspect_integration_test.go`) compares key fields
  with `nerdctl inspect` and checks the 404 mapping. If nerdctl changes the error text, that
  test fails, and the mapping must be updated.
- Revisit and switch to option B if: error text matching breaks or becomes ambiguous, users
  hit 500 instead of 503 during containerd outages, the fixed 5-second timeout is too short on
  slow hosts, or `container.Inspect` gains side effects (e.g. writes to stdout or exits the
  process, as `container.Kill` does — see #11).
- Environment variables (which may contain secrets) are visible with `containers:read`, as
  in `nerdctl inspect` and Portainer. Hiding them is a separate permission question for #74.
