// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// The last namespace chosen per environment, kept in this browser only. Storage can be
// unavailable (private mode, blocked site data): reads then return nothing and writes are
// skipped.

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
