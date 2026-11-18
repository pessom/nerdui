// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Tooltip } from "@mantine/core";
import { IconArrowUp } from "@tabler/icons-react";

/** Marks a stack whose containers run images with registry updates (#53). */
export function StacksUpdateBadge({ count }: { count: number }) {
  if (count === 0) return null;
  return (
    <Tooltip
      label={`${count} container${count === 1 ? "" : "s"} run images with a newer version in the registry. "Pull images and deploy" updates them.`}
    >
      <Badge size="xs" color="teal" variant="light" leftSection={<IconArrowUp size={10} />}>
        update
      </Badge>
    </Tooltip>
  );
}
