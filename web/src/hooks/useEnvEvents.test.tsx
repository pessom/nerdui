// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { StreamHandlers, openStream } from "../lib/ws";
import { INVALIDATE_DEBOUNCE_MS, useEnvEvents, type RuntimeEvent } from "./useEnvEvents";

function setup(env = "local", ns = "default") {
  const queryClient = new QueryClient();
  const invalidate = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue();
  let handlers: StreamHandlers<RuntimeEvent> | undefined;
  const close = vi.fn();
  const paths: string[] = [];
  const fakeOpen = ((path: string, h: StreamHandlers<RuntimeEvent>) => {
    paths.push(path);
    handlers = h;
    return { close };
  }) as unknown as typeof openStream;
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const hook = renderHook(({ e, n }) => useEnvEvents(e, n, fakeOpen), {
    wrapper,
    initialProps: { e: env, n: ns },
  });
  const emit = (ev: Partial<RuntimeEvent>) =>
    act(() =>
      handlers!.onData({ ts: "t", topic: "/x", kind: "container", action: "create", ...ev }),
    );
  return { hook, invalidate, emit, close, paths, handlers: () => handlers! };
}

describe("useEnvEvents", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("subscribes to the environment's namespace", () => {
    const { paths } = setup("local", "k8s.io");
    expect(paths).toEqual(["/api/v1/environments/local/events/stream?namespace=k8s.io"]);
  });

  it("refetches immediately on connected and tracks the containerd state", () => {
    const { hook, invalidate, emit } = setup();
    emit({ kind: "nerdui", action: "disconnected", topic: "/nerdui/disconnected" });
    expect(hook.result.current.containerd).toBe("disconnected");
    expect(invalidate).not.toHaveBeenCalled();

    emit({ kind: "nerdui", action: "connected", topic: "/nerdui/connected" });
    act(() => vi.advanceTimersByTime(0));
    expect(hook.result.current.containerd).toBe("connected");
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["env", "local"] });
  });

  it("debounces bursts of runtime events into one invalidation", () => {
    const { invalidate, emit } = setup();
    for (let i = 0; i < 20; i++) emit({ topic: "/containers/create" });
    act(() => vi.advanceTimersByTime(INVALIDATE_DEBOUNCE_MS - 1));
    expect(invalidate).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("reports stream status and closes on unmount or namespace change", () => {
    const { hook, close, paths, handlers } = setup();
    act(() => handlers().onStatus?.("open"));
    expect(hook.result.current.stream).toBe("open");
    hook.rerender({ e: "local", n: "k8s.io" });
    expect(close).toHaveBeenCalledTimes(1);
    expect(paths).toHaveLength(2);
    hook.unmount();
    expect(close).toHaveBeenCalledTimes(2);
  });
});
