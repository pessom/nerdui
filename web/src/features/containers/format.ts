// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { components } from "../../api/client";
import { shortDuration, timeAgo } from "../../lib/time";

export type Container = components["schemas"]["Container"];
export type Port = components["schemas"]["Port"];

/** Display name: nerdctl name, or the short ID for containers created by other tools. */
export function displayName(c: Container): string {
  return c.name || c.id.slice(0, 12);
}

/** Status text in the spirit of `nerdctl ps`, e.g. "Up 3h", "Exited (1) 2 hours ago". */
export function statusText(c: Container, now: number = Date.now()): string {
  switch (c.state) {
    case "running":
      return c.startedAt ? `Up ${shortDuration(now - Date.parse(c.startedAt))}` : "Up";
    case "paused":
      return "Paused";
    case "restarting":
      return `Restarting (${c.exitCode ?? "?"})`;
    case "exited":
      return `Exited (${c.exitCode ?? "?"})${c.exitedAt ? ` ${timeAgo(c.exitedAt, now)}` : ""}`;
    case "created":
      return "Created";
    default:
      return "Unknown";
  }
}

export const stateColor: Record<Container["state"], string> = {
  running: "teal",
  paused: "yellow",
  restarting: "orange",
  exited: "gray",
  created: "blue",
  unknown: "red",
};

/** "0.0.0.0:8080→80/tcp" style label. */
export function portLabel(p: Port): string {
  const host = p.hostIp && p.hostIp !== "0.0.0.0" && p.hostIp !== "::" ? `${p.hostIp}:` : "";
  return `${host}${p.hostPort}→${p.containerPort}/${p.protocol}`;
}

/**
 * URL to open a published TCP port from the browser: ports bound to all interfaces are
 * reachable on the host the UI is served from; loopback-only ports are not linked.
 */
export function portURL(p: Port, pageHost: string): string | undefined {
  if (p.protocol !== "tcp") return undefined;
  const any = !p.hostIp || p.hostIp === "0.0.0.0" || p.hostIp === "::";
  if (!any) return undefined;
  const host = pageHost.includes(":") && !pageHost.startsWith("[") ? `[${pageHost}]` : pageHost;
  return `http://${host}:${p.hostPort}`;
}

/** Text used by the search box. */
export function searchText(c: Container): string {
  return [
    c.name,
    c.id,
    c.image,
    c.compose?.project,
    c.compose?.service,
    c.command,
    c.state,
    c.namespace,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

export interface Group {
  project?: string;
  /** The namespace of the project, when containers of several namespaces are listed. */
  namespace?: string;
  containers: Container[];
}

/**
 * Groups containers by compose project (alphabetically), standalone containers last. With
 * byNamespace, projects of the same name in different namespaces are separate groups (#276).
 */
export function groupByProject(list: Container[], byNamespace = false): Group[] {
  const byProject = new Map<string, Group>();
  const standalone: Container[] = [];
  for (const c of list) {
    const project = c.compose?.project;
    if (!project) {
      standalone.push(c);
      continue;
    }
    const key = byNamespace ? `${project}\u0000${c.namespace}` : project;
    const group = byProject.get(key) ?? {
      project,
      ...(byNamespace ? { namespace: c.namespace } : {}),
      containers: [],
    };
    group.containers.push(c);
    byProject.set(key, group);
  }
  const groups = [...byProject.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([, group]) => group);
  if (standalone.length) groups.push({ containers: standalone });
  return groups;
}
