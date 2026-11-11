// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { backoffDelay, openStream, streamURL, type StreamStatus } from "./ws";

class FakeSocket {
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  closed = false;
  constructor(public url: string) {
    FakeSocket.instances.push(this);
  }
  open() {
    this.onopen?.();
  }
  message(data: unknown) {
    this.onmessage?.({
      data: typeof data === "string" ? data : JSON.stringify(data),
    } as MessageEvent);
  }
  drop() {
    this.onclose?.();
  }
  close() {
    this.closed = true;
    this.onclose?.();
  }
}

const Impl = FakeSocket as unknown as typeof WebSocket;
const last = () => FakeSocket.instances[FakeSocket.instances.length - 1]!;

describe("streamURL", () => {
  it("uses wss for https pages and ws otherwise", () => {
    expect(streamURL("/api/v1/x", { protocol: "https:", host: "nas:9443" })).toBe(
      "wss://nas:9443/api/v1/x",
    );
    expect(streamURL("/api/v1/x", { protocol: "http:", host: "localhost:5173" })).toBe(
      "ws://localhost:5173/api/v1/x",
    );
  });
});

describe("backoffDelay", () => {
  it("grows exponentially with jitter and a cap", () => {
    expect(backoffDelay(0, 1000, 30000, () => 1)).toBe(1000);
    expect(backoffDelay(0, 1000, 30000, () => 0)).toBe(500);
    expect(backoffDelay(3, 1000, 30000, () => 1)).toBe(8000);
    expect(backoffDelay(20, 1000, 30000, () => 1)).toBe(30000);
  });
});

describe("openStream", () => {
  beforeEach(() => {
    FakeSocket.instances = [];
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("dispatches data, error and end, and stops after end", () => {
    const onData = vi.fn();
    const onError = vi.fn();
    const onEnd = vi.fn();
    const statuses: StreamStatus[] = [];
    openStream(
      "/s",
      { onData, onError, onEnd, onStatus: (s) => statuses.push(s) },
      { WebSocketImpl: Impl, url: () => "ws://x/s" },
    );

    last().open();
    last().message({ type: "data", data: { n: 1 }, ts: "t" });
    last().message({ type: "error", error: { code: "not_found", message: "gone" }, ts: "t" });
    last().message("not json");
    last().message({ type: "end", ts: "t" });
    last().drop();
    vi.runAllTimers();

    expect(onData).toHaveBeenCalledWith({ n: 1 });
    expect(onError).toHaveBeenCalledWith({ code: "not_found", message: "gone" });
    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ code: "invalid_message" }));
    expect(onEnd).toHaveBeenCalledOnce();
    expect(FakeSocket.instances).toHaveLength(1); // no reconnect after end
    expect(statuses).toEqual(["connecting", "open", "closed"]);
  });

  it("reconnects with backoff and resets it after a successful open", () => {
    const urls = ["ws://x/1", "ws://x/2", "ws://x/3"];
    let i = 0;
    const statuses: StreamStatus[] = [];
    openStream(
      "/s",
      { onData: vi.fn(), onStatus: (s) => statuses.push(s) },
      {
        WebSocketImpl: Impl,
        url: () => urls[i++]!,
        random: () => 1,
      },
    );

    last().drop(); // fails before opening
    expect(statuses.at(-1)).toBe("reconnecting");
    vi.advanceTimersByTime(999);
    expect(FakeSocket.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeSocket.instances).toHaveLength(2);
    expect(last().url).toBe("ws://x/2"); // url() is evaluated per attempt

    last().drop(); // second failure: 2s
    vi.advanceTimersByTime(1999);
    expect(FakeSocket.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeSocket.instances).toHaveLength(3);

    last().open();
    last().drop(); // after a successful open the delay starts from 1s again
    vi.advanceTimersByTime(1000);
    expect(FakeSocket.instances).toHaveLength(4);
  });

  it("close() stops reconnecting", () => {
    const statuses: StreamStatus[] = [];
    const s = openStream(
      "/s",
      { onData: vi.fn(), onStatus: (st) => statuses.push(st) },
      { WebSocketImpl: Impl, url: () => "ws://x" },
    );
    last().open();
    last().drop();
    s.close();
    vi.runAllTimers();
    expect(FakeSocket.instances).toHaveLength(1);
    expect(statuses.at(-1)).toBe("closed");

    const s2 = openStream("/s", { onData: vi.fn() }, { WebSocketImpl: Impl, url: () => "ws://x" });
    const sock = last();
    sock.open();
    s2.close();
    expect(sock.closed).toBe(true);
    vi.runAllTimers();
    expect(FakeSocket.instances).toHaveLength(2);
  });

  it("does not reconnect when disabled", () => {
    openStream(
      "/s",
      { onData: vi.fn() },
      { WebSocketImpl: Impl, url: () => "ws://x", reconnect: false },
    );
    last().drop();
    vi.runAllTimers();
    expect(FakeSocket.instances).toHaveLength(1);
  });
});
