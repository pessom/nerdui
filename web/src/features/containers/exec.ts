// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** Splits a custom command on spaces; quotes are not interpreted (use sh -c for that). */
export function splitCommand(cmd: string): string[] {
  return cmd.trim().split(/\s+/).filter(Boolean);
}

/** Query of the exec WebSocket for the chosen options. */
export function execPath(
  env: string,
  id: string,
  namespace: string,
  opts: { cmd: string[]; user: string; workdir: string },
): string {
  const q = new URLSearchParams({ namespace });
  for (const a of opts.cmd) q.append("cmd", a);
  if (opts.user) q.set("user", opts.user);
  if (opts.workdir) q.set("workdir", opts.workdir);
  return `/api/v1/environments/${encodeURIComponent(env)}/containers/${encodeURIComponent(id)}/exec?${q.toString()}`;
}
