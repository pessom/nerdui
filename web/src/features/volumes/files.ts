// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** URL that downloads a file, or a directory as .tar.gz, of a volume (#58). */
export function volumeDownloadUrl(env: string, namespace: string, volume: string, path: string) {
  const q = new URLSearchParams({ namespace, path });
  return `/api/v1/environments/${encodeURIComponent(env)}/volumes/${encodeURIComponent(volume)}/files/download?${q.toString()}`;
}
