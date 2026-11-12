// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Card,
  Code,
  Group,
  List,
  Loader,
  ScrollArea,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { IconAlertTriangle, IconSearch } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { envInfoQuery, type SystemInfo } from "../../api/queries";
import { InfoCard, Row } from "../../components/InfoCard";
import {
  cgroupLabel,
  countByStatus,
  featureLabels,
  filterPlugins,
  formatBytes,
  type PluginStatus,
} from "./info";

const statusColor: Record<PluginStatus, string> = { ok: "teal", skip: "gray", error: "red" };

/** Host, runtime components, warnings and containerd plugins of an environment (#66). */
export function SystemInfoSection({ env }: { env: string }) {
  const info = useQuery(envInfoQuery(env));
  if (info.isPending) return <Loader aria-label="Loading host information" />;
  if (info.isError)
    return (
      <Alert color="gray" title="Host information unavailable">
        {info.error.message}
      </Alert>
    );
  const i = info.data;
  return (
    <Stack>
      <Notices info={i} />
      <SimpleGrid cols={{ base: 1, md: 2 }}>
        <HostCards info={i} />
      </SimpleGrid>
      <Plugins plugins={i.plugins} />
    </Stack>
  );
}

function Notices({ info }: { info: SystemInfo }) {
  return (
    <>
      {info.warnings.length > 0 && (
        <Alert color="yellow" icon={<IconAlertTriangle size={18} />} title="Host warnings">
          <List size="sm">
            {info.warnings.map((w) => (
              <List.Item key={w}>{w.replace(/^WARNING:\s*/, "")}</List.Item>
            ))}
          </List>
        </Alert>
      )}
      {info.deprecations.length > 0 && (
        <Alert color="orange" title="containerd deprecations">
          <List size="sm">
            {info.deprecations.map((d) => (
              <List.Item key={d.id}>
                {d.message} <Code>{d.id}</Code>
              </List.Item>
            ))}
          </List>
        </Alert>
      )}
    </>
  );
}

function HostCards({ info: i }: { info: SystemInfo }) {
  const version = (name: string) => {
    const c = i.components.find((c) => c.name === name);
    if (!c) return "—";
    if (!c.version)
      return (
        <Text size="sm" c="dimmed">
          unknown{c.error && ` (${c.error})`}
        </Text>
      );
    return c.commit ? (
      <Tooltip label={`commit ${c.commit}`}>
        <span>{c.version}</span>
      </Tooltip>
    ) : (
      c.version
    );
  };
  const features = Object.entries(featureLabels) as [keyof SystemInfo["features"], string][];
  return (
    <>
      <InfoCard title="Host">
        <Row label="Hostname" value={i.hostname} />
        <Row label="Operating system" value={i.operatingSystem || "—"} />
        <Row label="Kernel" value={i.kernelVersion} />
        <Row label="Architecture" value={`${i.osType}/${i.architecture}`} />
        <Row label="CPUs" value={i.cpus} />
        <Row label="Memory" value={i.memoryBytes ? formatBytes(i.memoryBytes) : "—"} />
        <Row label="cgroup" value={cgroupLabel(i)} />
        <Row label="Rootless" value={i.rootless ? "yes" : "no"} />
        <Row
          label="Features"
          value={
            <Group gap={4}>
              {features.map(([k, label]) => (
                <Badge
                  key={k}
                  size="sm"
                  variant="light"
                  color={i.features[k] ? "teal" : "gray"}
                  td={i.features[k] ? undefined : "line-through"}
                  title={i.features[k] ? "supported" : "not supported"}
                >
                  {label}
                </Badge>
              ))}
            </Group>
          }
        />
      </InfoCard>
      <InfoCard title="Runtime">
        <Row label="runc" value={version("runc")} />
        <Row label="BuildKit (buildctl)" value={version("buildctl")} />
        <Row
          label="CNI plugins"
          value={
            <Stack gap={0}>
              <Text size="sm">
                {i.cni.version ?? "unknown"} <Code>{i.cni.path}</Code>
              </Text>
              {i.cni.error && (
                <Text size="xs" c="dimmed">
                  {i.cni.error}
                </Text>
              )}
            </Stack>
          }
        />
        <Row label="Default snapshotter" value={i.snapshotter} />
        <Row label="Snapshotters" value={i.snapshotters.join(", ") || "—"} />
        <Row label="Log drivers" value={i.logDrivers.join(", ") || "—"} />
        <Row label="Security options" value={i.securityOptions.join(", ") || "—"} />
        <Row label="containerd ID" value={<Code>{i.id}</Code>} />
      </InfoCard>
    </>
  );
}

function Plugins({ plugins }: { plugins: SystemInfo["plugins"] }) {
  const [text, setText] = useState("");
  const [status, setStatus] = useState<PluginStatus | "all">("all");
  const counts = countByStatus(plugins);
  const shown = filterPlugins(plugins, text, status);
  return (
    <Card withBorder component="section" aria-label="containerd plugins">
      <Group justify="space-between" mb="xs" wrap="wrap">
        <Title order={4}>containerd plugins</Title>
        <Group gap="xs" wrap="wrap">
          <TextInput
            size="xs"
            placeholder="Filter by type or ID"
            aria-label="Filter plugins"
            leftSection={<IconSearch size={14} />}
            value={text}
            onChange={(e) => setText(e.currentTarget.value)}
          />
          <SegmentedControl
            size="xs"
            aria-label="Plugin status"
            value={status}
            onChange={(v) => setStatus(v as PluginStatus | "all")}
            data={[
              { value: "all", label: `All ${plugins.length}` },
              { value: "ok", label: `ok ${counts.ok}` },
              { value: "skip", label: `skip ${counts.skip}` },
              { value: "error", label: `error ${counts.error}` },
            ]}
          />
        </Group>
      </Group>
      <ScrollArea.Autosize mah={420}>
        <Table striped highlightOnHover stickyHeader>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Type</Table.Th>
              <Table.Th>ID</Table.Th>
              <Table.Th>Platforms</Table.Th>
              <Table.Th>Status</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {shown.map((p) => (
              <Table.Tr key={`${p.type}/${p.id}`}>
                <Table.Td>
                  <Text size="sm" ff="monospace">
                    {p.type}
                  </Text>
                </Table.Td>
                <Table.Td>{p.id}</Table.Td>
                <Table.Td>
                  <Text size="xs" c="dimmed">
                    {p.platforms?.join(", ")}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Stack gap={2}>
                    <Badge size="sm" variant="light" color={statusColor[p.status]}>
                      {p.status}
                    </Badge>
                    {p.error && (
                      <Text size="xs" c="dimmed" maw={360}>
                        {p.error}
                      </Text>
                    )}
                  </Stack>
                </Table.Td>
              </Table.Tr>
            ))}
            {shown.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={4}>
                  <Text size="sm" c="dimmed">
                    No plugins match
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
          </Table.Tbody>
        </Table>
      </ScrollArea.Autosize>
    </Card>
  );
}
