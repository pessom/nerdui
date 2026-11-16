// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Button, Checkbox, Popover, Stack } from "@mantine/core";
import { IconColumns } from "@tabler/icons-react";
import { useHiddenColumns } from "./useHiddenColumns";

/** A popover of checkboxes showing or hiding a table's optional columns (#90). */
export function ColumnsMenu({
  table,
  columns,
}: {
  table: string;
  /** The columns that can be hidden. */
  columns: { id: string; label: string }[];
}) {
  const { hidden, toggle } = useHiddenColumns(table);
  return (
    <Popover position="bottom-end" withArrow shadow="md">
      <Popover.Target>
        <Button variant="default" leftSection={<IconColumns size={16} stroke={1.5} />}>
          Columns
        </Button>
      </Popover.Target>
      <Popover.Dropdown>
        <Stack gap="xs" role="group" aria-label="Visible columns">
          {columns.map((c) => (
            <Checkbox
              key={c.id}
              label={c.label}
              checked={!hidden.includes(c.id)}
              onChange={(e) => toggle(c.id, e.currentTarget.checked)}
            />
          ))}
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}
