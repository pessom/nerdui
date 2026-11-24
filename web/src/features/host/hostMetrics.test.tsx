// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

vi.mock("../containers/StatsChart", () => ({
  default: ({ label, series }: { label: string; series: { label: string }[] }) => (
    <div role="img" aria-label={label}>
      {series.map((s) => s.label).join(",")}
    </div>
  ),
}));

const current = {
  time: "2027-03-01T10:00:00Z",
  cpu: 42.5,
  cpus: 4,
  load1: 1.25,
  memoryUsed: 3 * 1024 ** 3,
  memoryTotal: 4 * 1024 ** 3,
  swapUsed: 0,
  swapTotal: 0,
  netRx: 2048,
  netTx: 1024,
  disks: [
    { path: "/", paths: ["/", "/var/lib/nerdui"], total: 100 * 1024 ** 3, used: 95 * 1024 ** 3 },
  ],
};

describe("host usage", () => {
  it("shows current usage and charts, and switches the range", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/host/metrics": () =>
        json(200, {
          current,
          range: "1h",
          intervalSeconds: 15,
          points: [{ t: 1, cpu: 40, memory: 75, netRx: 1, netTx: 2, disks: { "/": 95 } }],
        }),
    });
    renderApp("/env/local");
    const section = await screen.findByRole("region", { name: "Host usage" });
    expect(await within(section).findByLabelText("CPU")).toHaveTextContent("42.5 %");
    expect(within(section).getByLabelText("Memory")).toHaveTextContent("3.0 GiB of 4.0 GiB");
    expect(within(section).getByLabelText("Disk /")).toHaveTextContent("95.0 %");
    expect(within(section).getByLabelText("Network")).toHaveTextContent("2.0 KiB/s");
    expect(await within(section).findByRole("img", { name: "Disks chart" })).toHaveTextContent("/");
    fireEvent.click(within(section).getByText("24h"));
    await waitFor(() => expect(calls.some((c) => c.query.get("range") === "24h")).toBe(true));
  });

  it("edits the alert thresholds", async () => {
    const alerts = {
      cpu: { enabled: false, percent: 90, minutes: 5 },
      memory: { enabled: false, percent: 90, minutes: 5 },
      disk: { enabled: false, percent: 90, minutes: 0 },
    };
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/settings/host-alerts": () => json(200, alerts),
      "PUT /api/v1/settings/host-alerts": (c) => json(200, c.body as object),
    });
    renderApp("/env/local/settings");
    const card = await screen.findByRole("region", { name: "Host alerts" });
    fireEvent.click(await within(card).findByLabelText("Each disk"));
    fireEvent.change(within(card).getByLabelText("Each disk threshold"), {
      target: { value: "85" },
    });
    fireEvent.click(within(card).getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PUT")?.body).toEqual({
        ...alerts,
        disk: { enabled: true, percent: 85, minutes: 0 },
      }),
    );
  });
});
