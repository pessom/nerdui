// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * WebSocket stream client (ADR-0007). Every stream is a separate WebSocket carrying JSON
 * envelopes `{type: "data" | "error" | "end"}`. The client reconnects with exponential
 * backoff and jitter until the server sends "end" or the caller closes the stream.
 */

export interface StreamError {
  code: string;
  message: string;
}

export type StreamMessage<T> =
  | { type: "data"; data: T; ts: string }
  | { type: "error"; error: StreamError; ts: string }
  | { type: "end"; ts: string };

export type StreamStatus = "connecting" | "open" | "reconnecting" | "closed";

export interface StreamHandlers<T> {
  onData: (data: T) => void;
  onError?: (error: StreamError) => void;
  onEnd?: () => void;
  onStatus?: (status: StreamStatus) => void;
}

export interface StreamOptions {
  /** Reconnect after unexpected closes (default true). */
  reconnect?: boolean;
  /** First retry delay in ms (default 1000). */
  baseDelayMs?: number;
  /** Maximum retry delay in ms (default 30000). */
  maxDelayMs?: number;
  /** Builds the URL for each (re)connect, e.g. to resume logs from the last timestamp. */
  url?: () => string;
  /** WebSocket implementation (tests). */
  WebSocketImpl?: typeof WebSocket;
  /** Random source for jitter (tests). */
  random?: () => number;
}

export interface Stream {
  close: () => void;
}

/** Absolute ws(s):// URL for an API path on the current origin. */
export function streamURL(
  path: string,
  loc: Pick<Location, "protocol" | "host"> = window.location,
): string {
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}${path}`;
}

/** Delay before reconnect attempt n (0-based): exponential, capped, 50–100 % jitter. */
export function backoffDelay(
  attempt: number,
  base: number,
  max: number,
  random: () => number,
): number {
  const exp = Math.min(max, base * 2 ** attempt);
  return Math.round(exp * (0.5 + random() * 0.5));
}

export function openStream<T>(
  path: string,
  handlers: StreamHandlers<T>,
  opts: StreamOptions = {},
): Stream {
  const {
    reconnect = true,
    baseDelayMs = 1000,
    maxDelayMs = 30000,
    url = () => streamURL(path),
    WebSocketImpl = WebSocket,
    random = Math.random,
  } = opts;

  let socket: WebSocket | null = null;
  let attempt = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;

  const setStatus = (s: StreamStatus) => handlers.onStatus?.(s);

  const connect = () => {
    setStatus(attempt === 0 ? "connecting" : "reconnecting");
    const ws = new WebSocketImpl(url());
    socket = ws;

    ws.onopen = () => {
      attempt = 0;
      setStatus("open");
    };

    ws.onmessage = (ev: MessageEvent) => {
      if (typeof ev.data !== "string") return; // binary frames are for exec only
      let msg: StreamMessage<T>;
      try {
        msg = JSON.parse(ev.data) as StreamMessage<T>;
      } catch {
        handlers.onError?.({ code: "invalid_message", message: "malformed stream message" });
        return;
      }
      switch (msg.type) {
        case "data":
          handlers.onData(msg.data);
          break;
        case "error":
          handlers.onError?.(msg.error);
          break;
        case "end":
          stopped = true;
          handlers.onEnd?.();
          break;
      }
    };

    ws.onclose = () => {
      if (socket !== ws) return;
      socket = null;
      if (stopped || !reconnect) {
        stopped = true;
        setStatus("closed");
        return;
      }
      const delay = backoffDelay(attempt, baseDelayMs, maxDelayMs, random);
      attempt++;
      setStatus("reconnecting");
      timer = setTimeout(connect, delay);
    };
  };

  connect();

  return {
    close: () => {
      if (stopped && !socket) return;
      stopped = true;
      clearTimeout(timer);
      const ws = socket;
      socket = null;
      ws?.close(1000, "client closed");
      setStatus("closed");
    },
  };
}
