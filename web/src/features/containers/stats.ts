// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import type { components } from "../../api/client";
import { openStream, type StreamError } from "../../lib/ws";

export type StatsSample = components["schemas"]["StatsSample"];

/** History kept in the browser (#16). */
export const HISTORY_SECONDS = 5 * 60;

/**
 * The x range of the charts: always HISTORY_SECONDS wide and ending at the latest sample (or
 * now), so the scale does not stretch while the history fills up.
 */
export function timeWindow(latest: number | null | undefined, now = Date.now() / 1000) {
  const hi = latest != null && Number.isFinite(latest) ? latest : now;
  return [hi - HISTORY_SECONDS, hi] as [number, number];
}

const pad = (n: number) => String(n).padStart(2, "0");

/** Local 24-hour clock for unix seconds: HH:MM, or HH:MM:SS with seconds. */
export function clock(t: number, seconds = false): string {
  const d = new Date(t * 1000);
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return seconds ? `${hm}:${pad(d.getSeconds())}` : hm;
}

/** One point of the charts: rates are derived from consecutive cumulative counters. */
export interface StatsPoint {
  /** Unix seconds. */
  t: number;
  cpu: number;
  memory: number;
  memoryLimit: number;
  rxRate: number;
  txRate: number;
  readRate: number;
  writeRate: number;
  pids: number;
}

/** Bytes per second between two samples; counters that went backwards (restart) give 0. */
export function rate(prev: number, next: number, seconds: number): number {
  if (seconds <= 0 || next < prev) return 0;
  return (next - prev) / seconds;
}

/** Appends a sample to the points, keeping HISTORY_SECONDS. */
export function addSample(
  points: StatsPoint[],
  prev: StatsSample | undefined,
  s: StatsSample,
): StatsPoint[] {
  const t = Date.parse(s.ts) / 1000;
  const dt = prev ? t - Date.parse(prev.ts) / 1000 : 0;
  const point: StatsPoint = {
    t,
    cpu: s.cpuPercent,
    memory: s.memoryUsage,
    memoryLimit: s.memoryLimit,
    rxRate: prev ? rate(prev.networkRx, s.networkRx, dt) : 0,
    txRate: prev ? rate(prev.networkTx, s.networkTx, dt) : 0,
    readRate: prev ? rate(prev.blockRead, s.blockRead, dt) : 0,
    writeRate: prev ? rate(prev.blockWrite, s.blockWrite, dt) : 0,
    pids: s.pids,
  };
  const next = points.concat(point);
  const from = t - HISTORY_SECONDS;
  const first = next.findIndex((p) => p.t >= from);
  return first > 0 ? next.slice(first) : next;
}

export type StatsStatus = "connecting" | "live" | "stopped" | "error";

/**
 * Streams stats of a running container. The history lives in the browser and survives
 * interval changes; it restarts when the container runs again.
 */
export function useContainerStats(
  env: string,
  id: string,
  namespace: string,
  interval: number,
  running: boolean,
) {
  const [points, setPoints] = useState<StatsPoint[]>([]);
  const [latest, setLatest] = useState<StatsSample>();
  const [status, setStatus] = useState<StatsStatus>(running ? "connecting" : "stopped");
  const [error, setError] = useState<StreamError>();

  useEffect(() => {
    if (!running) return;
    let prev: StatsSample | undefined;
    const q = new URLSearchParams({ namespace, interval: `${interval}s` });
    const stream = openStream<StatsSample>(
      `/api/v1/environments/${encodeURIComponent(env)}/containers/${encodeURIComponent(id)}/stats/stream?${q.toString()}`,
      {
        onData: (s) => {
          const before = prev;
          prev = s;
          setLatest(s);
          setPoints((p) => addSample(p, before, s));
          setStatus("live");
          setError(undefined);
        },
        onError: (e) => {
          setError(e);
          setStatus("error");
        },
        onEnd: () => setStatus((s) => (s === "error" ? s : "stopped")),
      },
    );
    return () => stream.close();
  }, [env, id, namespace, interval, running]);

  return { points, latest, status: running ? status : "stopped", error };
}
