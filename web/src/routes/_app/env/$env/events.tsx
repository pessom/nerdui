// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  SegmentedControl,
  Stack,
  Switch,
  Table,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import { IconSearch } from "@tabler/icons-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap } from "../../../../api/client";
import { envStatusQuery, keys } from "../../../../api/queries";
import { RouterAnchor } from "../../../../components/RouterAnchor";
import {
  eventColor,
  eventDetails,
  eventLabel,
  eventObject,
} from "../../../../features/events/format";

const envRoute = getRouteApi("/_app/env/$env");

const kinds = [
  { value: "", label: "All" },
  { value: "container", label: "Containers" },
  { value: "task", label: "Tasks" },
  { value: "image", label: "Images" },
  { value: "nerdui", label: "containerd" },
];

/**
 * Event feed (#65, ADR-0021): recorded containerd events, newest first. New events appear
 * because the environment's live event stream refetches the page.
 */
export const Route = createFileRoute("/_app/env/$env/events")({
  component: EventsPage,
});

function EventsPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const [kind, setKind] = useState("");
  const [allNamespaces, setAllNamespaces] = useState(false);
  const [objectInput, setObjectInput] = useState("");
  const [object, setObject] = useState("");
  const queryNs = allNamespaces ? "*" : namespace;

  const events = useInfiniteQuery({
    queryKey: [...keys.env(env), "events", queryNs, kind, object],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/api/v1/environments/{env}/events", {
          params: {
            path: { env },
            query: {
              namespace: queryNs,
              ...(kind ? { kind } : {}),
              ...(object ? { object } : {}),
              ...(pageParam ? { before: pageParam } : {}),
            },
          },
        }),
      ),
    getNextPageParam: (last) => (last.more ? last.events.at(-1)?.id : undefined),
  });
  const rows = events.data?.pages.flatMap((p) => p.events) ?? [];
  const recording = events.data?.pages[0]?.recording ?? true;

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Events</Title>
        {events.isFetching && !events.isPending && <Loader size="xs" aria-label="Refreshing" />}
      </Group>
      <Group gap="sm" wrap="wrap">
        <SegmentedControl aria-label="Event kind" value={kind} onChange={setKind} data={kinds} />
        <TextInput
          aria-label="Filter by object"
          placeholder="Container ID or name, image…"
          leftSection={<IconSearch size={16} />}
          w={260}
          value={objectInput}
          onChange={(e) => setObjectInput(e.currentTarget.value)}
          onKeyDown={(e) => e.key === "Enter" && setObject(objectInput.trim())}
          onBlur={() => setObject(objectInput.trim())}
        />
        <Switch
          label="All namespaces"
          checked={allNamespaces}
          onChange={(e) => setAllNamespaces(e.currentTarget.checked)}
        />
      </Group>
      {!recording && (
        <Alert color="gray" title="Event history is disabled">
          Start nerdui with --events-retention (e.g. 168h) to record events.
        </Alert>
      )}
      {events.isPending && <Loader aria-label="Loading" />}
      {events.isError && (
        <Alert color="red" title="Cannot read events">
          {events.error.message}
        </Alert>
      )}
      {events.data && (
        <Table striped verticalSpacing={4} fz="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th w={190}>Time</Table.Th>
              {allNamespaces && <Table.Th>Namespace</Table.Th>}
              <Table.Th>Event</Table.Th>
              <Table.Th>Object</Table.Th>
              <Table.Th>Details</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {rows.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={5}>
                  <Text c="dimmed" ta="center" py="md">
                    No events recorded yet.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {rows.map((e) => (
              <Table.Tr key={e.id}>
                <Table.Td>
                  <Text size="sm" c="dimmed" title={e.ts}>
                    {new Date(e.ts).toLocaleString()}
                  </Text>
                </Table.Td>
                {allNamespaces && <Table.Td>{e.namespace || "—"}</Table.Td>}
                <Table.Td>
                  <Badge variant="light" color={eventColor(e)}>
                    {eventLabel(e)}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  {(e.kind === "container" || e.kind === "task") && e.objectId ? (
                    <RouterAnchor
                      to="/env/$env/containers/$id"
                      params={{ env, id: e.objectId }}
                      search={{ ns: e.namespace || namespace }}
                      size="sm"
                      ff="monospace"
                      title={e.objectId}
                    >
                      {eventObject(e)}
                    </RouterAnchor>
                  ) : (
                    <Text size="sm" ff="monospace" title={e.objectId}>
                      {eventObject(e) || "—"}
                    </Text>
                  )}
                </Table.Td>
                <Table.Td>
                  <Text size="xs" c="dimmed" ff="monospace">
                    {eventDetails(e)}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      {events.hasNextPage && (
        <Button
          variant="default"
          onClick={() => void events.fetchNextPage()}
          loading={events.isFetchingNextPage}
        >
          Load older events
        </Button>
      )}
    </Stack>
  );
}
