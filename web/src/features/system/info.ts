// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { SystemInfo } from "../../api/queries";

export type Plugin = SystemInfo["plugins"][number];
export type PluginStatus = Plugin["status"];

const units = ["B", "KiB", "MiB", "GiB", "TiB"];

/** Binary size: 8589934592 → "8.0 GiB". */
export function formatBytes(n: number): string {
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return i === 0 ? `${v} B` : `${v.toFixed(1)} ${units[i]}`;
}

/** "v2 (systemd)"; cgroupVersion is empty on hosts without cgroups. */
export function cgroupLabel(info: Pick<SystemInfo, "cgroupVersion" | "cgroupDriver">): string {
  const v = info.cgroupVersion ? `v${info.cgroupVersion}` : "—";
  return info.cgroupDriver ? `${v} (${info.cgroupDriver})` : v;
}

export const featureLabels: Record<keyof SystemInfo["features"], string> = {
  memoryLimit: "Memory limit",
  swapLimit: "Swap limit",
  cpuCfsPeriod: "CPU CFS period",
  cpuCfsQuota: "CPU CFS quota",
  cpuShares: "CPU shares",
  cpuSet: "cpuset",
  pidsLimit: "PIDs limit",
  oomKillDisable: "OOM kill disable",
  ipv4Forwarding: "IPv4 forwarding",
};

/** Plugins matching a text filter (type or ID) and a status ("all" = any). */
export function filterPlugins(
  plugins: readonly Plugin[],
  text: string,
  status: PluginStatus | "all",
): Plugin[] {
  const needle = text.trim().toLowerCase();
  return plugins.filter(
    (p) =>
      (status === "all" || p.status === status) &&
      (!needle || p.type.toLowerCase().includes(needle) || p.id.toLowerCase().includes(needle)),
  );
}

export function countByStatus(plugins: readonly Plugin[]): Record<PluginStatus, number> {
  const c: Record<PluginStatus, number> = { ok: 0, skip: 0, error: 0 };
  for (const p of plugins) c[p.status]++;
  return c;
}
