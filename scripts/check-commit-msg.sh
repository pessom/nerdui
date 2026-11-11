#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 The nerdui Authors
# SPDX-License-Identifier: AGPL-3.0-only
# Validates a commit message against Conventional Commits 1.0.0.
# Usage: scripts/check-commit-msg.sh <commit-msg-file>
set -euo pipefail

msg_file="${1:?commit message file required}"
first_line="$(head -n1 "$msg_file")"

# Allow merge/revert/fixup commits created by git itself.
if [[ "$first_line" =~ ^(Merge|Revert|fixup!|squash!) ]]; then
  exit 0
fi

types="feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert"
pattern="^(${types})(\([a-z0-9._/-]+\))?!?: .{1,100}$"

if [[ ! "$first_line" =~ $pattern ]]; then
  echo "Invalid commit message: $first_line" >&2
  echo "" >&2
  echo "Expected Conventional Commits format:" >&2
  echo "  <type>(<optional scope>)!: <description>" >&2
  echo "Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert" >&2
  echo "Example: feat(containers): add restart action" >&2
  exit 1
fi
