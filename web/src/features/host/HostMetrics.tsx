// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Group,
  Loader,
  Paper,
  Progress,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Text,
  Title,
  useComputedColorScheme,
} from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { Suspense, lazy, useMemo, useState, type ReactNode } from "react";
import { api, unwrap, type components } from "../../api/client";
import { historyAxis } from "../containers/stats";
import { formatBytes } from "../system/info";

const StatsChart = lazy(() => import("../containers/StatsChart"));

type Range = "1h" | "6h" | "24h";
type Sample = components["schemas"]["HostSample"];
const ranges: Range[] = ["1h", "6h", "24h"];

const percent = (v: number) => `${v.toFixed(1)} %`;
const perSecond = (v: number) => `${formatBytes(Math.max(0, Math.round(v)))}/s`;
const diskColors = ["#fd7e14", "#12b886", "#be4bdb", "#fab005"];

/** Usage color: green, then yellow from 75 %, red from 90 %. */
const usageColor = (p: number) => (p >= 90 ? "red" : p >= 75 ? "yellow" : "teal");

function hostMetricsQuery(env: string, range: Range) {
  return {
    queryKey: ["host-metrics", env, range] as const,
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/host/metrics", {
          params: { path: { env }, query: { range } },
        }),
      ),
    refetchInterval: 15_000,
  };
}

type Metrics = Awaited<ReturnType<ReturnType<typeof hostMetricsQuery>["queryFn"]>>;

/** The chart columns of a history. */
function toColumns(data: Metrics | undefined) {
  const pts = data?.points ?? [];
  const disks = data?.current.disks.map((d) => d.path) ?? [];
  return {
    times: pts.map((p) => p.t),
    cpu: pts.map((p) => p.cpu),
    memory: pts.map((p) => p.memory),
    rx: pts.map((p) => p.netRx),
    tx: pts.map((p) => p.netTx),
    disks: disks.map((path, i) => ({
      label: path,
      values: pts.map((p) => p.disks[path] ?? null),
      color: diskColors[i % diskColors.length],
    })),
  };
}

/** CPU, memory, disks and network of the host nerdui runs on (#64). */
export function HostMetrics({ env }: { env: string }) {
  const [range, setRange] = useState<Range>("1h");
  const metrics = useQuery(hostMetricsQuery(env, range));
  const dark = useComputedColorScheme("light") === "dark";
  const axis = useMemo(() => historyAxis(range), [range]);
  const data = metrics.data;
  const columns = useMemo(() => toColumns(data), [data]);

  return (
    <Paper withBorder p="md" component="section" aria-label="Host usage">
      <Stack gap="sm">
        <Group justify="space-between">
          <Title order={3}>Host usage</Title>
          <SegmentedControl
            size="xs"
            aria-label="Host metrics range"
            value={range}
            onChange={(v) => setRange(v as Range)}
            data={ranges}
          />
        </Group>
        {metrics.isPending && <Loader aria-label="Loading host metrics" />}
        {metrics.isError && (
          <Alert color="yellow" title="No host metrics">
            {metrics.error.message}
          </Alert>
        )}
        {data && (
          <>
            <Tiles sample={data.current} />
            <Suspense fallback={<Loader aria-label="Loading charts" />}>
              <SimpleGrid cols={{ base: 1, md: 3 }} spacing="sm">
                <Chart title="CPU and memory">
                  <StatsChart
                    label="CPU and memory chart"
                    dark={dark}
                    min={0}
                    axis={axis}
                    times={columns.times}
                    format={percent}
                    series={[
                      { label: "CPU", values: columns.cpu, color: "#228be6" },
                      { label: "Memory", values: columns.memory, color: "#7950f2" },
                    ]}
                  />
                </Chart>
                <Chart title="Disks">
                  <StatsChart
                    label="Disks chart"
                    dark={dark}
                    min={0}
                    axis={axis}
                    times={columns.times}
                    format={percent}
                    series={columns.disks}
                  />
                </Chart>
                <Chart title="Network">
                  <StatsChart
                    label="Network chart"
                    dark={dark}
                    min={0}
                    axis={axis}
                    times={columns.times}
                    format={perSecond}
                    series={[
                      { label: "Received", values: columns.rx, color: "#15aabf" },
                      { label: "Sent", values: columns.tx, color: "#e64980" },
                    ]}
                  />
                </Chart>
              </SimpleGrid>
            </Suspense>
            <Text size="xs" c="dimmed">
              Sampled every {data.intervalSeconds} s and kept in memory for a day; a restart of
              nerdui starts the history anew. Alert thresholds are in Settings.
            </Text>
          </>
        )}
      </Stack>
    </Paper>
  );
}

function Tiles({ sample: s }: { sample: Sample }) {
  const mem = s.memoryTotal ? (s.memoryUsed / s.memoryTotal) * 100 : 0;
  return (
    <SimpleGrid cols={{ base: 1, xs: 2, md: 4 }} spacing="sm">
      <Tile label="CPU" value={percent(s.cpu)} percent={s.cpu}>
        {s.cpus} CPUs · load {s.load1.toFixed(2)}
      </Tile>
      <Tile label="Memory" value={percent(mem)} percent={mem}>
        {formatBytes(s.memoryUsed)} of {formatBytes(s.memoryTotal)}
        {s.swapTotal > 0 && ` · swap ${formatBytes(s.swapUsed)}`}
      </Tile>
      {s.disks.map((d) => {
        const p = d.total ? (d.used / d.total) * 100 : 0;
        return (
          <Tile key={d.path} label={`Disk ${d.path}`} value={percent(p)} percent={p}>
            <span title={d.paths.join(", ")}>
              {formatBytes(d.used)} of {formatBytes(d.total)}
            </span>
          </Tile>
        );
      })}
      <Tile label="Network" value={`↓ ${perSecond(s.netRx)}`}>
        ↑ {perSecond(s.netTx)}
      </Tile>
    </SimpleGrid>
  );
}

function Tile({
  label,
  value,
  percent: p,
  children,
}: {
  label: string;
  value: string;
  percent?: number;
  children: ReactNode;
}) {
  return (
    <Paper withBorder p="sm" aria-label={label}>
      <Text size="xs" c="dimmed">
        {label}
      </Text>
      <Text fw={600} size="lg">
        {value}
      </Text>
      {p !== undefined && (
        <Progress value={p} color={usageColor(p)} size="sm" my={4} aria-label={`${label} usage`} />
      )}
      <Text size="xs" c="dimmed">
        {children}
      </Text>
    </Paper>
  );
}

function Chart({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Paper withBorder p="xs">
      <Text size="sm" fw={500} mb={4}>
        {title}
      </Text>
      {children}
    </Paper>
  );
}
