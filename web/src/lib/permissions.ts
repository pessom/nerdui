// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useQuery } from "@tanstack/react-query";
import type { components } from "../api/schema";
import { meQuery } from "../api/queries";

export type Permission = components["schemas"]["Permission"];

/** Tells whether a session may perform an action (ADR-0058); the server checks it anyway. */
export function allows(
  session: { permissions?: Permission[] } | undefined,
  action: Permission,
): boolean {
  // A server without roles sends no list: every user was an administrator there.
  if (!session?.permissions) return true;
  return session.permissions.includes(action);
}

/** The caller's permissions as a function, for hiding what they may not do. */
export function useCan(): (action: Permission) => boolean {
  const { data } = useQuery(meQuery);
  return (action) => allows(data, action);
}
