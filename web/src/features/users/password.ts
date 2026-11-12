// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/** Server password policy (ADR-0005): at least 12 characters. */
export const MIN_PASSWORD = 12;

export function tooShort(password: string): boolean {
  return password.length > 0 && [...password].length < MIN_PASSWORD;
}

/** Same rule as the server: letters, digits, '.', '_' or '-', up to 64 characters. */
export const USERNAME_RE = /^[A-Za-z0-9._-]{1,64}$/;
