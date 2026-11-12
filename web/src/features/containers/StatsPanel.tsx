// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Group,
  Loader,
  Paper,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Text,
  useComputedColorScheme,
} from "@mantine/core";
import { Suspense, lazy, useMemo, useState } from "react";
import { formatBytes } from "../system/info";
import { useContainerStats, type StatsPoint } from "./stats";

const StatsChart = lazy(() => import("./StatsChart"));

const percent = (v: number) => `${v.toFixed(1)} %`;
const bytes = (v: number) => formatBytes(Math.max(0, Math.round(v)));
const perSecond = (v: number) => `${bytes(v)}/s`;

/** Stats tab of the container page (#16): live numbers and charts of the last minutes. */
export function StatsPanel({
  env,
  id,
  namespace,
  running,
}: {
  env: string;
  id: string;
  namespace: string;
  running: boolean;
}) {
  const [interval, setInterval] = useState("2");
  const dark = useComputedColorScheme("light") === "dark";
  const { points, latest, status, error } = useContainerStats(
    env,
    id,
    namespace,
    Number(interval),
    running,
  );
  const last = points.at(-1);

  if (!running) {
    return (
      <Alert color="gray" title="Container is not running">
        Stats are available while the container runs.
      </Alert>
    );
  }
  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <Group gap="xs">
          <SegmentedControl
            size="xs"
            aria-label="Sampling interval"
            value={interval}
            onChange={setInterval}
            data={[
              { value: "1", label: "1 s" },
              { value: "2", label: "2 s" },
              { value: "5", label: "5 s" },
            ]}
          />
          <Text size="xs" c="dimmed">
            last 5 minutes, kept in this browser
          </Text>
        </Group>
        <Badge
          variant="light"
          color={status === "live" ? "teal" : status === "error" ? "red" : "gray"}
          leftSection={status === "connecting" ? <Loader size={8} /> : undefined}
        >
          {status}
        </Badge>
      </Group>
      {error && (
        <Alert color="red" title="Cannot read stats">
          {error.message}
        </Alert>
      )}
      <SimpleGrid cols={{ base: 2, sm: 5 }} spacing="xs">
        <Figure label="CPU" value={latest ? percent(latest.cpuPercent) : "—"} />
        <Figure
          label="Memory"
          value={latest ? `${bytes(latest.memoryUsage)} / ${bytes(latest.memoryLimit)}` : "—"}
          hint={latest ? percent(latest.memoryPercent) : undefined}
        />
        <Figure
          label="Network"
          value={last ? `↓ ${perSecond(last.rxRate)}  ↑ ${perSecond(last.txRate)}` : "—"}
          hint={
            latest ? `${bytes(latest.networkRx)} / ${bytes(latest.networkTx)} total` : undefined
          }
        />
        <Figure
          label="Block I/O"
          value={last ? `R ${perSecond(last.readRate)}  W ${perSecond(last.writeRate)}` : "—"}
          hint={
            latest ? `${bytes(latest.blockRead)} / ${bytes(latest.blockWrite)} total` : undefined
          }
        />
        <Figure label="PIDs" value={latest ? String(latest.pids) : "—"} />
      </SimpleGrid>
      <Charts points={points} dark={dark} />
    </Stack>
  );
}

function Figure({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <Paper withBorder p="xs" component="section" aria-label={label}>
      <Text size="xs" c="dimmed">
        {label}
      </Text>
      <Text size="sm" fw={600} style={{ whiteSpace: "pre" }}>
        {value}
      </Text>
      {hint && (
        <Text size="xs" c="dimmed">
          {hint}
        </Text>
      )}
    </Paper>
  );
}

function Charts({ points, dark }: { points: StatsPoint[]; dark: boolean }) {
  const data = useMemo(() => {
    const col = (f: (p: StatsPoint) => number) => points.map(f);
    return {
      t: col((p) => p.t),
      cpu: col((p) => p.cpu),
      memory: col((p) => p.memory),
      limit: col((p) => p.memoryLimit),
      rx: col((p) => p.rxRate),
      tx: col((p) => p.txRate),
      read: col((p) => p.readRate),
      write: col((p) => p.writeRate),
    };
  }, [points]);
  return (
    <Suspense fallback={<Loader aria-label="Loading charts" />}>
      <SimpleGrid cols={{ base: 1, md: 2 }} spacing="sm">
        <Paper withBorder p="xs">
          <Text size="sm" fw={500}>
            CPU
          </Text>
          <StatsChart
            label="CPU chart"
            dark={dark}
            min={0}
            times={data.t}
            format={percent}
            series={[{ label: "CPU", values: data.cpu, color: "#228be6" }]}
          />
        </Paper>
        <Paper withBorder p="xs">
          <Text size="sm" fw={500}>
            Memory
          </Text>
          <StatsChart
            label="Memory chart"
            dark={dark}
            min={0}
            times={data.t}
            format={bytes}
            series={[
              { label: "Used", values: data.memory, color: "#7950f2" },
              { label: "Limit", values: data.limit, color: "#adb5bd", dash: true },
            ]}
          />
        </Paper>
        <Paper withBorder p="xs">
          <Text size="sm" fw={500}>
            Network
          </Text>
          <StatsChart
            label="Network chart"
            dark={dark}
            min={0}
            times={data.t}
            format={perSecond}
            series={[
              { label: "Received", values: data.rx, color: "#12b886" },
              { label: "Sent", values: data.tx, color: "#fd7e14" },
            ]}
          />
        </Paper>
        <Paper withBorder p="xs">
          <Text size="sm" fw={500}>
            Block I/O
          </Text>
          <StatsChart
            label="Block I/O chart"
            dark={dark}
            min={0}
            times={data.t}
            format={perSecond}
            series={[
              { label: "Read", values: data.read, color: "#15aabf" },
              { label: "Write", values: data.write, color: "#e64980" },
            ]}
          />
        </Paper>
      </SimpleGrid>
    </Suspense>
  );
}
