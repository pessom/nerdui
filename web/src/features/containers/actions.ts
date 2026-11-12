// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { notifications } from "@mantine/notifications";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, unwrapEmpty, type components } from "../../api/client";
import { keys } from "../../api/queries";
import { displayName, type Container } from "./format";

export type ContainerAction = components["schemas"]["ContainerActionName"];
type BulkResult = components["schemas"]["BulkActionResult"];

export interface ActionInfo {
  label: string;
  /** Past tense for notifications. */
  done: string;
  /** Destructive actions ask for confirmation. */
  confirm?: boolean;
  color?: string;
}

export const actionInfo: Record<ContainerAction, ActionInfo> = {
  start: { label: "Start", done: "Started" },
  stop: { label: "Stop", done: "Stopped" },
  restart: { label: "Restart", done: "Restarted" },
  pause: { label: "Pause", done: "Paused" },
  unpause: { label: "Resume", done: "Resumed" },
  kill: { label: "Kill", done: "Killed", confirm: true, color: "red" },
  remove: { label: "Remove", done: "Removed", confirm: true, color: "red" },
};

export const actionOrder: ContainerAction[] = [
  "start",
  "stop",
  "restart",
  "pause",
  "unpause",
  "kill",
  "remove",
];

/** Whether action applies to the container's state; mirrors the server's 409 checks. */
export function canApply(action: ContainerAction, c: Container): boolean {
  const live = c.state === "running" || c.state === "paused";
  switch (action) {
    case "start":
      return !live;
    case "stop":
      return live || c.state === "restarting";
    case "kill":
      return live;
    case "pause":
      return c.state === "running";
    case "unpause":
      return c.state === "paused";
    case "restart":
    case "remove":
      return true;
  }
}

export interface ActionRequest {
  action: ContainerAction;
  containers: Container[];
  force?: boolean;
  volumes?: boolean;
}

/**
 * Runs container actions: one container through its own endpoint, several through the bulk
 * endpoint. Reports the outcome as notifications and refreshes the list. `busy` holds the
 * IDs with an action in flight.
 */
export function useContainerActions(env: string, namespace: string) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());

  const mutation = useMutation({
    mutationFn: async ({ action, containers, force, volumes }: ActionRequest) => {
      const [only] = containers;
      if (containers.length === 1 && only) {
        const id = only.id;
        const path = { env, id };
        const query = { namespace };
        if (action === "remove") {
          await unwrapEmpty(
            api.DELETE("/api/v1/environments/{env}/containers/{id}", {
              params: { path, query: { ...query, force, volumes } },
            }),
          );
        } else {
          await unwrapEmpty(
            api.POST("/api/v1/environments/{env}/containers/{id}/{action}", {
              params: { path: { ...path, action }, query },
            }),
          );
        }
        return [{ id, ok: true }] as BulkResult[];
      }
      const res = await unwrap(
        api.POST("/api/v1/environments/{env}/containers/actions", {
          params: { path: { env }, query: { namespace } },
          body: {
            action,
            ids: containers.map((c) => c.id),
            ...(action === "remove" ? { force, volumes } : {}),
          },
        }),
      );
      return res.results;
    },
    onMutate: ({ containers }) => {
      setBusy((prev) => new Set([...prev, ...containers.map((c) => c.id)]));
    },
    onSettled: (_data, _error, { containers }) => {
      setBusy((prev) => {
        const next = new Set(prev);
        for (const c of containers) next.delete(c.id);
        return next;
      });
      void queryClient.invalidateQueries({ queryKey: keys.containers(env, namespace) });
    },
    onSuccess: (results, { action, containers }) => notifyResults(action, containers, results),
    onError: (error, { action, containers }) => {
      const info = actionInfo[action];
      notifications.show({
        color: "red",
        title: `Cannot ${info.label.toLowerCase()} ${subject(containers)}`,
        message: error.message,
        autoClose: false,
      });
    },
  });

  return { run: mutation.mutate, busy };
}

/** "web-1" for one container, "3 containers" for several. */
function subject(containers: Container[]): string {
  const [only] = containers;
  return containers.length === 1 && only ? displayName(only) : `${containers.length} containers`;
}

function notifyResults(action: ContainerAction, containers: Container[], results: BulkResult[]) {
  const info = actionInfo[action];
  const byId = new Map(containers.map((c) => [c.id, c]));
  const failed = results.filter((r) => !r.ok);
  const ok = results.length - failed.length;
  if (failed.length === 0) {
    notifications.show({
      color: "teal",
      message: `${info.done} ${subject(containers)}`,
    });
    return;
  }
  const lines = failed.map((r) => {
    const c = byId.get(r.id);
    return `${c ? displayName(c) : r.id.slice(0, 12)}: ${r.error?.message ?? "failed"}`;
  });
  notifications.show({
    color: ok ? "yellow" : "red",
    title: ok
      ? `${info.done} ${ok} of ${results.length} containers`
      : `Cannot ${info.label.toLowerCase()} containers`,
    message: lines.join("\n"),
    style: { whiteSpace: "pre-line" },
    autoClose: false,
  });
}
