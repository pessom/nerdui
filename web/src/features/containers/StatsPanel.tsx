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
import { useQuery } from "@tanstack/react-query";
import { Suspense, lazy, useMemo, useState, type ReactNode } from "react";
import { envStatusQuery, statsHistoryQuery } from "../../api/queries";
import { formatBytes } from "../system/info";
import {
  HISTORY_RANGES,
  historyAxis,
  liveAxis,
  useContainerStats,
  type HistoryRange,
  type StatsPoint,
} from "./stats";

const StatsChart = lazy(() => import("./StatsChart"));

const percent = (v: number) => `${v.toFixed(1)} %`;
const bytes = (v: number) => formatBytes(Math.max(0, Math.round(v)));
const perSecond = (v: number) => `${bytes(v)}/s`;

/** Stats tab of the container page (#16): live numbers and charts of the last minutes, and
 * the metrics history from Prometheus when the environment has it (#176). */
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
  const status = useQuery(envStatusQuery(env));
  const withHistory = status.data?.features.metricsHistory ?? false;
  const [period, setPeriod] = useState<Period>(running ? "live" : "1h");
  // Live stats need a running container; the history does not.
  const shown: Period = !withHistory ? "live" : !running && period === "live" ? "1h" : period;
  return (
    <Stack gap="sm">
      {withHistory && (
        <SegmentedControl
          size="xs"
          aria-label="Period"
          style={{ alignSelf: "flex-start" }}
          value={shown}
          onChange={(v) => setPeriod(v as Period)}
          data={[
            { value: "live", label: "5m (live)", disabled: !running },
            ...HISTORY_RANGES.map((r) => ({ value: r, label: r })),
          ]}
        />
      )}
      {shown !== "live" ? (
        <HistoryStats env={env} id={id} namespace={namespace} range={shown} />
      ) : running ? (
        <LiveStats env={env} id={id} namespace={namespace} />
      ) : (
        <Alert color="gray" title="Container is not running">
          Stats are available while the container runs.
        </Alert>
      )}
    </Stack>
  );
}

type Period = "live" | HistoryRange;

function LiveStats({ env, id, namespace }: { env: string; id: string; namespace: string }) {
  const [interval, setInterval] = useState("2");
  const dark = useComputedColorScheme("light") === "dark";
  const { points, latest, status, error } = useContainerStats(
    env,
    id,
    namespace,
    Number(interval),
    true,
  );
  const last = points.at(-1);
  return (
    <>
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
    </>
  );
}

/** Metrics history of a period from Prometheus (#176); containerd has no network metrics. */
function HistoryStats({
  env,
  id,
  namespace,
  range,
}: {
  env: string;
  id: string;
  namespace: string;
  range: HistoryRange;
}) {
  const dark = useComputedColorScheme("light") === "dark";
  const history = useQuery(statsHistoryQuery(env, namespace, id, range));
  const axis = useMemo(() => historyAxis(range), [range]);
  if (history.isPending) return <Loader aria-label="Loading history" />;
  if (history.isError)
    return (
      <Alert color="red" title="Cannot read the metrics history">
        {history.error.message}
      </Alert>
    );
  const h = history.data;
  const empty = [h.cpu, h.memory, h.pids].every((col) => col.every((v) => v == null));
  return (
    <>
      <Text size="xs" c="dimmed">
        From Prometheus (containerd metrics), one point per {h.step} s. Network traffic is not part
        of containerd&apos;s metrics.
      </Text>
      {empty && (
        <Alert color="yellow" title="No data for this container">
          Prometheus has no containerd metrics of this container in the last {range}. Check that
          containerd&apos;s metrics are enabled and scraped (see the deployment guide).
        </Alert>
      )}
      <Suspense fallback={<Loader aria-label="Loading charts" />}>
        <SimpleGrid cols={{ base: 1, md: 2 }} spacing="sm">
          <ChartCard title="CPU">
            <StatsChart
              label="CPU chart"
              dark={dark}
              min={0}
              axis={axis}
              times={h.times}
              format={percent}
              series={[{ label: "CPU", values: h.cpu, color: "#228be6" }]}
            />
          </ChartCard>
          <ChartCard title="Memory">
            <StatsChart
              label="Memory chart"
              dark={dark}
              min={0}
              axis={axis}
              times={h.times}
              format={bytes}
              series={[
                { label: "Used", values: h.memory, color: "#7950f2" },
                { label: "Limit", values: h.memoryLimit, color: "#adb5bd", dash: true },
              ]}
            />
          </ChartCard>
          <ChartCard title="Block I/O">
            <StatsChart
              label="Block I/O chart"
              dark={dark}
              min={0}
              axis={axis}
              times={h.times}
              format={perSecond}
              series={[
                { label: "Read", values: h.blockRead, color: "#15aabf" },
                { label: "Write", values: h.blockWrite, color: "#e64980" },
              ]}
            />
          </ChartCard>
          <ChartCard title="PIDs">
            <StatsChart
              label="PIDs chart"
              dark={dark}
              min={0}
              axis={axis}
              times={h.times}
              format={(v) => String(Math.round(v))}
              series={[{ label: "PIDs", values: h.pids, color: "#40c057" }]}
            />
          </ChartCard>
        </SimpleGrid>
      </Suspense>
    </>
  );
}

function ChartCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Paper withBorder p="xs">
      <Text size="sm" fw={500}>
        {title}
      </Text>
      {children}
    </Paper>
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
            axis={liveAxis}
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
            axis={liveAxis}
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
            axis={liveAxis}
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
            axis={liveAxis}
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
