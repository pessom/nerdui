// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import type { components } from "../api/client";
import { keys } from "../api/queries";
import { openStream, type StreamStatus } from "../lib/ws";

export type RuntimeEvent = components["schemas"]["Event"];

/** Coalesces bursts of events (e.g. compose up) into one refetch. */
export const INVALIDATE_DEBOUNCE_MS = 300;

export interface EnvEventsState {
  /** containerd event subscription state reported by the server. */
  containerd: "unknown" | "connected" | "disconnected";
  /** WebSocket state between the browser and nerdui. */
  stream: StreamStatus;
}

/**
 * Subscribes to the environment's containerd events (ADR-0007) and invalidates its cached
 * queries, so changes made anywhere — including the nerdctl CLI — appear without polling.
 * On "connected" (initial or after an outage) everything of the environment is refetched.
 */
export function useEnvEvents(
  env: string,
  namespace: string,
  openStreamImpl = openStream,
): EnvEventsState {
  const queryClient = useQueryClient();
  const [state, setState] = useState<EnvEventsState>({
    containerd: "unknown",
    stream: "connecting",
  });
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    const invalidate = (delay: number) => {
      clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: keys.env(env) });
      }, delay);
    };
    const path = `/api/v1/environments/${encodeURIComponent(env)}/events/stream?namespace=${encodeURIComponent(namespace)}`;
    const stream = openStreamImpl<RuntimeEvent>(path, {
      onData: (ev) => {
        if (ev.kind === "nerdui") {
          const connected = ev.action === "connected";
          setState((s) => ({ ...s, containerd: connected ? "connected" : "disconnected" }));
          if (connected) invalidate(0);
          return;
        }
        invalidate(INVALIDATE_DEBOUNCE_MS);
      },
      onStatus: (stream) => setState((s) => ({ ...s, stream })),
    });
    return () => {
      clearTimeout(timer.current);
      stream.close();
    };
  }, [env, namespace, queryClient, openStreamImpl]);

  return state;
}
