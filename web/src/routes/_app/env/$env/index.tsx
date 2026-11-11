// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Card, Group, Loader, SimpleGrid, Stack, Table, Text, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { envStatusQuery, versionQuery } from "../../../../api/queries";

/** Environment overview: connectivity and runtime versions (host details come with #66). */
export const Route = createFileRoute("/_app/env/$env/")({
  component: Overview,
});

function Overview() {
  const { env } = Route.useParams();
  const status = useQuery(envStatusQuery(env));
  const version = useQuery(versionQuery);

  if (status.isPending) return <Loader aria-label="Loading" />;
  if (status.isError) return <Text c="red">{status.error.message}</Text>;
  const s = status.data;
  const rt = s.runtime;

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>{s.name}</Title>
        <Badge color={s.status === "connected" ? "teal" : "red"} size="lg">
          {s.status}
        </Badge>
      </Group>
      {s.error && (
        <Text c="dimmed" size="sm">
          {s.error}
        </Text>
      )}
      <SimpleGrid cols={{ base: 1, md: 2 }}>
        <InfoCard title="containerd">
          <Row label="Version" value={rt?.containerd || "—"} />
          <Row
            label="Support"
            value={
              rt?.containerd ? (
                <Badge color={rt.containerdSupported ? "teal" : "yellow"} variant="light">
                  {rt.containerdSupported ? "supported" : "unsupported"}
                </Badge>
              ) : (
                "—"
              )
            }
          />
          <Row label="Default namespace" value={s.defaultNamespace} />
        </InfoCard>
        <InfoCard title="nerdctl">
          <Row label="Host binary" value={rt?.nerdctl.version || "not found"} />
          <Row label="Required" value={rt ? `${rt.nerdctl.required}.x` : "—"} />
          <Row
            label="Mode"
            value={
              rt && (rt.nerdctl.compatible || rt.nerdctl.checkSkipped) ? (
                <Badge color="teal" variant="light">
                  read-write
                </Badge>
              ) : (
                <Badge color="orange" variant="light">
                  read-only
                </Badge>
              )
            }
          />
          <Row label="Linked packages" value={rt?.nerdctlLibrary || "—"} />
        </InfoCard>
        <InfoCard title="nerdui">
          <Row label="Version" value={version.data?.nerdui.version ?? "—"} />
          <Row label="Commit" value={version.data?.nerdui.commit ?? "—"} />
          <Row label="Platform" value={version.data?.nerdui.platform ?? "—"} />
        </InfoCard>
      </SimpleGrid>
    </Stack>
  );
}

function InfoCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card withBorder>
      <Title order={4} mb="xs">
        {title}
      </Title>
      <Table>
        <Table.Tbody>{children}</Table.Tbody>
      </Table>
    </Card>
  );
}

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <Table.Tr>
      <Table.Th w="45%" fw={500} c="dimmed">
        {label}
      </Table.Th>
      <Table.Td>{value}</Table.Td>
    </Table.Tr>
  );
}
