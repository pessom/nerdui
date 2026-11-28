// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Badge, Box, Group, Paper, Text } from "@mantine/core";
import type { Network } from "../../api/queries";
import { RouterAnchor } from "../../components/RouterAnchor";
import { color, label, topologyContainers, type TopologyContainer } from "./topology";

// Fixed geometry: cards have known heights, so edge ends need no DOM measuring.
const NET_H = 56;
const CTR_HEAD = 30;
const CTR_LINE = 20;
const CTR_PAD = 8;
const GAP = 12;

const ctrHeight = (c: TopologyContainer) => CTR_HEAD + c.links.length * CTR_LINE + CTR_PAD;

/**
 * Network topology (#61): networks on the left, containers on the right, an edge per
 * attachment ending at the line with the container's address on that network.
 */
export function NetworkTopology({
  env,
  namespace,
  networks,
}: {
  env: string;
  namespace: string;
  networks: Network[];
}) {
  const containers = topologyContainers(networks);
  const netY = new Map<string, number>();
  networks.forEach((n, i) => netY.set(n.name, i * (NET_H + GAP) + NET_H / 2));
  const edges: { key: string; from: number; to: number; color: string; dashed: boolean }[] = [];
  let y = 0;
  for (const c of containers) {
    c.links.forEach((l, i) =>
      edges.push({
        key: `${c.id}/${l.network}`,
        from: netY.get(l.network) ?? 0,
        to: y + CTR_HEAD + i * CTR_LINE + CTR_LINE / 2,
        color: l.color,
        dashed: !c.running,
      }),
    );
    y += ctrHeight(c) + GAP;
  }
  const height = Math.max(networks.length * (NET_H + GAP), y) - GAP;

  if (networks.length === 0) {
    return (
      <Text c="dimmed" ta="center" py="md">
        No networks yet.
      </Text>
    );
  }

  return (
    <Box
      data-testid="network-topology"
      style={{
        display: "grid",
        gridTemplateColumns: "minmax(0, 1fr) clamp(24px, 12%, 140px) minmax(0, 1.4fr)",
        alignItems: "start",
      }}
    >
      <Box component="ul" aria-label="Networks" m={0} p={0} style={{ listStyle: "none" }}>
        {networks.map((n, i) => (
          <Paper
            component="li"
            key={n.name}
            withBorder
            px="xs"
            aria-label={`Network ${n.name}`}
            style={{
              height: NET_H,
              marginBottom: i < networks.length - 1 ? GAP : 0,
              borderLeft: `4px solid var(--mantine-color-${color(i)}-filled)`,
              display: "flex",
              flexDirection: "column",
              justifyContent: "center",
              overflow: "hidden",
            }}
          >
            <Group gap={6} wrap="nowrap">
              <Text size="sm" fw={600} truncate title={n.name}>
                {n.name}
              </Text>
              {n.default && (
                <Badge size="xs" variant="light">
                  default
                </Badge>
              )}
            </Group>
            <Text size="xs" c="dimmed" ff="monospace" truncate>
              {n.ipam
                .map((r) => r.subnet)
                .filter(Boolean)
                .join(", ") ||
                n.driver ||
                "no subnet"}
              {n.containers.length === 0 && " · no containers"}
            </Text>
          </Paper>
        ))}
      </Box>
      <svg
        aria-hidden
        width="100%"
        height={height}
        viewBox={`0 0 100 ${height}`}
        preserveAspectRatio="none"
        style={{ display: "block", overflow: "visible" }}
      >
        {edges.map((e) => (
          <path
            key={e.key}
            data-edge={e.key}
            d={`M0 ${e.from} C50 ${e.from} 50 ${e.to} 100 ${e.to}`}
            fill="none"
            stroke={`var(--mantine-color-${e.color}-filled)`}
            strokeWidth={2}
            strokeDasharray={e.dashed ? "4 4" : undefined}
            strokeOpacity={e.dashed ? 0.6 : 0.9}
            vectorEffect="non-scaling-stroke"
          />
        ))}
      </svg>
      <Box component="ul" aria-label="Containers" m={0} p={0} style={{ listStyle: "none" }}>
        {containers.length === 0 && (
          <Text size="sm" c="dimmed" pl="xs">
            No containers attached.
          </Text>
        )}
        {containers.map((c, i) => (
          <Paper
            component="li"
            key={c.id}
            withBorder
            px="xs"
            aria-label={`Container ${label(c)}`}
            style={{
              height: ctrHeight(c),
              marginBottom: i < containers.length - 1 ? GAP : 0,
              overflow: "hidden",
              opacity: c.running ? 1 : 0.75,
            }}
          >
            <Group gap={6} wrap="nowrap" style={{ height: CTR_HEAD }}>
              <RouterAnchor
                to="/env/$env/containers/$id"
                params={{ env, id: c.id }}
                search={{ ns: namespace }}
                size="sm"
                fw={600}
                truncate
                title={c.id}
              >
                {label(c)}
              </RouterAnchor>
              {!c.running && (
                <Badge size="xs" variant="outline" color="gray">
                  stopped
                </Badge>
              )}
            </Group>
            {c.links.map((l) => (
              <Group key={l.network} gap={6} wrap="nowrap" style={{ height: CTR_LINE }}>
                <Box
                  style={{
                    width: 8,
                    height: 8,
                    flex: "none",
                    borderRadius: "50%",
                    background: `var(--mantine-color-${l.color}-filled)`,
                  }}
                />
                <Text size="xs" c="dimmed" truncate style={{ flex: "0 1 auto" }}>
                  {l.network}
                </Text>
                <Text size="xs" ff="monospace" truncate>
                  {l.ips.length > 0 ? l.ips.join(", ") : c.running ? "no address" : "—"}
                </Text>
              </Group>
            ))}
          </Paper>
        ))}
      </Box>
    </Box>
  );
}
