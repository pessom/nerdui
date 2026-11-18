// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { components } from "../../api/client";

export type NamespaceSummary = components["schemas"]["Summary"]["namespaces"][number];

/** containerd's identifier rule: letters and digits separated by _ . - (up to 76). */
const NAME = /^[A-Za-z0-9]+(?:[._-](?:[A-Za-z0-9]+))*$/;

/** Namespaces other tools rely on: removing them takes typing the name (#195). */
export const WELL_KNOWN = ["default", "k8s.io", "moby", "buildkit"];

export function nameError(name: string): string | undefined {
  if (!name) return undefined;
  if (name.length > 76) return "At most 76 characters";
  if (!NAME.test(name)) return "Letters and digits, separated by single . _ or -";
  return undefined;
}

/** What keeps a namespace from being removed, as far as the counters tell. */
export function holders(n: NamespaceSummary): string[] {
  const out: string[] = [];
  const add = (count: number, word: string) => {
    if (count > 0) out.push(`${count} ${word}${count === 1 ? "" : "s"}`);
  };
  add(n.containers.total, "container");
  add(n.images, "image");
  add(n.volumes, "volume");
  return out;
}
