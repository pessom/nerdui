# 0027. Stack file editor: server validation, schema completion, versioned saves

- **Status:** Accepted
- **Date:** 2026-11-16

## Context

Stacks need an editor for `compose.yaml` and `.env` (#26): highlighting, completion from the
Compose schema, errors, validation like `nerdctl compose config` before saving and a diff. The
files may also be edited on the host meanwhile, and a stack operation may be running.
`codemirror-json-schema` was considered for completion but pulls `shiki`, `markdown-it` (with an
`argparse` dependency under Python-2.0, outside the allowed licenses) and a `deepmerge` advisory.

## Decision

- **Editor:** CodeMirror 6 (ADR-0010) with `@codemirror/lang-yaml`, `@codemirror/lint`,
  `@codemirror/autocomplete` and `@codemirror/merge` for the diff, loaded lazily on the file tabs.
- **Completion:** a small module resolves the cursor's place in the YAML (indentation, parent
  keys, list items) in the Compose JSON schema (`$ref`, `allOf`/`oneOf`/`anyOf`,
  `patternProperties`, `items`) and offers that level's keys with their descriptions, or a key's
  enum and boolean values. The schema is served by `GET /api/v1/compose-schema` from compose-go's
  embedded copy, so completion matches the compose-go version of the build (ADR-0025).
- **Validation:** `POST …/stacks/{name}/validate` loads the project like `nerdctl compose`
  (ADR-0025) with the edited content: it is written to hidden temporary files in the stack
  directory, so includes, extends and relative paths resolve as for the real file, and removed
  afterwards; the stack's override file and `.env` (or the edited `.env`) take part. Errors name
  the real file; a line is reported when the loader gives one, else the UI finds the quoted key.
  The editor checks while typing (debounced); saving is allowed only for content that loads.
- **Saving:** `GET/PUT …/stacks/{name}/files/{compose|env}`. A file has a version (SHA-256 of
  its content); a save carries the version it was loaded at and is refused (409) when the file
  changed on disk, or while an operation of the stack runs in any namespace. The previous content
  is kept as `<file>.bak`; writes are atomic (temp file, fsync, rename) and keep the file's mode
  (new `.env`: 0600). The UI shows the diff before saving and offers "Save and deploy".
- **New stacks:** `POST …/stacks` creates `<stacks-dir>/<name>/compose.yaml` (a starter file
  when empty); content that does not load is refused and nothing is left behind.
- Editing applies to managed and external (adopted in place) stacks alike; it needs
  `stacks:write` and no writable nerdctl.

## Consequences

- Completion is structural, not a full JSON-schema validator in the browser; the server's load
  is the authority.
- One backup per file; history and rollback come with Git-backed stacks (v0.2).
- Validation writes short-lived files into the stack directory (root-owned, removed at once).
