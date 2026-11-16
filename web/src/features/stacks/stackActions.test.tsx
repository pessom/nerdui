// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Job } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

vi.mock("../jobs/JobTerminal", () => ({
  default: ({ jobId }: { jobId: string }) => <pre aria-label="Job output">output of {jobId}</pre>,
}));

const base = "/api/v1/environments/local/stacks";

const app = {
  name: "app",
  mode: "managed",
  path: "/var/lib/nerdui/stacks/app",
  composeFile: "compose.yaml",
  status: "running",
  containers: 1,
  running: 1,
  services: [
    {
      name: "web",
      defined: true,
      running: 1,
      containers: [{ id: "a".repeat(64), name: "app-web-1", state: "running" }],
    },
  ],
};

const job = (id: string, title: string): Job => ({
  id,
  kind: "stack.up",
  title,
  environment: "local",
  namespace: "default",
  status: "running",
  startedAt: "2027-03-01T10:00:00Z",
  user: "admin",
});

describe("stack operations", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("deploys as a job shown in the Jobs panel and asks for the name to remove volumes", async () => {
    const started: Job[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}/app`]: () => json(200, app),
      "GET /api/v1/jobs": () => json(200, { jobs: started }),
      [`POST ${base}/app/up`]: () => {
        started.push(job("j1", "Deploy stack app"));
        return json(202, { job: started[0] });
      },
      [`POST ${base}/app/down`]: () => {
        started.push(job("j2", "Down (removing volumes) stack app"));
        return json(202, { job: started[1] });
      },
    });
    renderApp("/env/local/stacks/app");

    fireEvent.click(await screen.findByRole("button", { name: "Deploy" }));
    const panel = await screen.findByRole("dialog", { name: "Jobs" });
    const item = await within(panel).findByRole("region", { name: "Deploy stack app" });
    expect(await within(item).findByLabelText("Job output")).toHaveTextContent("output of j1");
    const up = calls.find((c) => c.path === `${base}/app/up`);
    expect(up?.body).toEqual({});
    expect(up?.query.get("namespace")).toBe("default");
    fireEvent.keyDown(panel, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Jobs" })).not.toBeInTheDocument(),
    );

    // Running stack: Stop instead of Start.
    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Start" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Down" }));
    const down = await screen.findByRole("dialog", { name: "Down stack" });
    fireEvent.click(within(down).getByLabelText("Remove the stack's volumes"));
    const confirm = within(down).getByRole("button", { name: "Down and remove volumes" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(down).getByLabelText("Type app to confirm"), {
      target: { value: "app" },
    });
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(calls.find((c) => c.path === `${base}/app/down`)?.body).toEqual({
        volumes: true,
        confirm: "app",
      }),
    );
    expect(
      await within(await screen.findByRole("dialog", { name: "Jobs" })).findByRole("region", {
        name: "Down (removing volumes) stack app",
      }),
    ).toBeInTheDocument();
  });

  it("offers no operations for an unmanaged project", async () => {
    fakeApi({
      ...signedIn,
      [`GET ${base}/manual`]: () =>
        json(200, {
          name: "manual",
          mode: "unmanaged",
          status: "unknown",
          containers: 1,
          running: 1,
          services: [],
        }),
    });
    renderApp("/env/local/stacks/manual");
    expect(await screen.findByRole("button", { name: "Adopt" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Deploy" })).not.toBeInTheDocument();
  });
});
