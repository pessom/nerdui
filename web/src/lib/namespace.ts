// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The last namespace chosen per environment, kept in this browser only. Storage can be
// unavailable (private mode, blocked site data): reads then return nothing and writes are
// skipped.

/** The ?ns= value of a list that shows every namespace (#276). */
export const ALL_NAMESPACES = "*";

/** Pages of an environment whose list can show every namespace. */
export const ALL_NAMESPACE_PAGES = [
  "containers",
  "images",
  "volumes",
  "networks",
  "stacks",
  "events",
] as const;

/** A tooltip for actions that need one namespace while all are listed. */
export const CHOOSE_NAMESPACE = "Choose a namespace to create or change things in it";

/** Groups items by their namespace, in the order the namespaces first appear. */
export function byNamespace<T extends { namespace: string }>(items: T[]): Map<string, T[]> {
  const groups = new Map<string, T[]>();
  for (const item of items) {
    const group = groups.get(item.namespace);
    if (group) group.push(item);
    else groups.set(item.namespace, [item]);
  }
  return groups;
}

const key = (env: string) => `nerdui.namespace.${env}`;

export function rememberedNamespace(env: string): string | undefined {
  try {
    return localStorage.getItem(key(env)) ?? undefined;
  } catch {
    return undefined;
  }
}

export function rememberNamespace(env: string, ns: string | undefined): void {
  try {
    if (ns) localStorage.setItem(key(env), ns);
    else localStorage.removeItem(key(env));
  } catch {
    // not persisted
  }
}
