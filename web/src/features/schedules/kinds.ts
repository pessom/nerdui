// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import cronstrue from "cronstrue";
import type { components } from "../../api/schema";

export type Schedule = components["schemas"]["Schedule"];
export type ScheduleInput = components["schemas"]["ScheduleInput"];
export type ScheduleKind = components["schemas"]["ScheduleKind"];
export type ScheduleRun = components["schemas"]["ScheduleRun"];

export const kindLabels: Record<ScheduleKind, string> = {
  prune: "Prune",
  backup: "Backup of nerdui",
  "stack.redeploy": "Redeploy stack (pull images)",
  "stack.restart": "Restart stack",
  "container.redeploy": "Redeploy container (pull image)",
  "container.restart": "Restart container",
  "image-updates.check": "Image update check",
  "volume.backup": "Backup of a volume",
};

/** Kinds that run on a stack or container named in target. */
export const targetKinds: ScheduleKind[] = [
  "stack.redeploy",
  "stack.restart",
  "container.redeploy",
  "container.restart",
];

/** Kinds that run in an environment. */
export function needsEnvironment(kind: ScheduleKind): boolean {
  return kind !== "backup";
}

/** Kinds that run in a namespace. */
export function needsNamespace(kind: ScheduleKind): boolean {
  return kind === "prune" || kind === "volume.backup" || targetKinds.includes(kind);
}

const presets: Record<string, string> = {
  "@yearly": "0 0 1 1 *",
  "@annually": "0 0 1 1 *",
  "@monthly": "0 0 1 * *",
  "@weekly": "0 0 * * 0",
  "@daily": "0 0 * * *",
  "@midnight": "0 0 * * *",
  "@hourly": "0 * * * *",
};

/** A cron expression in words, e.g. "At 03:00"; undefined when it cannot be read. */
export function describeCron(expr: string): string | undefined {
  const e = expr.trim();
  const every = /^@every\s+(\S+)$/.exec(e);
  if (every) return `Every ${every[1]}`;
  try {
    return cronstrue.toString(presets[e] ?? e, { use24HourTimeFormat: true, verbose: false });
  } catch {
    return undefined;
  }
}

/** The task of a schedule in a few words. */
export function describeTask(s: Schedule): string {
  const where = [s.environment, s.namespace].filter(Boolean).join(" / ");
  switch (s.kind) {
    case "prune": {
      const what = [
        s.options.containers && "stopped containers",
        s.options.images && (s.options.allImages ? "unused images" : "dangling images"),
        s.options.networks && "unused networks",
        s.options.buildCache && "build cache",
      ].filter(Boolean);
      return `${what.join(", ")} in ${where}`;
    }
    case "backup":
      return `to ${s.options.directory ?? ""}, keep ${s.options.keep ?? 0}`;
    case "image-updates.check":
      return s.environment ?? "";
    case "volume.backup":
      return `${s.target ?? ""} in ${where}, keep ${s.options.keep ?? 0}${s.options.stopContainers ? ", containers stopped" : ""}`;
    default:
      return `${s.target ?? ""} in ${where}`;
  }
}

/** Seconds as "5 minutes", "6 hours", "1 day". */
export function formatSeconds(sec: number): string {
  const units: [number, string][] = [
    [86400, "day"],
    [3600, "hour"],
    [60, "minute"],
  ];
  for (const [n, name] of units) {
    if (sec >= n && sec % n === 0) {
      const v = sec / n;
      return `${v} ${name}${v === 1 ? "" : "s"}`;
    }
  }
  return `${sec} seconds`;
}

export const schedulesKey = ["schedules"] as const;

export const statusColor: Record<string, string> = {
  running: "blue",
  succeeded: "teal",
  failed: "red",
  skipped: "gray",
};
