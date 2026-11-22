// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** The file name from a Content-Disposition header. */
export function attachmentName(header: string | null, fallback: string): string {
  const m = /filename="?([^";]+)"?/.exec(header ?? "");
  return m?.[1] ?? fallback;
}
