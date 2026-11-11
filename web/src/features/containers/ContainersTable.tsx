// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Anchor,
  Badge,
  Group,
  ScrollArea,
  SegmentedControl,
  Switch,
  Table,
  Text,
  TextInput,
  Tooltip,
  UnstyledButton,
} from "@mantine/core";
import { useLocalStorage } from "@mantine/hooks";
import {
  IconChevronDown,
  IconChevronUp,
  IconSearch,
  IconSelector,
  IconStack2,
} from "@tabler/icons-react";
import {
  columnFilteringFeature,
  createColumnHelper,
  createFilteredRowModel,
  createSortedRowModel,
  globalFilteringFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_datetime,
  tableFeatures,
  useTable,
  type SortingState,
} from "@tanstack/react-table";
import { Fragment, useMemo, useState } from "react";
import {
  displayName,
  groupByProject,
  portLabel,
  portURL,
  searchText,
  stateColor,
  statusText,
  type Container,
} from "./format";

const features = tableFeatures({
  rowSortingFeature,
  columnFilteringFeature,
  globalFilteringFeature,
  sortedRowModel: createSortedRowModel(),
  filteredRowModel: createFilteredRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric, datetime: sortFn_datetime },
});

const col = createColumnHelper<typeof features, Container>();

const columns = col.columns([
  col.accessor((c) => displayName(c), {
    id: "name",
    header: "Name",
    sortFn: "alphanumeric",
    cell: ({ row }) => {
      const c = row.original;
      return (
        <Group gap={6} wrap="nowrap">
          <Text fw={500} size="sm" title={c.id}>
            {displayName(c)}
          </Text>
          {c.compose?.service && (
            <Badge size="xs" variant="outline" color="gray">
              {c.compose.service}
            </Badge>
          )}
        </Group>
      );
    },
  }),
  col.accessor("state", {
    header: "State",
    sortFn: "alphanumeric",
    cell: ({ row }) => {
      const c = row.original;
      return (
        <Group gap={6} wrap="nowrap">
          <Badge color={stateColor[c.state]} variant="light">
            {c.state}
          </Badge>
          {c.health && (
            <Badge
              color={c.health === "healthy" ? "teal" : c.health === "starting" ? "yellow" : "red"}
              variant="dot"
            >
              {c.health}
            </Badge>
          )}
        </Group>
      );
    },
  }),
  col.accessor((c) => statusText(c), {
    id: "status",
    header: "Status",
    enableSorting: false,
    cell: ({ getValue }) => (
      <Text size="sm" c="dimmed">
        {getValue()}
      </Text>
    ),
  }),
  col.accessor("image", {
    header: "Image",
    sortFn: "alphanumeric",
    cell: ({ getValue }) => (
      <Text size="sm" ff="monospace" truncate="end" maw={320} title={getValue()}>
        {getValue()}
      </Text>
    ),
  }),
  col.accessor((c) => c.ports.map(portLabel).join(" "), {
    id: "ports",
    header: "Ports",
    enableSorting: false,
    cell: ({ row }) => <Ports container={row.original} />,
  }),
  col.accessor("createdAt", {
    header: "Created",
    sortFn: "datetime",
    sortDescFirst: true,
    cell: ({ getValue }) => (
      <Text size="sm" c="dimmed" title={getValue()}>
        {new Date(getValue()).toLocaleString()}
      </Text>
    ),
  }),
]);

function Ports({ container }: { container: Container }) {
  if (container.ports.length === 0)
    return (
      <Text size="sm" c="dimmed">
        —
      </Text>
    );
  return (
    <Group gap={4}>
      {container.ports.map((p) => {
        const label = portLabel(p);
        const url = portURL(p, window.location.hostname);
        return url ? (
          <Anchor
            key={label}
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            size="sm"
            ff="monospace"
          >
            {label}
          </Anchor>
        ) : (
          <Text key={label} size="sm" ff="monospace">
            {label}
          </Text>
        );
      })}
    </Group>
  );
}

type StateFilter = "all" | "running";

export function ContainersTable({ containers }: { containers: Container[] }) {
  const [sorting, setSorting] = useState<SortingState>([{ id: "createdAt", desc: true }]);
  const [search, setSearch] = useState("");
  const [stateFilter, setStateFilter] = useLocalStorage<StateFilter>({
    key: "nerdui.containers.state",
    defaultValue: "all",
  });
  const [grouped, setGrouped] = useLocalStorage<boolean>({
    key: "nerdui.containers.grouped",
    defaultValue: true,
  });

  const data = useMemo(
    () =>
      stateFilter === "running" ? containers.filter((c) => c.state === "running") : containers,
    [containers, stateFilter],
  );

  const table = useTable({
    features,
    columns,
    data,
    getRowId: (c) => c.id,
    state: { sorting, globalFilter: search },
    onSortingChange: setSorting,
    onGlobalFilterChange: setSearch,
    globalFilterFn: (row, _columnId, value: string) =>
      searchText(row.original).includes(String(value).toLowerCase()),
  });

  const rows = table.getRowModel().rows;
  const groups = grouped
    ? groupByProject(rows.map((r) => r.original))
    : [{ project: undefined, containers: rows.map((r) => r.original) }];
  const rowById = new Map(rows.map((r) => [r.original.id, r]));
  const running = containers.filter((c) => c.state === "running").length;

  return (
    <>
      <Group mb="sm" justify="space-between" wrap="wrap">
        <Group gap="sm" wrap="wrap">
          <TextInput
            aria-label="Search containers"
            placeholder="Search name, image, project…"
            leftSection={<IconSearch size={16} />}
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            w={280}
          />
          <SegmentedControl
            aria-label="Container state filter"
            value={stateFilter}
            onChange={(v) => setStateFilter(v as StateFilter)}
            data={[
              { value: "all", label: `All (${containers.length})` },
              { value: "running", label: `Running (${running})` },
            ]}
          />
        </Group>
        <Switch
          label="Group by compose project"
          checked={grouped}
          onChange={(e) => setGrouped(e.currentTarget.checked)}
        />
      </Group>

      <ScrollArea>
        <Table striped highlightOnHover verticalSpacing="xs" miw={900}>
          <Table.Thead>
            {table.getHeaderGroups().map((hg) => (
              <Table.Tr key={hg.id}>
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
              </Table.Tr>
            ))}
          </Table.Thead>
          <Table.Tbody>
            {rows.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={columns.length}>
                  <Text c="dimmed" ta="center" py="md">
                    {containers.length === 0
                      ? "No containers in this namespace."
                      : "No containers match the filters."}
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {groups.map((g) => (
              <Fragment key={g.project ?? "__standalone"}>
                {grouped && (g.project || groups.length > 1) && (
                  <Table.Tr>
                    <Table.Td colSpan={columns.length} bg="var(--mantine-color-default-hover)">
                      <Tooltip
                        label={g.project ? "Compose project" : "Not part of a compose project"}
                      >
                        <Group gap={6}>
                          <IconStack2 size={16} />
                          <Text fw={600} size="sm">
                            {g.project ?? "Standalone"}
                          </Text>
                          <Text size="xs" c="dimmed">
                            {g.containers.filter((c) => c.state === "running").length}/
                            {g.containers.length} running
                          </Text>
                        </Group>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                )}
                {g.containers.map((c) => {
                  const row = rowById.get(c.id)!;
                  return (
                    <Table.Tr key={row.id}>
                      {row.getAllCells().map((cell) => (
                        <Table.Td key={cell.id}>
                          <table.FlexRender cell={cell} />
                        </Table.Td>
                      ))}
                    </Table.Tr>
                  );
                })}
              </Fragment>
            ))}
          </Table.Tbody>
        </Table>
      </ScrollArea>
    </>
  );
}
