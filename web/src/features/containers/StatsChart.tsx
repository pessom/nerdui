// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useRef } from "react";
import uPlot, { type AlignedData, type Options } from "uplot";
import "uplot/dist/uPlot.min.css";
import { timeWindow, type TimeAxis } from "./stats";

export interface ChartSeries {
  label: string;
  /** null is a gap. */
  values: (number | null)[];
  color: string;
  dash?: boolean;
}

/**
 * Small live line chart (uPlot, #16). Loaded lazily with the Stats tab; it is recreated only
 * when the shape changes, data updates call setData.
 */
export default function StatsChart({
  label,
  times,
  series,
  format,
  dark,
  min,
  axis,
}: {
  label: string;
  times: number[];
  series: ChartSeries[];
  format: (v: number) => string;
  dark: boolean;
  /** Fixed lower bound of the y axis (0 for most charts). */
  min?: number;
  axis: TimeAxis;
}) {
  const host = useRef<HTMLDivElement>(null);
  const plot = useRef<uPlot>(null);
  const shape = series.map((s) => `${s.label}:${s.color}:${s.dash ? 1 : 0}`).join("|");
  const formatRef = useRef(format);
  useEffect(() => {
    formatRef.current = format;
  }, [format]);

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const axisColor = dark ? "#909296" : "#868e96";
    const grid = dark ? "#373a40" : "#e9ecef";
    const opts: Options = {
      width: el.clientWidth || 300,
      height: 160,
      legend: { show: true },
      cursor: { drag: { x: false, y: false } },
      scales: {
        // A fixed window ending at the latest point.
        x: { time: true, range: (_u, _lo, hi) => timeWindow(hi, undefined, axis.seconds) },
        y: { range: (_u, lo, hi) => [min ?? lo, Math.max(hi, 1)] },
      },
      axes: [
        {
          stroke: axisColor,
          grid: { stroke: grid },
          ticks: { stroke: grid },
          incrs: axis.incrs,
          values: (_u, vals) => vals.map((v) => axis.tick(v)),
        },
        {
          stroke: axisColor,
          grid: { stroke: grid },
          ticks: { stroke: grid },
          size: 70,
          values: (_u, vals) => vals.map((v) => formatRef.current(v)),
        },
      ],
      series: [
        { value: (_u, v) => (v == null ? "—" : axis.legend(v)) },
        ...series.map((s) => ({
          label: s.label,
          stroke: s.color,
          width: 1.5,
          dash: s.dash ? [4, 4] : undefined,
          value: (_u: uPlot, v: number | null) => (v == null ? "—" : formatRef.current(v)),
        })),
      ],
    };
    const p = new uPlot(opts, [[], ...series.map(() => [])] as AlignedData, el);
    plot.current = p;
    const observer = new ResizeObserver(() => p.setSize({ width: el.clientWidth, height: 160 }));
    observer.observe(el);
    return () => {
      observer.disconnect();
      p.destroy();
      plot.current = null;
    };
    // Recreated when the shape, theme or axis changes; data goes through setData below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shape, dark, min, axis]);

  useEffect(() => {
    plot.current?.setData([times, ...series.map((s) => s.values)] as AlignedData);
  }, [times, series]);

  return <div ref={host} role="img" aria-label={label} style={{ width: "100%" }} />;
}
