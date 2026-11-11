#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The nerdui Authors
# SPDX-License-Identifier: AGPL-3.0-only
#
# Verifies that every tracked source file carries an SPDX license header.
# Usage: scripts/check-spdx.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

expected="SPDX-License-Identifier: AGPL-3.0-only"
missing=0

while IFS= read -r file; do
  case "$file" in
    web/src/api/schema.d.ts) continue ;; # generated from api/openapi.yaml
  esac
  # Skip generated code (Go convention: "Code generated ... DO NOT EDIT." at the top),
  # e.g. sqlc output in internal/store/db.
  if head -n 1 "$file" | grep -q '^// Code generated .* DO NOT EDIT\.$'; then
    continue
  fi
  if ! head -n 5 "$file" | grep -qF "$expected"; then
    echo "missing SPDX header: $file" >&2
    missing=1
  fi
done < <(git ls-files --cached --others --exclude-standard -- '*.go' '*.ts' '*.tsx' '*.js' '*.sh' '*.sql')

if [ "$missing" -ne 0 ]; then
  echo "" >&2
  echo "Add to the top of the file (after a shebang, if any):" >&2
  echo "  // SPDX-FileCopyrightText: $(date +%Y) The nerdui Authors" >&2
  echo "  // $expected" >&2
  exit 1
fi
echo "SPDX headers OK"
