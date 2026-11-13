// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { act, fireEvent, screen, within } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, envStatus, fakeApi, json } from "../../test/server";
import { execPath, splitCommand } from "./exec";
import type { ExecState } from "./ExecTerminal";

const states: ((s: ExecState) => void)[] = [];
vi.mock("./ExecTerminal", () => ({
  default: ({ path, onState }: { path: string; onState: (s: ExecState) => void }) => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    useEffect(() => {
      states.push(onState);
      onState({ status: "open" });
    }, [onState]);
    return <pre aria-label="Terminal session">{path}</pre>;
  },
}));

describe("exec helpers", () => {
  it("builds the WebSocket path", () => {
    expect(splitCommand("  psql  -U postgres ")).toEqual(["psql", "-U", "postgres"]);
    const p = execPath("local", "abc", "default", {
      cmd: ["sh", "-l"],
      user: "1000:1000",
      workdir: "/srv",
    });
    const url = new URL(p, "http://x");
    expect(url.pathname).toBe("/api/v1/environments/local/containers/abc/exec");
    expect(url.searchParams.getAll("cmd")).toEqual(["sh", "-l"]);
    expect(url.searchParams.get("user")).toBe("1000:1000");
    expect(url.searchParams.get("workdir")).toBe("/srv");
  });
});

const id = "d".repeat(64);
const base = "/api/v1/environments/local/containers";

function setup(state: string, exec = true) {
  fakeApi({
    ...signedIn,
    "GET /api/v1/environments/local": () => json(200, { ...envStatus, features: { exec } }),
    [`GET ${base}`]: () =>
      json(200, {
        namespace: "default",
        containers: [
          {
            id,
            name: "app",
            namespace: "default",
            image: "alpine:3",
            command: "sleep",
            state,
            createdAt: "2027-03-01T10:00:00Z",
            ports: [],
            labels: {},
          },
        ],
      }),
    [`GET ${base}/${id}/inspect`]: () =>
      json(200, { Id: id, Name: "app", State: { Status: state } }),
  });
}

describe("terminal tab", () => {
  beforeEach(() => {
    states.length = 0;
    vi.stubGlobal("WebSocket", SilentWebSocket);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("connects with the chosen command, user and directory, then disconnects", async () => {
    setup("running");
    renderApp(`/env/local/containers/${id}?tab=terminal`);
    fireEvent.click(await screen.findByLabelText("Command", { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: "Custom command…", hidden: true }));
    fireEvent.change(screen.getByLabelText("Custom command"), {
      target: { value: "psql -U postgres" },
    });
    fireEvent.change(screen.getByLabelText("User"), { target: { value: "postgres" } });
    fireEvent.change(screen.getByLabelText("Working directory"), { target: { value: "/tmp" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    const term = await screen.findByLabelText("Terminal session", { selector: "pre" });
    const url = new URL(term.textContent ?? "", "http://x");
    expect(url.searchParams.getAll("cmd")).toEqual(["psql", "-U", "postgres"]);
    expect(url.searchParams.get("user")).toBe("postgres");
    expect(url.searchParams.get("workdir")).toBe("/tmp");
    expect(url.searchParams.get("namespace")).toBe("default");
    expect(await screen.findByText("open")).toBeInTheDocument();

    act(() => states.at(-1)?.({ status: "exited", exitCode: 2 }));
    expect(screen.getByText("exited (2)")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "New session" }));
    expect(await screen.findByText("open")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(screen.queryByLabelText("Terminal session")).not.toBeInTheDocument();
    expect(screen.getByText("Choose a command and connect.")).toBeInTheDocument();
  });

  it("explains disabled terminals and stopped containers", async () => {
    setup("running", false);
    renderApp(`/env/local/containers/${id}?tab=terminal`);
    expect(await screen.findByText("Terminal disabled")).toBeInTheDocument();
  });

  it("needs a running container", async () => {
    setup("exited");
    renderApp(`/env/local/containers/${id}?tab=terminal`);
    const tab = await screen.findByRole("tabpanel");
    expect(await within(tab).findByText("Container is not running")).toBeInTheDocument();
  });
});
