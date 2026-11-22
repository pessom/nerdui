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

/** The healthcheck command as `--health-cmd` shows it; undefined when checks are disabled. */
export function healthTest(test: readonly string[] | null | undefined): string | undefined {
  if (!test?.length || test[0] === "NONE") return undefined;
  const [kind, ...rest] = test;
  if (kind === "CMD-SHELL") return rest.join(" ");
  if (kind === "CMD") return rest.map(quoteArg).join(" ");
  return test.map(quoteArg).join(" ");
}

/** A Go duration in nanoseconds as "1m30s", "500ms"; undefined for 0 (the default). */
export function goDuration(ns: number | undefined): string | undefined {
  if (!ns) return undefined;
  if (ns < 1e9) return `${Math.round(ns / 1e6)}ms`;
  let s = Math.round(ns / 1e9);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  s %= 60;
  return [h && `${h}h`, m && `${m}m`, s && `${s}s`].filter(Boolean).join("");
}
