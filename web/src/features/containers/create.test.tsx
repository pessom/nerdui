// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Job } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { emptyForm, parseArgs, secretsMasked, toRequest } from "./create";

vi.mock("../jobs/JobTerminal", () => ({ default: () => <pre aria-label="Job output" /> }));

describe("create helpers", () => {
  it("parses commands like a shell's quoting", () => {
    expect(parseArgs(`sh -c "echo \\"hi\\" there" 'a b' c\\ d`).args).toEqual([
      "sh",
      "-c",
      'echo "hi" there',
      "a b",
      "c d",
    ]);
    expect(parseArgs(`  `).args).toEqual([]);
    expect(parseArgs(`echo ''`).args).toEqual(["echo", ""]);
    expect(parseArgs(`echo "x`).error).toMatch(/unterminated/);
  });

  it("builds the request and skips empty rows", () => {
    const { request } = toRequest({
      ...emptyForm,
      image: " nginx ",
      command: "nginx -g 'daemon off;'",
      ports: [
        { hostIp: "", hostPort: "8080", containerPort: "80", protocol: "tcp" },
        { hostIp: "", hostPort: "", containerPort: "", protocol: "tcp" },
      ],
      env: [
        { name: "A", value: "1", secret: true },
        { name: "", value: "x", secret: false },
      ],
      mounts: [{ type: "tmpfs", source: "", target: "/tmp", readOnly: false }],
      memoryMiB: "128",
      restart: "on-failure",
      retries: "3",
      labels: [{ key: "app", value: "web" }],
    });
    expect(request).toEqual({
      image: "nginx",
      start: true,
      command: ["nginx", "-g", "daemon off;"],
      ports: [{ hostPort: 8080, containerPort: 80, protocol: "tcp" }],
      env: [{ name: "A", value: "1" }],
      mounts: [{ type: "tmpfs", target: "/tmp" }],
      memoryBytes: 128 * 1024 * 1024,
      restart: "on-failure:3",
      labels: { app: "web" },
    });
    expect(secretsMasked("--env=A=s3cret x", [{ name: "A", value: "s3cret", secret: true }])).toBe(
      "--env=A=•••••• x",
    );
  });
});

describe("create container page", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("previews the command, creates a job and opens the container from it", async () => {
    const job: Job = {
      id: "cj1",
      kind: "container.create",
      title: "Create container web",
      environment: "local",
      namespace: "default",
      status: "running",
      startedAt: "2027-03-01T10:00:00Z",
    };
    let jobs: Job[] = [];
    const id = "e".repeat(64);
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/containers": () =>
        json(200, { namespace: "default", containers: [] }),
      "POST /api/v1/environments/local/containers/command": (c) => {
        const body = c.body as { image: string; env?: { value: string }[] };
        if (body.image === "--bad")
          return json(400, {
            error: { code: "invalid_argument", message: "invalid image reference" },
          });
        return json(200, {
          command: `nerdctl run -d --name=web --env=TOKEN=${body.env?.[0]?.value ?? ""} ${body.image}`,
        });
      },
      "POST /api/v1/environments/local/containers": () => {
        jobs = [job];
        return json(202, { job });
      },
      "GET /api/v1/jobs": () => json(200, { jobs }),
      [`GET /api/v1/environments/local/containers/${id}/inspect`]: () =>
        json(200, { Id: id, Name: "web", State: { Status: "running" } }),
    });
    const { router } = renderApp("/env/local/containers?ns=default");
    fireEvent.click(await screen.findByRole("button", { name: "New container" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/containers/new"));

    fireEvent.change(await screen.findByLabelText(/^Image/), { target: { value: "--bad" } });
    expect(await screen.findByText("invalid image reference")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create and start" })).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/^Image/), { target: { value: "nginx:1.27" } });
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "web" } });
    fireEvent.click(screen.getByRole("button", { name: "Add environment variable" }));
    fireEvent.change(screen.getByLabelText("Variable name 1"), { target: { value: "TOKEN" } });
    fireEvent.change(screen.getByLabelText("Variable value 1"), { target: { value: "s3cret" } });
    fireEvent.click(screen.getByLabelText("Secret"));
    expect(await screen.findByText(/--env=TOKEN=•••••• nginx:1.27/)).toBeInTheDocument();
    expect(screen.queryByText(/s3cret/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Create and start" }));
    const panel = await screen.findByRole("dialog", { name: "Jobs" });
    await waitFor(() => expect(router.state.location.pathname).toBe("/env/local/containers"));
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/containers"));
    expect(post?.body).toMatchObject({
      image: "nginx:1.27",
      name: "web",
      env: [{ name: "TOKEN", value: "s3cret" }],
      restart: "unless-stopped",
      start: true,
    });
    expect(post?.query.get("namespace")).toBe("default");

    // When the job has finished, its result links to the new container.
    jobs = [
      {
        ...job,
        status: "succeeded",
        finishedAt: "2027-03-01T10:00:05Z",
        result: { containerId: id },
      },
    ];
    const item = await within(panel).findByRole("region", { name: "Create container web" });
    fireEvent.click(
      await within(item).findByRole("button", { name: "Open container" }, { timeout: 5000 }),
    );
    await waitFor(() => expect(router.state.location.pathname).toBe(`/env/local/containers/${id}`));
  });
});
