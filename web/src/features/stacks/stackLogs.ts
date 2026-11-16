// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useCallback, useEffect, useRef, useState } from "react";
import type { components } from "../../api/client";
import { openStream, streamURL, type StreamError } from "../../lib/ws";
import { MAX_LINES, formatLine, tsKey, type LogsStatus } from "../containers/logs";

export type StackLogLine = components["schemas"]["StackLogLine"];

export interface StackEntry extends StackLogLine {
  seq: number;
}

/** Lines per container read as history (the server caps it at 5000). */
export const STACK_TAILS = ["100", "200", "500", "1000", "5000"];

// Distinct, readable on light and dark backgrounds.
const COLORS = ["36", "35", "33", "32", "34", "91", "96", "95", "93", "92"];

/** ANSI color of a service, stable by its position among the stack's services. */
export function serviceColor(services: readonly string[], service: string): string {
  const i = services.indexOf(service);
  return COLORS[(i < 0 ? 0 : i) % COLORS.length]!;
}

/** Terminal text of a stack's line: the colored service, then the line as for a container. */
export function formatStackLine(
  e: StackLogLine,
  timestamps: boolean,
  services: readonly string[],
  width: number,
): string {
  const label = `\x1b[${serviceColor(services, e.service)}m${e.service.padEnd(width)} │\x1b[0m`;
  return `${label}${formatLine(e, timestamps)}`;
}

export function stackLogsPath(env: string, name: string): string {
  return `/api/v1/environments/${encodeURIComponent(env)}/stacks/${encodeURIComponent(name)}/logs/stream`;
}

/**
 * Streams the merged logs of a stack (#30). A reconnect resumes from the oldest of the
 * containers' last timestamps and drops lines each container already sent.
 */
export function useStackLogs(
  env: string,
  name: string,
  params: { namespace: string; follow: boolean; tail: string },
) {
  const { namespace, follow, tail } = params;
  const key = [env, name, namespace, follow, tail].join("\0");
  const [buffer, setBuffer] = useState<{ key: string; entries: StackEntry[] }>({
    key,
    entries: [],
  });
  const [status, setStatus] = useState<LogsStatus>("connecting");
  const [error, setError] = useState<{ key: string; error: StreamError }>();
  const [generation, setGeneration] = useState(0);
  const seq = useRef(0);
  const last = useRef<{ key: string; ts: Map<string, string> }>({ key, ts: new Map() });

  useEffect(() => {
    if (last.current.key !== key) last.current = { key, ts: new Map() };
    let ended = false;
    const path = stackLogsPath(env, name);
    const stream = openStream<StackLogLine[]>(
      path,
      {
        onData: (lines) => {
          const seen = last.current.ts;
          const fresh = lines.filter((l) => {
            const prev = seen.get(l.containerId);
            return !l.ts || !prev || tsKey(l.ts) > tsKey(prev);
          });
          for (const l of fresh) if (l.ts) seen.set(l.containerId, l.ts);
          if (fresh.length === 0) return;
          setBuffer((b) => {
            const base = b.key === key ? b.entries : [];
            const next = base.concat(fresh.map((l) => ({ ...l, seq: ++seq.current })));
            return {
              key,
              entries: next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next,
            };
          });
        },
        onError: (e) => setError({ key, error: e }),
        onEnd: () => {
          ended = true;
          setStatus("ended");
        },
        onStatus: (s) => {
          if (s === "open") setError(undefined);
          if (!(s === "closed" && ended)) setStatus(s);
        },
      },
      {
        url: () => {
          const q = new URLSearchParams({ namespace, tail });
          if (follow) q.set("follow", "true");
          const stamps = [...last.current.ts.values()];
          if (stamps.length) {
            q.set(
              "since",
              stamps.reduce((a, b) => (tsKey(a) < tsKey(b) ? a : b)),
            );
            q.set("tail", "5000");
          }
          return streamURL(`${path}?${q.toString()}`);
        },
      },
    );
    return () => stream.close();
  }, [key, env, name, namespace, follow, tail, generation]);

  const restart = useCallback(() => setGeneration((g) => g + 1), []);
  const clear = useCallback(() => setBuffer({ key, entries: [] }), [key]);
  return {
    entries: buffer.key === key ? buffer.entries : noEntries,
    status,
    error: error?.key === key ? error.error : undefined,
    restart,
    clear,
  };
}

const noEntries: StackEntry[] = [];
