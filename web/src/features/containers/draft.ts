// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { CreateForm } from "./create";

/**
 * A filled-in create form handed to the new container page, e.g. from a template (ADR-0059).
 * It stays in memory: the values may hold secrets and never go into the URL or history.
 */
export type ContainerDraft = {
  form: CreateForm;
  /** Where it came from, e.g. "template Uptime Kuma". */
  origin: string;
  /** Settings of the source the form cannot express. */
  unsupported: string[];
};

let pending: ContainerDraft | undefined;

export function setContainerDraft(d: ContainerDraft) {
  pending = d;
}

/** Returns the pending draft once. */
export function takeContainerDraft(): ContainerDraft | undefined {
  const d = pending;
  pending = undefined;
  return d;
}
