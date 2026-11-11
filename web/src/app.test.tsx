// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createMemoryHistory, createRouter } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setCsrfToken } from "./api/client";
import { App } from "./App";
import { DEFAULT_SOURCE_URL } from "./components/Footer";
import { createQueryClient } from "./router";
import { routeTree } from "./routeTree.gen";
import {
  SilentWebSocket,
  envStatus,
  fakeApi,
  json,
  runtime,
  session,
  unauthenticated,
  version,
} from "./test/server";

function renderApp(path: string) {
  const queryClient = createQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false } });
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(<App queryClient={queryClient} router={router} />);
  return router;
}

const common = {
  "GET /api/v1/version": () => json(200, version),
  "GET /api/v1/environments": () =>
    json(200, {
      environments: [
        { id: "local", name: "Local host", kind: "local", defaultNamespace: "default" },
      ],
    }),
  "GET /api/v1/environments/local": () => json(200, envStatus),
  "GET /api/v1/environments/local/namespaces": () =>
    json(200, { namespaces: ["default", "k8s.io"], default: "default" }),
};

describe("nerdui app shell", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    setCsrfToken("");
  });
  afterEach(() => vi.unstubAllGlobals());

  it("redirects to setup on a fresh install and creates the admin", async () => {
    let setupDone = false;
    const { calls } = fakeApi({
      ...common,
      "GET /api/v1/auth/me": () => (setupDone ? json(200, session) : unauthenticated()),
      "GET /api/v1/setup": () => json(200, { required: !setupDone }),
      "POST /api/v1/setup": () => {
        setupDone = true;
        return json(201, session);
      },
    });
    const router = renderApp("/");

    expect(await screen.findByRole("heading", { name: "Set up nerdui" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Setup token/), { target: { value: " tok " } });
    fireEvent.change(screen.getByLabelText(/^Password/), { target: { value: "short" } });
    expect(await screen.findByText(/Use at least 12 characters/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/^Password/), {
      target: { value: "correct horse battery" },
    });
    fireEvent.change(screen.getByLabelText(/Repeat password/), {
      target: { value: "correct horse battery" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create admin account" }));

    expect(await screen.findByRole("heading", { name: "Local host" })).toBeInTheDocument();
    const post = calls.find((c) => c.method === "POST" && c.path === "/api/v1/setup");
    expect(post?.body).toEqual({
      token: "tok",
      username: "admin",
      password: "correct horse battery",
    });
    expect(router.state.location.pathname).toBe("/env/local");
  });

  it("redirects to login, signs in and returns to the requested page", async () => {
    let loggedIn = false;
    fakeApi({
      ...common,
      "GET /api/v1/auth/me": () => (loggedIn ? json(200, session) : unauthenticated()),
      "GET /api/v1/setup": () => json(200, { required: false }),
      "POST /api/v1/auth/login": (c) => {
        const body = c.body as { password: string };
        if (body.password !== "correct horse battery") {
          return json(401, { error: { code: "unauthenticated", message: "invalid" } });
        }
        loggedIn = true;
        return json(200, session);
      },
    });
    const router = renderApp("/env/local?ns=k8s.io");

    expect(await screen.findByRole("heading", { name: "Sign in to nerdui" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Username/), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/Password/), { target: { value: "wrong password!" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Invalid username or password.")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/Password/), {
      target: { value: "correct horse battery" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("heading", { name: "Local host" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/env/local");
    expect(router.state.location.search).toEqual({ ns: "k8s.io" });
  });

  it("renders the overview and signs out with the CSRF token", async () => {
    const { calls } = fakeApi({
      ...common,
      "GET /api/v1/auth/me": () => json(200, session),
      "GET /api/v1/setup": () => json(200, { required: false }),
      "POST /api/v1/auth/logout": () => new Response(null, { status: 204 }),
    });
    const router = renderApp("/");

    expect(await screen.findByRole("heading", { name: "Local host" })).toBeInTheDocument();
    expect(screen.getByText("connected")).toBeInTheDocument();
    expect(screen.getByText("read-write")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("data-active", "true");
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "Source code" })).toHaveAttribute(
        "href",
        version.nerdui.source,
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "admin" }));
    fireEvent.click(await screen.findByText("Sign out"));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    const logout = calls.find((c) => c.path === "/api/v1/auth/logout");
    expect(logout?.headers.get("X-CSRF-Token")).toBe("csrf-123");
  });

  it("shows read-only and unsupported containerd banners", async () => {
    fakeApi({
      ...common,
      "GET /api/v1/auth/me": () => json(200, session),
      "GET /api/v1/environments/local": () =>
        json(200, {
          ...envStatus,
          runtime: {
            ...runtime,
            containerd: "v2.0.14",
            containerdSupported: false,
            nerdctl: { path: "nerdctl", version: "2.3.5", required: "2.4", compatible: false },
          },
        }),
    });
    renderApp("/env/local");

    expect(await screen.findByText("Read-only mode")).toBeInTheDocument();
    expect(screen.getByText(/Host nerdctl 2.3.5 is incompatible/)).toBeInTheDocument();
    expect(screen.getByText("Unsupported containerd version")).toBeInTheDocument();
    expect(screen.getByText("read-only")).toBeInTheDocument();
  });

  it("shows a banner when containerd is unreachable", async () => {
    fakeApi({
      ...common,
      "GET /api/v1/auth/me": () => json(200, session),
      "GET /api/v1/environments/local": () =>
        json(200, { ...envStatus, status: "unavailable", error: "containerd is unavailable" }),
      "GET /api/v1/environments/local/namespaces": () =>
        json(503, { error: { code: "unavailable", message: "containerd is unavailable" } }),
    });
    renderApp("/env/local");
    expect(await screen.findByText("containerd is not reachable")).toBeInTheDocument();
  });

  it("falls back to the default source link before the version is known", async () => {
    fakeApi({
      "GET /api/v1/auth/me": () => unauthenticated(),
      "GET /api/v1/setup": () => json(200, { required: false }),
      "GET /api/v1/version": () => new Promise<Response>(() => {}), // never resolves
    });
    renderApp("/login");
    expect(await screen.findByRole("link", { name: "Source code" })).toHaveAttribute(
      "href",
      DEFAULT_SOURCE_URL,
    );
  });
});
