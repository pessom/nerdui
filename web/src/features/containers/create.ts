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

/** Opts a container in to automatic updates (#54, ADR-0032). */
export const AUTO_UPDATE_LABEL = "nerdui.auto-update";

/** Opts a container in to restarts when its healthcheck fails (#22, ADR-0043). */
export const AUTO_HEAL_LABEL = "nerdui.auto-heal";

/** Whether a boolean nerdui label is on, as the server reads it. */
export function labelEnabled(
  labels: Record<string, string> | null | undefined,
  label: string,
): boolean {
  const v = labels?.[label]?.trim().toLowerCase();
  return v === "true" || v === "1" || v === "yes" || v === "on";
}

/** Labels the form sets with its own switches. */
const managedLabels = [AUTO_UPDATE_LABEL, AUTO_HEAL_LABEL];

/** Whether labels opt in to automatic updates, as the server reads them. */
export function autoUpdateEnabled(labels?: Record<string, string> | null): boolean {
  return labelEnabled(labels, AUTO_UPDATE_LABEL);
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
  /** Pull and recreate when the image's tag gets a newer image (#54): the nerdui.auto-update label. */
  autoUpdate: boolean;
  /** Healthcheck (#237): the image's, one set here, or none. */
  health: "image" | "custom" | "none";
  healthCmd: string;
  healthInterval: string;
  healthTimeout: string;
  healthStartPeriod: string;
  healthRetries: string;
  /** Restart when unhealthy (#22): the nerdui.auto-heal label. */
  autoHeal: boolean;
  start: boolean;
  // Advanced (#18)
  extraNetworks: string[];
  ip: string;
  privileged: boolean;
  capAdd: string[];
  capDrop: string[];
  devices: string[];
  dns: string[];
  extraHosts: string[];
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
  autoUpdate: false,
  health: "image",
  healthCmd: "",
  healthInterval: "",
  healthTimeout: "",
  healthStartPeriod: "",
  healthRetries: "",
  autoHeal: false,
  start: true,
  extraNetworks: [],
  ip: "",
  privileged: false,
  capAdd: [],
  capDrop: [],
  devices: [],
  dns: [],
  extraHosts: [],
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
  const labels: Record<string, string> = Object.fromEntries(
    f.labels
      .filter((l) => l.key.trim() && !managedLabels.includes(l.key.trim()))
      .map((l) => [l.key.trim(), l.value]),
  );
  if (f.autoUpdate) labels[AUTO_UPDATE_LABEL] = "true";
  if (f.autoHeal && f.health !== "none") labels[AUTO_HEAL_LABEL] = "true";
  if (Object.keys(labels).length) request.labels = labels;
  const list = (k: "extraNetworks" | "capAdd" | "capDrop" | "devices" | "dns" | "extraHosts") => {
    const v = f[k].map((x) => x.trim()).filter(Boolean);
    if (v.length) request[k] = v;
  };
  list("extraNetworks");
  opt("ip", f.ip.trim());
  if (f.privileged) request.privileged = true;
  list("capAdd");
  list("capDrop");
  list("devices");
  list("dns");
  list("extraHosts");
  if (f.health === "none") request.healthcheck = { disabled: true };
  if (f.health === "custom") {
    const h: NonNullable<CreateRequest["healthcheck"]> = {};
    if (f.healthCmd.trim()) h.command = f.healthCmd.trim();
    if (f.healthInterval.trim()) h.interval = f.healthInterval.trim();
    if (f.healthTimeout.trim()) h.timeout = f.healthTimeout.trim();
    if (f.healthStartPeriod.trim()) h.startPeriod = f.healthStartPeriod.trim();
    if (f.healthRetries.trim()) h.retries = Number(f.healthRetries);
    if (Object.keys(h).length) request.healthcheck = h;
  }
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

/** Joins arguments into a line that parseArgs splits back into the same arguments. */
export function joinArgs(args: string[]): string {
  return args
    .map((a) =>
      a !== "" && /^[A-Za-z0-9_./:=@%+,-]+$/.test(a) ? a : `'${a.replaceAll("'", `'"'"'`)}'`,
    )
    .join(" ");
}

const restarts = ["no", "always", "unless-stopped", "on-failure"] as const;

/** The form for an existing container's settings (#18): the inverse of toRequest. */
export function fromRequest(r: CreateRequest): CreateForm {
  const [policy, retries] = (r.restart ?? "no").split(":");
  return {
    ...emptyForm,
    image: r.image,
    name: r.name ?? "",
    command: joinArgs(r.command ?? []),
    entrypoint: r.entrypoint ?? "",
    ports: (r.ports ?? []).map((p) => ({
      hostIp: p.hostIp ?? "",
      hostPort: p.hostPort ? String(p.hostPort) : "",
      containerPort: String(p.containerPort),
      protocol: p.protocol === "udp" ? "udp" : "tcp",
    })),
    env: (r.env ?? []).map((e) => ({ name: e.name, value: e.value, secret: false })),
    mounts: (r.mounts ?? []).map((m) => ({
      type: m.type as MountRow["type"],
      source: m.source ?? "",
      target: m.target,
      readOnly: !!m.readOnly,
    })),
    network: r.network ?? "",
    hostname: r.hostname ?? "",
    user: r.user ?? "",
    workdir: r.workdir ?? "",
    cpus: r.cpus ? String(r.cpus) : "",
    memoryMiB: r.memoryBytes ? String(r.memoryBytes / 1024 / 1024) : "",
    restart: (restarts as readonly string[]).includes(policy ?? "")
      ? (policy as CreateForm["restart"])
      : "no",
    retries: retries ?? "",
    labels: Object.entries(r.labels ?? {})
      .filter(([key]) => !managedLabels.includes(key))
      .map(([key, value]) => ({ key, value })),
    autoUpdate: autoUpdateEnabled(r.labels),
    autoHeal: labelEnabled(r.labels, AUTO_HEAL_LABEL),
    health: r.healthcheck?.disabled ? "none" : r.healthcheck ? "custom" : "image",
    healthCmd: r.healthcheck?.command ?? "",
    healthInterval: r.healthcheck?.interval ?? "",
    healthTimeout: r.healthcheck?.timeout ?? "",
    healthStartPeriod: r.healthcheck?.startPeriod ?? "",
    healthRetries: r.healthcheck?.retries ? String(r.healthcheck.retries) : "",
    start: true,
    extraNetworks: r.extraNetworks ?? [],
    ip: r.ip ?? "",
    privileged: !!r.privileged,
    capAdd: r.capAdd ?? [],
    capDrop: r.capDrop ?? [],
    devices: r.devices ?? [],
    dns: r.dns ?? [],
    extraHosts: r.extraHosts ?? [],
  };
}
