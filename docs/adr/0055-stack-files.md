# 0055. Editing all files of a stack's directory

- **Status:** Accepted
- **Date:** 2026-11-25

## Context

The stack page edits the compose file and `.env` (#26, ADR-0008). Stacks often need more files
next to them: an override file, configs mounted into containers (`./nginx.conf:/etc/…`),
scripts. They had to be changed on the host (#27).

## Options considered

- **More fixed tabs** (override, …): simple, but configs have any name and place.
- **A file tree of the stack's directory** with an editor: any text file, created and deleted
  in place.
- **Upload and archives**: useful for binary files, but larger; left for later.

## Decision

- The stack page gets a **Files** tab: the tree of the stack's directory (directories first,
  `.git`, symbolic links and special files left out, at most 2000 entries) and an editor for
  the selected text file (YAML highlighted). Files can be created (folders as needed) and
  deleted; empty folders too.
- API: `GET …/stacks/{name}/tree`, and `GET`/`PUT`/`DELETE …/stacks/{name}/tree/file?path=`.
  Writes and deletes carry the version that was read, like the compose editor, and wait for
  running operations of the stack (409).
- Paths are relative and checked; every access goes through `os.Root` on the stack's
  directory, so neither `..` nor a symbolic link leaves it.
- The compose file and `.env` keep their tabs, which check them while typing; saved through
  the tree they are checked first and keep a `.bak` as well. The compose file cannot be
  deleted. Other files are saved, then the project is checked with them and a failure is
  shown, since an override file can break it.
- Binary files and files over 1 MiB are listed but not shown or edited; they can be deleted.
- New files are `0644` (readable by containers they are mounted into), `.env*` files `0600`;
  existing files keep their mode.
- The files of a Git stack are shown read-only: they come from the repository.

## Consequences

- Other files have no backup on save or delete.
- Upload, rename and binary files are not covered; files can still be copied to the host.
