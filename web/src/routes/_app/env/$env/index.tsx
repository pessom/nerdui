// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Button, Group, Loader, SimpleGrid, Stack, Text, Title } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconTrash } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { envStatusQuery, versionQuery } from "../../../../api/queries";
import { InfoCard, Row } from "../../../../components/InfoCard";
import { PruneDialog } from "../../../../features/prune/PruneDialog";
import { SystemInfoSection } from "../../../../features/system/SystemInfoSection";

/** Environment overview: connectivity, versions, host and runtime information (#66). */
export const Route = createFileRoute("/_app/env/$env/")({
  component: Overview,
});

function Overview() {
  const { env } = Route.useParams();
  const status = useQuery(envStatusQuery(env));
  const version = useQuery(versionQuery);
  const [pruning, { open, close }] = useDisclosure();

  if (status.isPending) return <Loader aria-label="Loading" />;
  if (status.isError) return <Text c="red">{status.error.message}</Text>;
  const s = status.data;
  const rt = s.runtime;

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>{s.name}</Title>
        <Group gap="xs">
          {s.status === "connected" && (
            <Button
              variant="default"
              leftSection={<IconTrash size={16} />}
              onClick={open}
              disabled={!!rt && !rt.nerdctl.compatible && !rt.nerdctl.checkSkipped}
            >
              Clean up…
            </Button>
          )}
          <Badge color={s.status === "connected" ? "teal" : "red"} size="lg">
            {s.status}
          </Badge>
        </Group>
      </Group>
      <PruneDialog
        env={env}
        defaultNamespace={s.defaultNamespace}
        opened={pruning}
        onClose={close}
      />
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
          <Row
            label="Supported by this build"
            value={rt?.supportedContainerd.length ? rt.supportedContainerd.join(", ") : "—"}
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
      {s.status === "connected" && <SystemInfoSection env={env} />}
    </Stack>
  );
}
