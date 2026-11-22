// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Loader,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDebouncedValue } from "@mantine/hooks";
import { IconDownload, IconSearch } from "@tabler/icons-react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { api, unwrap } from "../../api/client";
import type { components } from "../../api/schema";
import { Time } from "../../components/Time";
import { withBase } from "../../lib/base";

type Entry = components["schemas"]["AuditEntry"];
type Result = Entry["result"];

const pageSize = 100;

const periods = [
  { value: "1", label: "Last 24 hours" },
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "all", label: "All time" },
];

const resultColor: Record<Result, string> = {
  ok: "teal",
  failed: "red",
  denied: "orange",
  accepted: "blue",
};

function sinceOf(period: string): string | undefined {
  if (period === "all") return undefined;
  return new Date(Date.now() - Number(period) * 24 * 3600 * 1000).toISOString();
}

/** Audit log of user actions (#79, ADR-0036). */
export const Route = createFileRoute("/_app/audit")({
  component: AuditPage,
});

function AuditPage() {
  const [user, setUser] = useState("");
  const [q, setQ] = useState("");
  const [result, setResult] = useState<string | null>(null);
  const [period, setPeriod] = useState("7");
  // The start of the period, fixed when the period is chosen.
  const [since, setSince] = useState(() => sinceOf("7"));
  const [debouncedQ] = useDebouncedValue(q, 300);
  const [debouncedUser] = useDebouncedValue(user, 300);

  const filters = useMemo(() => {
    const f: Record<string, string> = {};
    if (debouncedUser.trim()) f.user = debouncedUser.trim();
    if (debouncedQ.trim()) f.q = debouncedQ.trim();
    if (result) f.result = result;
    if (since) f.since = since;
    return f;
  }, [debouncedUser, debouncedQ, result, since]);

  const log = useInfiniteQuery({
    queryKey: ["audit", filters],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET("/api/v1/audit", {
          params: {
            query: {
              ...filters,
              result: filters.result as Result | undefined,
              limit: pageSize,
              before: pageParam || undefined,
            },
          },
        }),
      ),
    getNextPageParam: (last) =>
      last.entries.length === pageSize ? last.entries[last.entries.length - 1]?.id : undefined,
  });
  const entries = log.data?.pages.flatMap((p) => p.entries) ?? [];
  const exportHref = withBase(`/api/v1/audit/export?${new URLSearchParams(filters).toString()}`);

  return (
    <Stack>
      <Group justify="space-between" align="flex-end">
        <div>
          <Title order={2}>Audit log</Title>
          <Text size="sm" c="dimmed">
            Who changed what: every change made through nerdui, terminal sessions, denied requests
            and logins. Request contents are not recorded.
          </Text>
        </div>
        <Button
          component="a"
          href={exportHref}
          variant="default"
          leftSection={<IconDownload size={16} />}
        >
          Export CSV
        </Button>
      </Group>
      <Group align="flex-end" wrap="wrap">
        <TextInput
          label="Search"
          placeholder="container, stack, action…"
          leftSection={<IconSearch size={16} />}
          value={q}
          onChange={(e) => setQ(e.currentTarget.value)}
          w={260}
        />
        <TextInput
          label="User"
          value={user}
          onChange={(e) => setUser(e.currentTarget.value)}
          w={180}
        />
        <Select
          label="Result"
          placeholder="Any"
          clearable
          data={["ok", "failed", "denied", "accepted"]}
          value={result}
          onChange={setResult}
          w={160}
        />
        <Select
          label="Period"
          data={periods}
          value={period}
          onChange={(v) => {
            if (!v) return;
            setPeriod(v);
            setSince(sinceOf(v));
          }}
          allowDeselect={false}
          w={180}
        />
      </Group>
      {log.isPending && <Loader aria-label="Loading audit log" />}
      {log.isError && (
        <Alert color="red" title="Cannot read the audit log">
          {log.error.message}
        </Alert>
      )}
      {log.data && entries.length === 0 && <Text c="dimmed">No entries match.</Text>}
      {entries.length > 0 && (
        <Table.ScrollContainer minWidth={800}>
          <Table striped verticalSpacing={4} fz="sm" aria-label="Audit entries">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Time</Table.Th>
                <Table.Th>User</Table.Th>
                <Table.Th>Action</Table.Th>
                <Table.Th>Request</Table.Th>
                <Table.Th>Result</Table.Th>
                <Table.Th>Client</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {entries.map((e) => (
                <Table.Tr key={e.id}>
                  <Table.Td style={{ whiteSpace: "nowrap" }}>
                    <Time value={e.at} />
                  </Table.Td>
                  <Table.Td>
                    {e.user || "—"}
                    {e.apiKey && (
                      <Badge size="xs" variant="light" ml={4}>
                        API key
                      </Badge>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Code>{e.action}</Code>
                  </Table.Td>
                  <Table.Td style={{ wordBreak: "break-all" }}>
                    <Text size="sm">
                      {e.method} {e.path}
                    </Text>
                    {(e.object || e.namespace) && (
                      <Text size="xs" c="dimmed">
                        {[e.environment, e.namespace, e.object].filter(Boolean).join(" · ")}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Badge size="sm" variant="light" color={resultColor[e.result]}>
                      {e.result}
                    </Badge>
                    {e.message && (
                      <Text size="xs" c="dimmed">
                        {e.message}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Tooltip label={e.userAgent || "unknown client"} multiline w={320}>
                      <Text size="sm" span>
                        {e.clientIp || "—"}
                      </Text>
                    </Tooltip>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      {log.hasNextPage && (
        <Group justify="center">
          <Anchor component="button" onClick={() => void log.fetchNextPage()}>
            {log.isFetchingNextPage ? "Loading…" : "Show older entries"}
          </Anchor>
        </Group>
      )}
    </Stack>
  );
}
