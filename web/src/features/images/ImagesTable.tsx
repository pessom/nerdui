// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Badge,
  Button,
  Checkbox,
  Group,
  Paper,
  ScrollArea,
  SegmentedControl,
  Table,
  Text,
  TextInput,
  Tooltip,
  UnstyledButton,
  VisuallyHidden,
} from "@mantine/core";
import { useLocalStorage } from "@mantine/hooks";
import {
  IconChevronDown,
  IconChevronUp,
  IconSearch,
  IconSelector,
  IconTrash,
} from "@tabler/icons-react";
import {
  columnFilteringFeature,
  createColumnHelper,
  createFilteredRowModel,
  createSortedRowModel,
  globalFilteringFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  sortFn_datetime,
  tableFeatures,
  useTable,
  type SortingState,
} from "@tanstack/react-table";
import { useMemo, useState } from "react";
import type { Image } from "../../api/queries";
import { timeAgo } from "../../lib/time";
import { formatBytes } from "../system/info";
import { displayName, matchesUsage, searchText, shortID, type UsageFilter } from "./format";
import { ImageRowActions, type ImageActionHandlers } from "./ImageActions";

const features = tableFeatures({
  rowSortingFeature,
  columnFilteringFeature,
  globalFilteringFeature,
  sortedRowModel: createSortedRowModel(),
  filteredRowModel: createFilteredRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric, basic: sortFn_basic, datetime: sortFn_datetime },
});

const col = createColumnHelper<typeof features, Image>();

const columns = col.columns([
  col.accessor((i) => displayName(i), {
    id: "name",
    header: "Image",
    sortFn: "alphanumeric",
    cell: ({ row }) => {
      const i = row.original;
      return (
        <Text
          size="sm"
          fw={500}
          ff="monospace"
          title={i.name}
          c={i.repository ? undefined : "dimmed"}
        >
          {displayName(i)}
        </Text>
      );
    },
  }),
  col.accessor((i) => shortID(i.digest), {
    id: "id",
    header: "ID",
    enableSorting: false,
    cell: ({ row }) => (
      <Text size="sm" ff="monospace" c="dimmed" title={row.original.digest}>
        {shortID(row.original.digest)}
      </Text>
    ),
  }),
  col.accessor("containers", {
    header: "Used by",
    sortFn: "basic",
    sortDescFirst: true,
    cell: ({ getValue }) => {
      const n = getValue();
      return n > 0 ? (
        <Badge variant="light" color="teal">
          {n === 1 ? "1 container" : `${n} containers`}
        </Badge>
      ) : (
        <Badge variant="light" color="gray">
          unused
        </Badge>
      );
    },
  }),
  col.accessor((i) => i.platforms.join(", "), {
    id: "platforms",
    header: "Platforms",
    enableSorting: false,
    cell: ({ getValue }) => (
      <Text size="sm" c="dimmed">
        {getValue() || "—"}
      </Text>
    ),
  }),
  col.accessor("size", {
    header: "Size",
    sortFn: "basic",
    sortDescFirst: true,
    cell: ({ row }) => (
      <Tooltip
        label={`Unpacked ${formatBytes(row.original.size)}, downloaded content ${formatBytes(
          row.original.contentSize,
        )}`}
      >
        <Text size="sm">{formatBytes(row.original.size)}</Text>
      </Tooltip>
    ),
  }),
  col.accessor("createdAt", {
    header: "Created",
    sortFn: "datetime",
    sortDescFirst: true,
    cell: ({ getValue }) => (
      <Text size="sm" c="dimmed" title={new Date(getValue()).toLocaleString()}>
        {timeAgo(getValue())}
      </Text>
    ),
  }),
]);

/** Image list (#42): search, used/unused filter, sorting; actions and bulk removal (#44). */
export function ImagesTable({
  images,
  actions,
}: {
  images: Image[];
  /** Tag, push and remove; without them the table is read-only. */
  actions?: ImageActionHandlers;
}) {
  const [selectedNames, setSelectedNames] = useState<ReadonlySet<string>>(new Set());
  const [sorting, setSorting] = useState<SortingState>([{ id: "name", desc: false }]);
  const [search, setSearch] = useState("");
  const [usage, setUsage] = useLocalStorage<UsageFilter>({
    key: "nerdui.images.usage",
    defaultValue: "all",
  });
  const data = useMemo(() => images.filter((i) => matchesUsage(i, usage)), [images, usage]);
  const table = useTable({
    features,
    columns,
    data,
    getRowId: (i) => i.name,
    state: { sorting, globalFilter: search },
    onSortingChange: setSorting,
    onGlobalFilterChange: setSearch,
    globalFilterFn: (row, _columnId, value: string) =>
      searchText(row.original).includes(String(value).toLowerCase()),
  });
  const rows = table.getRowModel().rows;
  const used = images.filter((i) => i.containers > 0).length;
  const total = images.reduce((sum, i) => sum + i.size, 0);
  // Selection survives refetches; removed images drop out of it.
  const selected = images.filter((i) => selectedNames.has(i.name));
  const visible = rows.map((r) => r.original);
  const allVisible = visible.length > 0 && visible.every((i) => selectedNames.has(i.name));
  const someVisible = visible.some((i) => selectedNames.has(i.name));
  const toggle = (names: string[], on: boolean) =>
    setSelectedNames((prev) => {
      const next = new Set(prev);
      for (const n of names) {
        if (on) next.add(n);
        else next.delete(n);
      }
      return next;
    });
  const selectable = !!actions && !actions.disabled;
  const extraCols = actions ? 2 : 0;

  return (
    <>
      <Group mb="sm" justify="space-between" wrap="wrap">
        <Group gap="sm" wrap="wrap">
          <TextInput
            aria-label="Search images"
            placeholder="Search name, digest, platform…"
            leftSection={<IconSearch size={16} />}
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            w={280}
          />
          <SegmentedControl
            aria-label="Image usage filter"
            value={usage}
            onChange={(v) => setUsage(v as UsageFilter)}
            data={[
              { value: "all", label: `All (${images.length})` },
              { value: "used", label: `Used (${used})` },
              { value: "unused", label: `Unused (${images.length - used})` },
            ]}
          />
        </Group>
        <Text size="sm" c="dimmed">
          {formatBytes(total)} unpacked in total
        </Text>
      </Group>
      {actions && selected.length > 0 && (
        <Paper withBorder p="xs" mb="sm">
          <Group justify="space-between">
            <Text size="sm">{selected.length} selected</Text>
            <Group gap="xs">
              <Button size="xs" variant="default" onClick={() => setSelectedNames(new Set())}>
                Clear
              </Button>
              <Button
                size="xs"
                color="red"
                leftSection={<IconTrash size={14} />}
                disabled={!!actions.disabled}
                onClick={() => actions.remove(selected)}
              >
                Remove…
              </Button>
            </Group>
          </Group>
        </Paper>
      )}
      <ScrollArea>
        <Table striped highlightOnHover verticalSpacing="xs" miw={800}>
          <Table.Thead>
            {table.getHeaderGroups().map((hg) => (
              <Table.Tr key={hg.id}>
                {actions && (
                  <Table.Th w={40}>
                    <Checkbox
                      aria-label="Select all shown images"
                      disabled={!selectable}
                      checked={allVisible}
                      indeterminate={someVisible && !allVisible}
                      onChange={(e) =>
                        toggle(
                          visible.map((i) => i.name),
                          e.currentTarget.checked,
                        )
                      }
                    />
                  </Table.Th>
                )}
                {hg.headers.map((h) => {
                  const sorted = h.column.getIsSorted();
                  const Icon =
                    sorted === "asc"
                      ? IconChevronUp
                      : sorted === "desc"
                        ? IconChevronDown
                        : IconSelector;
                  return (
                    <Table.Th
                      key={h.id}
                      aria-sort={
                        sorted === "asc"
                          ? "ascending"
                          : sorted === "desc"
                            ? "descending"
                            : undefined
                      }
                    >
                      {h.column.getCanSort() ? (
                        <UnstyledButton
                          onClick={h.column.getToggleSortingHandler()}
                          fw={600}
                          fz="sm"
                        >
                          <Group gap={4} wrap="nowrap">
                            <table.FlexRender header={h} />
                            <Icon size={14} />
                          </Group>
                        </UnstyledButton>
                      ) : (
                        <table.FlexRender header={h} />
                      )}
                    </Table.Th>
                  );
                })}
                {actions && (
                  <Table.Th w={48}>
                    <VisuallyHidden>Actions</VisuallyHidden>
                  </Table.Th>
                )}
              </Table.Tr>
            ))}
          </Table.Thead>
          <Table.Tbody>
            {rows.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={columns.length + extraCols}>
                  <Text c="dimmed" ta="center" py="md">
                    {images.length === 0
                      ? "No images in this namespace."
                      : "No images match the filters."}
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {rows.map((row) => (
              <Table.Tr
                key={row.id}
                bg={
                  selectedNames.has(row.original.name)
                    ? "var(--mantine-primary-color-light)"
                    : undefined
                }
              >
                {actions && (
                  <Table.Td>
                    <Checkbox
                      aria-label={`Select ${displayName(row.original)}`}
                      disabled={!selectable}
                      checked={selectedNames.has(row.original.name)}
                      onChange={(e) => toggle([row.original.name], e.currentTarget.checked)}
                    />
                  </Table.Td>
                )}
                {row.getAllCells().map((cell) => (
                  <Table.Td key={cell.id}>
                    <table.FlexRender cell={cell} />
                  </Table.Td>
                ))}
                {actions && (
                  <Table.Td>
                    <ImageRowActions image={row.original} actions={actions} />
                  </Table.Td>
                )}
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </ScrollArea>
    </>
  );
}
