// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useCallback, useEffect, useRef, useState } from "react";
import type { components } from "../../api/client";
import { openStream, streamURL, type StreamError, type StreamStatus } from "../../lib/ws";

export type LogLine = components["schemas"]["LogLine"];

/** A received line with a sequence number, so viewers can append only what is new. */
export interface Entry extends LogLine {
  seq: number;
}

/** Lines kept in the browser; older ones are dropped (download has everything). */
export const MAX_LINES = 10_000;

export type StreamFilter = "all" | "stdout" | "stderr";

export interface ViewOptions {
  timestamps: boolean;
  stream: StreamFilter;
  /** Filter text; a regular expression when regex is set. */
  filter: string;
  regex: boolean;
}

// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g;

/** Text without ANSI escape sequences, for filtering. */
export function stripAnsi(s: string): string {
  return s.replace(ANSI, "");
}

/** Builds the line filter; an invalid regular expression is reported, not thrown. */
export function compileFilter(
  text: string,
  regex: boolean,
): { test: (s: string) => boolean; error?: string } {
  if (!text) return { test: () => true };
  if (!regex) {
    const needle = text.toLowerCase();
    return { test: (s) => s.toLowerCase().includes(needle) };
  }
  try {
    const re = new RegExp(text, "i");
    return { test: (s) => re.test(s) };
  } catch (e) {
    return { test: () => true, error: e instanceof Error ? e.message : String(e) };
  }
}

export function lineMatches(
  e: LogLine,
  opts: Pick<ViewOptions, "stream">,
  test: (s: string) => boolean,
): boolean {
  return (opts.stream === "all" || e.stream === opts.stream) && test(stripAnsi(e.line));
}

const DIM = "\x1b[2m";
const RED = "\x1b[31m";
const RESET = "\x1b[0m";

/**
 * Terminal text of a line: optional dim timestamp, a red bar for stderr, the line with its
 * own colors. A trailing reset keeps unterminated colors from leaking into the next line.
 */
export function formatLine(e: LogLine, timestamps: boolean): string {
  const ts = timestamps ? `${DIM}${(e.ts ?? "").padEnd(30)}${RESET} ` : "";
  const mark = e.stream === "stderr" ? `${RED}▌${RESET}` : " ";
  return `${ts}${mark}${e.line}${RESET}`;
}

/**
 * Sortable form of a server timestamp (UTC, RFC 3339 with up to 9 fraction digits, trailing
 * zeros trimmed by Go): "…T10:00:01Z" and "…T10:00:01.5Z" compare correctly as strings.
 */
export function tsKey(ts: string): string {
  const m = /^(.{19})(?:\.(\d{1,9}))?Z$/.exec(ts);
  return m ? `${m[1]}.${(m[2] ?? "").padEnd(9, "0")}` : ts;
}

export function logsPath(env: string, id: string): string {
  return `/api/v1/environments/${encodeURIComponent(env)}/containers/${encodeURIComponent(id)}/logs`;
}

export interface StreamParams {
  namespace: string;
  follow: boolean;
  /** Number of lines, or "all". */
  tail: string;
}

export function streamQuery(p: StreamParams, since?: string): string {
  const q = new URLSearchParams({ namespace: p.namespace, tail: p.tail });
  if (p.follow) q.set("follow", "true");
  if (since) q.set("since", since);
  return q.toString();
}

export type LogsStatus = StreamStatus | "ended";

/**
 * Streams the logs of a container (ADR-0014). Reconnects resume from the last timestamp:
 * the server filters by second, so lines up to that timestamp are skipped. `restart`
 * reconnects with the same buffer (e.g. after the container started again).
 */
export function useContainerLogs(env: string, id: string, params: StreamParams) {
  const { namespace, follow, tail } = params;
  // Another container, tail or follow setting starts from scratch.
  const key = [env, id, namespace, follow, tail].join("\0");
  const [buffer, setBuffer] = useState<{ key: string; entries: Entry[] }>({ key, entries: [] });
  const [status, setStatus] = useState<LogsStatus>("connecting");
  const [error, setError] = useState<{ key: string; error: StreamError }>();
  const [generation, setGeneration] = useState(0);
  const seq = useRef(0);
  const lastTs = useRef<{ key: string; ts?: string }>({ key });

  useEffect(() => {
    if (lastTs.current.key !== key) lastTs.current = { key };
    let ended = false;
    const path = `${logsPath(env, id)}/stream`;
    const stream = openStream<LogLine[]>(
      path,
      {
        onData: (lines) => {
          const last = lastTs.current.ts && tsKey(lastTs.current.ts);
          const fresh = last ? lines.filter((l) => !l.ts || tsKey(l.ts) > last) : lines;
          for (const l of fresh) if (l.ts) lastTs.current = { key, ts: l.ts };
          if (fresh.length === 0) return;
          setBuffer((prev) => {
            const base = prev.key === key ? prev.entries : [];
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
          const since = lastTs.current.ts;
          // Resume after the last line: tail no longer applies.
          const q = since
            ? streamQuery({ namespace, follow, tail: "all" }, since)
            : streamQuery({ namespace, follow, tail });
          return streamURL(`${path}?${q}`);
        },
      },
    );
    return () => stream.close();
  }, [key, env, id, namespace, follow, tail, generation]);

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

const noEntries: Entry[] = [];
