// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { ContainerInspect } from "../../api/queries";

export type { ContainerInspect };

/** "KEY=value" entries as [key, value]; an entry without "=" has an empty value. */
export function envPairs(env: readonly string[] | undefined): [string, string][] {
  return (env ?? []).map((e) => {
    const i = e.indexOf("=");
    return i < 0 ? [e, ""] : [e.slice(0, i), e.slice(i + 1)];
  });
}

/** The command line the container runs: Path followed by Args. */
export function commandLine(c: ContainerInspect): string {
  return [c.Path, ...(c.Args ?? [])]
    .filter((s): s is string => !!s)
    .map(quoteArg)
    .join(" ");
}

function quoteArg(a: string): string {
  return /^[\w@%+=:,./-]+$/.test(a) ? a : `'${a.replaceAll("'", `'\\''`)}'`;
}

/** Restart policy as `nerdctl run --restart` accepts it, e.g. "on-failure:3". */
export function restartPolicy(c: ContainerInspect): string {
  const p = c.HostConfig?.RestartPolicy;
  if (!p?.Name) return "no";
  return p.Name === "on-failure" && p.MaximumRetryCount
    ? `${p.Name}:${p.MaximumRetryCount}`
    : p.Name;
}

export interface PortBinding {
  container: string;
  host: string;
}

/** Published ports as "0.0.0.0:8080" → "80/tcp"; exposed ports without bindings are skipped. */
export function portBindings(c: ContainerInspect): PortBinding[] {
  const out: PortBinding[] = [];
  for (const [container, bindings] of Object.entries(c.NetworkSettings?.Ports ?? {})) {
    for (const b of bindings ?? []) {
      const ip = b.HostIp && b.HostIp.includes(":") ? `[${b.HostIp}]` : (b.HostIp ?? "");
      out.push({ container, host: ip ? `${ip}:${b.HostPort ?? ""}` : (b.HostPort ?? "") });
    }
  }
  return out.sort((a, b) => a.container.localeCompare(b.container, undefined, { numeric: true }));
}

/** Pretty JSON for the viewer and the copy button (same indentation as `nerdctl inspect`). */
export function inspectJSON(c: ContainerInspect): string {
  return JSON.stringify(c, null, 4);
}
