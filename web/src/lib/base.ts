// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The path prefix nerdui is served under behind a reverse proxy (#222, ADR-0046), "" or e.g.
 * "/nerdui": the server writes it into <base href> of index.html.
 */
export const basePath: string = (() => {
  try {
    return new URL(document.baseURI).pathname.replace(/\/+$/, "");
  } catch {
    return "";
  }
})();

/** An absolute path on this nerdui, e.g. withBase("/api/v1/version"). */
export function withBase(path: string): string {
  return basePath + path;
}

/** An absolute URL on this nerdui, for links shown to users (webhook URLs, curl examples). */
export function absoluteURL(path: string): string {
  return window.location.origin + basePath + path;
}
