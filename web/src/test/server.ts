// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { vi } from "vitest";

export interface Call {
  method: string;
  path: string;
  query: URLSearchParams;
  headers: Headers;
  body: unknown;
}

type Handler = (call: Call) => Response | Promise<Response>;

/** Minimal fake of the nerdui API: routes "METHOD /path" to handlers and records calls. */
export function fakeApi(routes: Record<string, Handler>) {
  const calls: Call[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const req = input instanceof Request ? input : new Request(input);
    const url = new URL(req.url);
    const text = req.method === "GET" ? "" : await req.text();
    const call: Call = {
      method: req.method,
      path: url.pathname,
      query: url.searchParams,
      headers: req.headers,
      body: text ? JSON.parse(text) : undefined,
    };
    calls.push(call);
    const handler = routes[`${req.method} ${url.pathname}`];
    if (!handler) return json(404, { error: { code: "not_found", message: url.pathname } });
    return handler(call);
  });
  vi.stubGlobal("fetch", fetchMock);
  return { calls, fetchMock };
}

export function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

export const unauthenticated = () =>
  json(401, { error: { code: "unauthenticated", message: "authentication required" } });

export const session = {
  user: { id: 1, username: "admin", role: "admin", mustChangePassword: false },
  csrfToken: "csrf-123",
  expiresAt: "2027-01-01T00:00:00Z",
};

export const version = {
  nerdui: {
    version: "v0.1.0",
    commit: "abc",
    date: "now",
    goVersion: "go",
    platform: "linux/amd64",
    source: "https://example.com/fork/nerdui",
    license: "AGPL-3.0-only",
  },
};

export const runtime = {
  containerd: "v2.4.2",
  containerdRevision: "rev",
  containerdSupported: true,
  supportedContainerd: ["2.3", "2.4"],
  nerdctlLibrary: "v2.4.2",
  nerdctl: { path: "nerdctl", version: "2.4.2", required: "2.4", compatible: true },
};

export const envStatus = {
  id: "local",
  name: "Local host",
  kind: "local",
  defaultNamespace: "default",
  status: "connected",
  runtime,
  features: { exec: true },
};

/** No-op WebSocket so the events stream does not reach the network in tests. */
export class SilentWebSocket {
  onopen: (() => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  constructor(public url: string) {}
  close() {}
}

/** WebSocket fake that records instances, so tests can open, feed and drop streams. */
export class RecordingWebSocket {
  static instances: RecordingWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  closed = false;
  constructor(public url: string) {
    RecordingWebSocket.instances.push(this);
  }
  static find(part: string): RecordingWebSocket[] {
    return RecordingWebSocket.instances.filter((s) => s.url.includes(part));
  }
  open() {
    this.onopen?.();
  }
  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent);
  }
  drop() {
    this.onclose?.();
  }
  close() {
    this.closed = true;
    this.onclose?.();
  }
}
