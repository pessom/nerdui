// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Code, Paper, SimpleGrid, Stack, Table, Text, Title } from "@mantine/core";
import type { ReactNode } from "react";
import { timeAgo } from "../../lib/time";
import {
  commandLine,
  envPairs,
  portBindings,
  restartPolicy,
  type ContainerInspect,
} from "./inspect";

/** Key fields of `nerdctl inspect` as separate blocks (#13). */
export function ContainerOverview({ inspect: c }: { inspect: ContainerInspect }) {
  const env = envPairs(c.Config?.Env);
  const labels = Object.entries(c.Config?.Labels ?? {}).sort(([a], [b]) => a.localeCompare(b));
  const mounts = c.Mounts ?? [];
  const networks = Object.entries(c.NetworkSettings?.Networks ?? {});
  const ports = portBindings(c);
  const state = c.State;

  return (
    <Stack>
      <SimpleGrid cols={{ base: 1, md: 2 }}>
        <Block title="General">
          <KeyValues
            rows={[
              ["ID", <Code key="id">{c.Id}</Code>],
              ["Image", c.Image],
              ["Command", c.Path ? <Code key="cmd">{commandLine(c)}</Code> : undefined],
              ["Working dir", c.Config?.WorkingDir],
              ["User", c.Config?.User],
              ["Hostname", c.Config?.Hostname],
              ["Platform", c.Platform],
              ["Created", time(c.Created)],
            ]}
          />
        </Block>
        <Block title="State">
          <KeyValues
            rows={[
              ["Status", state?.Status ? <Badge variant="light">{state.Status}</Badge> : undefined],
              ["Started", time(state?.StartedAt)],
              ["Finished", time(state?.FinishedAt)],
              ["Exit code", state?.Status === "exited" ? String(state.ExitCode ?? "") : undefined],
              ["Error", state?.Error],
              ["PID", state?.Pid ? String(state.Pid) : undefined],
              ["Restart policy", restartPolicy(c)],
              ["Restart count", String(c.RestartCount ?? 0)],
            ]}
          />
        </Block>
      </SimpleGrid>

      <Block title="Ports" empty={ports.length === 0 && "No published ports."}>
        <List head={["Host", "Container"]} rows={ports.map((p) => [p.host, p.container])} mono />
      </Block>

      <Block title="Environment" empty={env.length === 0 && "No environment variables."}>
        <List head={["Name", "Value"]} rows={env} mono />
      </Block>

      <Block title="Mounts" empty={mounts.length === 0 && "No mounts."}>
        <List
          head={["Type", "Source", "Destination", "Mode"]}
          rows={mounts.map((m) => [
            m.Type ?? "",
            m.Name || m.Source || "",
            m.Destination ?? "",
            [m.RW === false ? "ro" : "rw", m.Mode].filter(Boolean).join(","),
          ])}
          mono
        />
      </Block>

      <Block title="Networks" empty={networks.length === 0 && "No networks."}>
        <List
          head={["Network", "IP address", "IPv6 address", "MAC address"]}
          rows={networks.map(([name, n]) => [
            name,
            n.IPAddress ? `${n.IPAddress}/${n.IPPrefixLen ?? ""}` : "",
            n.GlobalIPv6Address ?? "",
            n.MacAddress ?? "",
          ])}
          mono
        />
        {c.HostConfig?.NetworkMode && (
          <Text size="xs" c="dimmed" mt="xs">
            Network mode: {c.HostConfig.NetworkMode}
          </Text>
        )}
      </Block>

      <Block title="Labels" empty={labels.length === 0 && "No labels."}>
        <List head={["Name", "Value"]} rows={labels} mono />
      </Block>
    </Stack>
  );
}

function Block({
  title,
  empty,
  children,
}: {
  title: string;
  empty?: string | false;
  children: ReactNode;
}) {
  return (
    <Paper withBorder p="md" component="section" aria-label={title}>
      <Title order={4} mb="xs">
        {title}
      </Title>
      {empty ? (
        <Text size="sm" c="dimmed">
          {empty}
        </Text>
      ) : (
        children
      )}
    </Paper>
  );
}

function KeyValues({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <Table withRowBorders={false} verticalSpacing={4}>
      <Table.Tbody>
        {rows
          .filter(([, v]) => v !== undefined && v !== "" && v !== false)
          .map(([k, v]) => (
            <Table.Tr key={k}>
              <Table.Th w={140} fw={500} c="dimmed" fz="sm">
                {k}
              </Table.Th>
              <Table.Td fz="sm" style={{ wordBreak: "break-all" }}>
                {v}
              </Table.Td>
            </Table.Tr>
          ))}
      </Table.Tbody>
    </Table>
  );
}

function List({ head, rows, mono }: { head: string[]; rows: string[][]; mono?: boolean }) {
  return (
    <Table.ScrollContainer minWidth={400}>
      <Table striped verticalSpacing={4}>
        <Table.Thead>
          <Table.Tr>
            {head.map((h) => (
              <Table.Th key={h}>{h}</Table.Th>
            ))}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((r, i) => (
            <Table.Tr key={i}>
              {r.map((cell, j) => (
                <Table.Td
                  key={j}
                  fz="sm"
                  ff={mono ? "monospace" : undefined}
                  style={{ wordBreak: "break-all" }}
                >
                  {cell}
                </Table.Td>
              ))}
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

/** "2027-03-01 10:00:00 (5 minutes ago)"; Go's zero time (never happened) is hidden. */
function time(iso: string | undefined): string | undefined {
  if (!iso || iso.startsWith("0001-")) return undefined;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  return `${new Date(t).toLocaleString()} (${timeAgo(iso)})`;
}
