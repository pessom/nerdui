// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Tooltip } from "@mantine/core";
import type { Stack } from "../../api/queries";

const color: Record<Stack["status"], string> = {
  running: "teal",
  partial: "yellow",
  stopped: "gray",
  unknown: "red",
};

const modeHint: Record<Stack["mode"], string> = {
  managed: "In the stacks directory of nerdui",
  external: "Registered in place from another directory",
  git: "Cloned from a Git repository: pull to update it",
  unmanaged: "Seen only in container labels: nerdui does not know its compose file",
};

/** Status badge of a stack; unknown explains why. */
export function StackStatus({ stack }: { stack: Stack }) {
  const badge = (
    <Badge color={color[stack.status]} variant="light">
      {stack.status}
    </Badge>
  );
  const hint = stack.error ?? (stack.status === "unknown" ? modeHint[stack.mode] : undefined);
  return hint ? (
    <Tooltip label={hint} multiline w={320}>
      {badge}
    </Tooltip>
  ) : (
    badge
  );
}

/** Mode badge: managed, external or unmanaged. */
export function StackMode({ mode }: { mode: Stack["mode"] }) {
  return (
    <Tooltip label={modeHint[mode]}>
      <Badge
        size="xs"
        variant="outline"
        color={mode === "unmanaged" ? "orange" : mode === "git" ? "grape" : "gray"}
      >
        {mode}
      </Badge>
    </Tooltip>
  );
}
