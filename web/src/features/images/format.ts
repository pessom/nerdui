// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { Image } from "../../api/queries";

/** "docker.io/library/nginx:1.27" → "nginx:1.27"; dangling images show their short ID. */
export function displayName(img: Image): string {
  if (!img.repository) return `<none> ${shortID(img.digest)}`;
  return img.tag ? `${img.repository}:${img.tag}` : img.repository;
}

/** "sha256:abcdef…" → "abcdef123456", like `nerdctl images`. */
export function shortID(digest: string): string {
  const hex = digest.includes(":") ? digest.slice(digest.indexOf(":") + 1) : digest;
  return hex.slice(0, 12);
}

export function searchText(img: Image): string {
  return [img.name, img.digest, ...img.platforms].join(" ").toLowerCase();
}

export type UsageFilter = "all" | "used" | "unused";

export function matchesUsage(img: Image, filter: UsageFilter): boolean {
  if (filter === "used") return img.containers > 0;
  if (filter === "unused") return img.containers === 0;
  return true;
}
