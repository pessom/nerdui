// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** The URL is an SSH one: ssh:// or scp-like git@host:path. */
export const isSSH = (url: string) => /^ssh:\/\//.test(url) || /^[\w.-]+@[\w.-]+:[^/]/.test(url);

/** Name suggested from a repository URL: its last path element without ".git". */
export function repoName(url: string): string {
  const last = url.trim().replace(/\/+$/, "").split(/[/:]/).pop() ?? "";
  return last
    .replace(/\.git$/, "")
    .toLowerCase()
    .replace(/[^a-z0-9_.-]/g, "-")
    .replace(/^[^a-z0-9]+/, "");
}

/** A browsable https URL of the repository, when it has one. */
export function webURL(url: string): string | undefined {
  const scp = /^git@([\w.-]+):(.+?)(\.git)?$/.exec(url);
  if (scp) return `https://${scp[1]}/${scp[2]}`;
  if (/^https?:\/\//.test(url)) {
    try {
      const u = new URL(url);
      u.username = "";
      u.password = "";
      return u.toString().replace(/\.git$/, "");
    } catch {
      return undefined;
    }
  }
  return undefined;
}
