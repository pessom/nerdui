// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Job } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";

vi.mock("./JobTerminal", () => ({
  default: ({ jobId }: { jobId: string }) => <pre aria-label="Job output">output of {jobId}</pre>,
}));

const running: Job = {
  id: "j1",
  kind: "image.pull",
  title: "Pull nginx:1.27",
  environment: "local",
  namespace: "default",
  status: "running",
  startedAt: "2027-03-01T10:00:00Z",
  user: "admin",
};

describe("jobs", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", SilentWebSocket);
    localStorage.clear();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("pulls an image as a job and shows it in the panel", async () => {
    let jobs: Job[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () =>
        json(200, { namespace: "default", images: [] }),
      "POST /api/v1/environments/local/images/pull": () => {
        jobs = [running];
        return json(202, { job: running });
      },
      "GET /api/v1/jobs": () => json(200, { jobs }),
      "POST /api/v1/jobs/j1/cancel": () => {
        jobs = [{ ...running, status: "canceled", finishedAt: "2027-03-01T10:01:00Z" }];
        return json(202, jobs[0]);
      },
    });
    renderApp("/env/local/images?ns=default");

    fireEvent.click(await screen.findByRole("button", { name: "Pull image" }));
    const dialog = await screen.findByRole("dialog", { name: "Pull image" });
    fireEvent.change(within(dialog).getByLabelText(/^Image/), {
      target: { value: " nginx:1.27 " },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Pull" }));

    const panel = await screen.findByRole("dialog", { name: "Jobs" });
    const item = await within(panel).findByRole("region", { name: "Pull nginx:1.27" });
    expect(within(item).getByText("running")).toBeInTheDocument();
    // The started job is expanded with its output.
    expect(await within(item).findByLabelText("Job output")).toHaveTextContent("output of j1");
    expect(calls.find((c) => c.path.endsWith("/images/pull"))?.body).toEqual({
      reference: "nginx:1.27",
    });
    expect(calls.find((c) => c.path.endsWith("/images/pull"))?.query.get("namespace")).toBe(
      "default",
    );
    // The header shows the running job.
    expect(screen.getByRole("button", { name: "Jobs" })).toBeInTheDocument();

    // The output can be opened full screen.
    fireEvent.click(within(item).getByRole("button", { name: "Full screen output" }));
    const full = await screen.findByRole("dialog", { name: "Pull nginx:1.27" });
    expect(within(full).getByLabelText("Job output")).toHaveTextContent("output of j1");
    fireEvent.keyDown(full, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Pull nginx:1.27" })).not.toBeInTheDocument(),
    );
    // Escape closes only the full-screen output; the panel stays open with the inline output.
    expect(screen.getByRole("dialog", { name: "Jobs" })).toBeInTheDocument();
    expect(item.isConnected).toBe(true);
    expect(await within(item).findByLabelText("Job output")).toBeInTheDocument();

    fireEvent.click(within(item).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(within(item).getByText("canceled")).toBeInTheDocument());
  });

  it("pulls all platforms and reports errors", async () => {
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () =>
        json(200, { namespace: "default", images: [] }),
      "POST /api/v1/environments/local/images/pull": () =>
        json(409, { error: { code: "conflict", message: "image is busy with job j1" } }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/images");
    fireEvent.click(await screen.findByRole("button", { name: "Pull image" }));
    const dialog = await screen.findByRole("dialog", { name: "Pull image" });
    fireEvent.change(within(dialog).getByLabelText(/^Image/), { target: { value: "alpine" } });
    fireEvent.click(within(dialog).getByLabelText("Platform", { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: /All platforms/, hidden: true }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Pull" }));
    expect(await within(dialog).findByText(/busy with job/)).toBeInTheDocument();
    expect(calls.find((c) => c.path.endsWith("/images/pull"))?.body).toEqual({
      reference: "alpine",
      allPlatforms: true,
    });
  });

  it("disables pulls in read-only mode", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local": () =>
        json(200, {
          id: "local",
          name: "Local host",
          kind: "local",
          defaultNamespace: "default",
          status: "connected",
          runtime: {
            containerd: "v2.4.2",
            containerdRevision: "r",
            containerdSupported: true,
            supportedContainerd: ["2.3", "2.4"],
            nerdctlLibrary: "v2.4.2",
            nerdctl: { path: "nerdctl", version: "2.3.5", required: "2.4", compatible: false },
          },
        }),
      "GET /api/v1/environments/local/images": () =>
        json(200, { namespace: "default", images: [] }),
      "GET /api/v1/jobs": () => json(200, { jobs: [] }),
    });
    renderApp("/env/local/images");
    await waitFor(() => expect(screen.getByRole("button", { name: "Pull image" })).toBeDisabled());
  });
});
