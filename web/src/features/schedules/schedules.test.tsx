// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";
import { describeCron, describeTask, formatSeconds, type Schedule } from "./kinds";

const prune: Schedule = {
  id: 1,
  name: "Weekly prune",
  kind: "prune",
  environment: "local",
  namespace: "default",
  options: { images: true, allImages: true, containers: true },
  cron: "0 4 * * 0",
  enabled: true,
  createdAt: "2027-03-01T10:00:00Z",
  updatedAt: "2027-03-01T10:00:00Z",
  nextRunAt: "2027-03-07T04:00:00Z",
  lastRunAt: "2027-02-28T04:00:00Z",
  lastStatus: "failed",
  lastMessage: "read-only mode",
};

describe("schedule helpers", () => {
  it("describes cron expressions and tasks", () => {
    expect(describeCron("0 3 * * *")).toBe("At 03:00");
    expect(describeCron("@daily")).toBe("At 00:00");
    expect(describeCron("@every 6h")).toBe("Every 6h");
    expect(describeCron("every day")).toBeUndefined();
    expect(describeTask(prune)).toBe("stopped containers, unused images in local / default");
    expect(formatSeconds(300)).toBe("5 minutes");
    expect(formatSeconds(86400)).toBe("1 day");
  });
});

describe("schedules page", () => {
  it("lists, creates and runs schedules", async () => {
    let list: Schedule[] = [prune];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/schedules": () => json(200, { schedules: list }),
      "GET /api/v1/environments": () =>
        json(200, { environments: [{ id: "local", name: "Local", kind: "local" }] }),
      "GET /api/v1/environments/local/stacks": () =>
        json(200, {
          namespace: "default",
          stacks: [
            {
              name: "demo",
              status: "running",
              services: [],
              containers: 0,
              running: 0,
              git: {
                pollInterval: 300,
                autoUpdate: "notify",
                url: "x",
                ref: "",
                fullRef: "",
                composePath: "",
                commit: { hash: "a", message: "" },
                pulledAt: "2027-03-01T10:00:00Z",
                webhook: false,
              },
            },
          ],
        }),
      "POST /api/v1/schedules": (c) => {
        const s = {
          ...prune,
          ...(c.body as object),
          id: 2,
          lastRunAt: undefined,
          lastStatus: "",
        } as Schedule;
        list = [...list, s];
        return json(201, s);
      },
      "POST /api/v1/schedules/1/run": () =>
        json(202, {
          job: {
            id: "j1",
            kind: "system.prune",
            title: "Prune",
            environment: "local",
            status: "running",
            startedAt: "2027-03-01T10:00:00Z",
          },
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/schedules");
    const table = await screen.findByRole("table", { name: "Schedules" });
    expect(within(table).getByText("Weekly prune")).toBeInTheDocument();
    expect(within(table).getByText("At 04:00, only on Sunday")).toBeInTheDocument();
    expect(within(table).getByText("failed")).toBeInTheDocument();
    const git = screen.getByRole("region", { name: "Git polling" });
    expect(await within(git).findByText("every 5 minutes")).toBeInTheDocument();

    fireEvent.click(within(table).getByRole("button", { name: "Actions for Weekly prune" }));
    const menu = await screen.findByRole("menu", { hidden: true });
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Run now", hidden: true }));
    await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/schedules/1/run")).toBe(true));

    fireEvent.click(screen.getByRole("button", { name: "New schedule" }));
    const dialog = await screen.findByRole("dialog", { name: "New schedule" });
    fireEvent.change(within(dialog).getByLabelText(/^Name\W*$/), { target: { value: "Nightly" } });
    fireEvent.change(within(dialog).getByLabelText(/^Cron\W*$/), { target: { value: "bad" } });
    expect(await within(dialog).findByText("Not a valid cron expression")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText(/^Cron\W*$/), {
      target: { value: "30 2 * * *" },
    });
    expect(within(dialog).getByText("At 02:30")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(
        calls.find((c) => c.method === "POST" && c.path === "/api/v1/schedules")?.body,
      ).toMatchObject({
        name: "Nightly",
        kind: "prune",
        environment: "local",
        namespace: "default",
        cron: "30 2 * * *",
        options: { images: true },
      }),
    );
    expect(await within(table).findByText("Nightly")).toBeInTheDocument();
  });
});
