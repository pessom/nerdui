// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** "key=value" lines → labels; returns an error message for a malformed line. */
export function parseLabels(text: string): { labels: Record<string, string>; error?: string } {
  const labels: Record<string, string> = {};
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const i = line.indexOf("=");
    const key = (i < 0 ? line : line.slice(0, i)).trim();
    if (!key) return { labels, error: `Label without a key: "${line}"` };
    labels[key] = i < 0 ? "" : line.slice(i + 1);
  }
  return { labels };
}
