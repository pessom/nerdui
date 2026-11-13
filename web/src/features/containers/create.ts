// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { components } from "../../api/client";

export type CreateRequest = components["schemas"]["CreateContainerRequest"];

/**
 * Splits a command line into arguments like a shell does for quoting only: "…" and '…'
 * group words, backslash escapes inside "…" and outside quotes. No variables or globs.
 * Returns an error for an unterminated quote.
 */
export function parseArgs(line: string): { args: string[]; error?: string } {
  const args: string[] = [];
  let cur = "";
  let inWord = false;
  let quote: '"' | "'" | null = null;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]!;
    if (quote === "'") {
      if (ch === "'") quote = null;
      else cur += ch;
      continue;
    }
    if (ch === "\\" && i + 1 < line.length && quote !== null) {
      const next = line[i + 1]!;
      if (next === '"' || next === "\\") {
        cur += next;
        i++;
        continue;
      }
    }
    if (ch === "\\" && quote === null && i + 1 < line.length) {
      cur += line[++i];
      inWord = true;
      continue;
    }
    if (quote === '"') {
      if (ch === '"') quote = null;
      else cur += ch;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      inWord = true;
    } else if (/\s/.test(ch)) {
      if (inWord) args.push(cur);
      cur = "";
      inWord = false;
    } else {
      cur += ch;
      inWord = true;
    }
  }
  if (quote) return { args, error: `unterminated ${quote} quote` };
  if (inWord) args.push(cur);
  return { args };
}

export interface PortRow {
  hostIp: string;
  hostPort: string;
  containerPort: string;
  protocol: "tcp" | "udp";
}
export interface EnvRow {
  name: string;
  value: string;
  secret: boolean;
}
export interface MountRow {
  type: "volume" | "bind" | "tmpfs";
  source: string;
  target: string;
  readOnly: boolean;
}
export interface LabelRow {
  key: string;
  value: string;
}

export interface CreateForm {
  image: string;
  name: string;
  command: string;
  entrypoint: string;
  ports: PortRow[];
  env: EnvRow[];
  mounts: MountRow[];
  network: string;
  hostname: string;
  user: string;
  workdir: string;
  cpus: string;
  memoryMiB: string;
  restart: "no" | "always" | "unless-stopped" | "on-failure";
  retries: string;
  labels: LabelRow[];
  start: boolean;
}

export const emptyForm: CreateForm = {
  image: "",
  name: "",
  command: "",
  entrypoint: "",
  ports: [],
  env: [],
  mounts: [],
  network: "",
  hostname: "",
  user: "",
  workdir: "",
  cpus: "",
  memoryMiB: "",
  restart: "unless-stopped",
  retries: "",
  labels: [],
  start: true,
};

/** The API request for a form; rows left empty are skipped. The server validates. */
export function toRequest(f: CreateForm): { request: CreateRequest; error?: string } {
  const cmd = parseArgs(f.command);
  const request: CreateRequest = { image: f.image.trim(), start: f.start };
  const opt = <K extends keyof CreateRequest>(k: K, v: CreateRequest[K] | "" | undefined) => {
    if (v !== "" && v !== undefined) request[k] = v;
  };
  opt("name", f.name.trim());
  opt("entrypoint", f.entrypoint.trim());
  if (cmd.args.length) request.command = cmd.args;
  const ports = f.ports
    .filter((p) => p.containerPort.trim())
    .map((p) => ({
      ...(p.hostIp.trim() ? { hostIp: p.hostIp.trim() } : {}),
      ...(p.hostPort.trim() ? { hostPort: Number(p.hostPort) } : {}),
      containerPort: Number(p.containerPort),
      protocol: p.protocol,
    }));
  if (ports.length) request.ports = ports;
  const env = f.env
    .filter((e) => e.name.trim())
    .map((e) => ({ name: e.name.trim(), value: e.value }));
  if (env.length) request.env = env;
  const mounts = f.mounts
    .filter((m) => m.target.trim())
    .map((m) => ({
      type: m.type,
      ...(m.source.trim() ? { source: m.source.trim() } : {}),
      target: m.target.trim(),
      ...(m.readOnly ? { readOnly: true } : {}),
    }));
  if (mounts.length) request.mounts = mounts;
  opt("network", f.network.trim());
  opt("hostname", f.hostname.trim());
  opt("user", f.user.trim());
  opt("workdir", f.workdir.trim());
  if (f.cpus.trim()) request.cpus = Number(f.cpus);
  if (f.memoryMiB.trim()) request.memoryBytes = Math.round(Number(f.memoryMiB) * 1024 * 1024);
  request.restart =
    f.restart === "on-failure" && f.retries.trim() ? `on-failure:${f.retries.trim()}` : f.restart;
  const labels = Object.fromEntries(
    f.labels.filter((l) => l.key.trim()).map((l) => [l.key.trim(), l.value]),
  );
  if (Object.keys(labels).length) request.labels = labels;
  return { request, error: cmd.error ? `Command: ${cmd.error}` : undefined };
}

/** Hides the values of secret variables in the displayed command. */
export function secretsMasked(command: string, env: EnvRow[]): string {
  let out = command;
  for (const e of env) {
    if (e.secret && e.value) out = out.split(e.value).join("••••••");
  }
  return out;
}
