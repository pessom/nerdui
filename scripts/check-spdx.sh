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
  if ! head -n 5 "$file" | grep -qF "$expected"; then
    echo "missing SPDX header: $file" >&2
    missing=1
  fi
done < <(git ls-files --cached --others --exclude-standard -- '*.go' '*.ts' '*.tsx' '*.js' '*.sh')

if [ "$missing" -ne 0 ]; then
  echo "" >&2
  echo "Add to the top of the file (after a shebang, if any):" >&2
  echo "  // SPDX-FileCopyrightText: $(date +%Y) The nerdui Authors" >&2
  echo "  // $expected" >&2
  exit 1
fi
echo "SPDX headers OK"
