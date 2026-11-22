# 0047. Backups of nerdui's own data

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

Moving nerdui to a new server, or recovering from a lost disk, needs its users, settings,
credentials and stacks (#89). Everything lives in the data directory: the SQLite database, the
managed stacks (`<data-dir>/stacks` or `--stacks-dir`) and the Git stacks (`<data-dir>/git`).
The database holds credentials in plain text (Git tokens and SSH keys, notification tokens,
TOTP secrets, password hashes), protected only by file permissions.

## Options considered

- **Contents:** the database only, or also the stack files (compose files, `.env`), which
  nerdui owns for managed and Git stacks. Stacks adopted from other directories, containers
  and volumes belong to the host.
- **Protection:** an optional or a required password. A backup file is copied around (laptops,
  cloud storage), so a plain one leaks every credential.
- **Encryption format:** own scrypt + AES-GCM code, or age (`filippo.io/age`, BSD-3-Clause), a
  reviewed format with a CLI to decrypt archives by hand.
- **Restore:** upload in the UI and replace the database under the running server, or a CLI
  command on a stopped nerdui.

## Decision

- A backup is a tar.gz of a manifest (format, time, nerdui version, schema version, source
  directories), a `VACUUM INTO` snapshot of the database, the managed stacks and the Git stacks
  (Git clones in progress skipped, symlinks kept), encrypted with age's scrypt passphrase mode
  (work factor 2^16, 64 MiB). The password is required, at least 12 characters.
- `POST /api/v1/backup` (`settings:write`) builds the archive in memory and downloads it;
  Settings → Backup asks for the password twice.
- `nerdui admin restore <file>` (password from the terminal or `--password-stdin`) restores on
  a stopped nerdui: it decrypts and unpacks into a temporary directory confined with `os.Root`,
  opens the database (migrations bring an older backup up to date; a newer one is refused),
  moves stack paths from the source's directories to this host's, and only then puts the
  database, stacks and Git stacks in place. Existing ones need `--force` and are kept as
  `<name>.before-restore-<time>`.
- Scheduled backups to a directory with retention come with the scheduler (#82).

## Consequences

- An archive can be decrypted without nerdui: `age -d backup.tar.gz.age | tar tz`.
- A lost password makes the backup useless; nerdui cannot recover it.
- Sessions and API keys are restored as well: they keep working on the new server.
