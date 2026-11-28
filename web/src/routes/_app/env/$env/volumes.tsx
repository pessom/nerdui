// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  SegmentedControl,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDisclosure, useLocalStorage } from "@mantine/hooks";
import { IconArchive, IconFolderOpen, IconPlus, IconRuler, IconTrash } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { envStatusQuery, volumesQuery, type Volume } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import type { UsageFilter } from "../../../../features/images/format";
import { formatBytes } from "../../../../features/system/info";
import { CreateVolumeDialog, RemoveVolumeDialog } from "../../../../features/volumes/VolumeDialogs";
import { VolumeBrowser } from "../../../../features/volumes/VolumeBrowser";
import { VolumeBackupsDialog } from "../../../../features/volumes/VolumeBackupsDialog";
import { Time } from "../../../../components/Time";
import { useCan } from "../../../../lib/permissions";

const envRoute = getRouteApi("/_app/env/$env");

/** Volumes of a namespace (#57): which containers use them, sizes on demand. */
export const Route = createFileRoute("/_app/env/$env/volumes")({
  component: VolumesPage,
});

function VolumesPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(volumesQuery(env, namespace));
  const [withSizes, setWithSizes] = useState(false);
  const sizes = useQuery({ ...volumesQuery(env, namespace, true), enabled: withSizes });
  const sizeOf = new Map(sizes.data?.volumes.map((v) => [v.name, v.size]));
  const [creating, { open, close }] = useDisclosure();
  const [removing, setRemoving] = useState<Volume>();
  const [browsing, setBrowsing] = useState<string>();
  const [backingUp, setBackingUp] = useState<string>();
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  // Browsing, backups and changes run a helper container: volumes:write (ADR-0058).
  const write = useCan()("volumes:write");
  // Same filter as on the Images page; a volume is used when containers mount it.
  const [usage, setUsage] = useLocalStorage<UsageFilter>({
    key: "nerdui.volumes.usage",
    defaultValue: "all",
  });
  const all = list.data?.volumes ?? [];
  const usedCount = all.filter((v) => v.containers.length > 0).length;
  const shown =
    usage === "all" ? all : all.filter((v) => v.containers.length > 0 === (usage === "used"));

  return (
    <Stack>
      <Group justify="space-between">
        <Group gap="xs">
          <Title order={2}>Volumes</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        <Group gap="xs">
          <SegmentedControl
            aria-label="Volume usage filter"
            value={usage}
            onChange={(v) => setUsage(v as UsageFilter)}
            data={[
              { value: "all", label: `All (${all.length})` },
              { value: "used", label: `Used (${usedCount})` },
              { value: "unused", label: `Unused (${all.length - usedCount})` },
            ]}
          />
          <Button
            variant="default"
            leftSection={<IconRuler size={16} />}
            loading={sizes.isFetching}
            onClick={() => (withSizes ? void sizes.refetch() : setWithSizes(true))}
          >
            Calculate sizes
          </Button>
          {write && (
            <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
              <Button leftSection={<IconPlus size={16} />} onClick={open} disabled={readOnly}>
                Create volume
              </Button>
            </Tooltip>
          )}
        </Group>
      </Group>
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot read volumes">
          {list.error.message}
        </Alert>
      )}
      {sizes.isError && (
        <Alert color="red" title="Cannot calculate sizes">
          {sizes.error.message}
        </Alert>
      )}
      {list.data && (
        <Table striped verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Name</Table.Th>
              <Table.Th>Used by</Table.Th>
              <Table.Th>Size</Table.Th>
              <Table.Th>Created</Table.Th>
              <Table.Th w={80} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {shown.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={5}>
                  <Text c="dimmed" ta="center" py="md">
                    {usage === "all" || all.length === 0
                      ? "No volumes in this namespace."
                      : `No ${usage} volumes.`}
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {shown.map((v) => {
              const size = sizeOf.get(v.name);
              const used = v.containers.length > 0;
              return (
                <Table.Tr key={v.name}>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Text
                        size="sm"
                        fw={500}
                        title={v.mountpoint}
                        style={{ wordBreak: "break-all" }}
                      >
                        {v.name}
                      </Text>
                      {v.anonymous && (
                        <Badge size="xs" variant="outline" color="gray">
                          anonymous
                        </Badge>
                      )}
                    </Group>
                    {Object.keys(v.labels).length > 0 && (
                      <Text size="xs" c="dimmed">
                        {Object.entries(v.labels)
                          .map(([k, val]) => (val ? `${k}=${val}` : k))
                          .join(", ")}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    {used ? (
                      <Group gap={6}>
                        {v.containers.map((c) => (
                          <RouterAnchor
                            key={c.id}
                            to="/env/$env/containers/$id"
                            params={{ env, id: c.id }}
                            search={{ ns: namespace }}
                            size="sm"
                            title={c.id}
                          >
                            {c.name || c.id.slice(0, 12)}
                          </RouterAnchor>
                        ))}
                      </Group>
                    ) : (
                      <Text size="sm" c="dimmed">
                        unused
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm">{size != null ? formatBytes(size) : "—"}</Text>
                  </Table.Td>
                  <Table.Td>
                    {v.createdAt ? <Time value={v.createdAt} /> : <Text size="sm">—</Text>}
                  </Table.Td>
                  <Table.Td>
                    {write && (
                      <Group gap={4} wrap="nowrap">
                        <Tooltip label="Browse files">
                          <ActionIcon
                            variant="subtle"
                            color="gray"
                            aria-label={`Browse ${v.name}`}
                            onClick={() => setBrowsing(v.name)}
                          >
                            <IconFolderOpen size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label="Backups">
                          <ActionIcon
                            variant="subtle"
                            color="gray"
                            aria-label={`Backups of ${v.name}`}
                            onClick={() => setBackingUp(v.name)}
                          >
                            <IconArchive size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip
                          label={
                            used
                              ? "Used by containers: remove them first"
                              : readOnly
                                ? "Read-only mode"
                                : "Remove"
                          }
                        >
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label={`Remove ${v.name}`}
                            disabled={used || readOnly}
                            onClick={() => setRemoving(v)}
                          >
                            <IconTrash size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    )}
                  </Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>
      )}
      <CreateVolumeDialog env={env} namespace={namespace} opened={creating} onClose={close} />
      <VolumeBrowser
        env={env}
        namespace={namespace}
        volume={browsing}
        onClose={() => setBrowsing(undefined)}
      />
      <VolumeBackupsDialog
        env={env}
        namespace={namespace}
        volume={backingUp}
        readOnly={readOnly}
        onClose={() => setBackingUp(undefined)}
      />
      <RemoveVolumeDialog
        env={env}
        namespace={namespace}
        volume={removing}
        onClose={() => setRemoving(undefined)}
      />
    </Stack>
  );
}
