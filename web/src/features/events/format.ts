// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { HistoryEvent } from "../../api/queries";

/** Badge color of an event: failures red, removals orange, starts green. */
export function eventColor(e: HistoryEvent): string {
  if (e.kind === "nerdui") return e.action === "connected" ? "teal" : "red";
  if (e.action === "oom") return "red";
  if (e.action === "exit") return e.attributes?.exitStatus === "0" ? "gray" : "red";
  if (e.action === "delete") return "orange";
  if (e.action === "start" || e.action === "create") return "teal";
  return "blue";
}

/** "container create", "task exit (137)", "containerd disconnected". */
export function eventLabel(e: HistoryEvent): string {
  if (e.kind === "nerdui") return `containerd ${e.action}`;
  const status = e.action === "exit" && e.attributes?.exitStatus;
  return status ? `${e.kind} ${e.action} (${status})` : `${e.kind} ${e.action}`;
}

/** Container name, or the short ID; images by name. */
export function eventObject(e: HistoryEvent): string {
  if (e.objectName) return e.objectName;
  const id = e.objectId ?? "";
  return /^[0-9a-f]{64}$/.test(id) ? id.slice(0, 12) : id;
}

/** Attributes worth showing, without those already in the label. */
export function eventDetails(e: HistoryEvent): string {
  return Object.entries(e.attributes ?? {})
    .filter(([k]) => k !== "exitStatus")
    .map(([k, v]) => `${k}=${v}`)
    .join(" ");
}
