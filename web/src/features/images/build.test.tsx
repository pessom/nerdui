// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../api/schema";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { parseArgs, parseTags } from "./build";

type Job = components["schemas"]["Job"];

describe("build helpers", () => {
  it("parses tags and build arguments", () => {
    expect(parseTags(" app:1, registry.local/app:latest\napp:dev ")).toEqual([
      "app:1",
      "registry.local/app:latest",
      "app:dev",
    ]);
    expect(parseArgs("VERSION=1.2\n# comment\n\nURL=http://x/?a=b")).toEqual({
      args: { VERSION: "1.2", URL: "http://x/?a=b" },
    });
    expect(parseArgs("VERSION=1\nnot an argument").bad).toBe("not an argument");
  });
});

describe("building images (#45)", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("builds from a Dockerfile as a job", async () => {
    const job: Job = {
      id: "b1",
      kind: "image.build",
      title: "Build app:1",
      environment: "local",
      namespace: "default",
      status: "running",
      startedAt: "2027-03-01T10:00:00Z",
    };
    let jobs: Job[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () =>
        json(200, { namespace: "default", images: [] }),
      "GET /api/v1/environments/local/images/builder": () =>
        json(200, { available: true, host: "unix:///run/buildkit/buildkitd.sock" }),
      "POST /api/v1/environments/local/images/build": () => {
        jobs = [job];
        return json(202, { job });
      },
      "GET /api/v1/jobs": () => json(200, { jobs }),
    });
    renderApp("/env/local/images");
    fireEvent.click(await screen.findByRole("button", { name: "Build" }));
    const dialog = await screen.findByRole("dialog", { name: "Build image" });
    const submit = within(dialog).getByRole("button", { name: "Build" });
    expect(submit).toBeDisabled();

    fireEvent.change(within(dialog).getByRole("textbox", { name: /^Dockerfile/ }), {
      target: { value: "FROM alpine:3\nRUN true\n" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Tags/), {
      target: { value: "app:1 app:latest" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Build arguments/), {
      target: { value: "nope" },
    });
    expect(await within(dialog).findByText("Not NAME=value: nope")).toBeInTheDocument();
    expect(submit).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText(/^Build arguments/), {
      target: { value: "VERSION=1" },
    });
    fireEvent.click(within(dialog).getByLabelText("Push after the build"));
    fireEvent.click(submit);

    expect(await screen.findByRole("dialog", { name: "Jobs" })).toBeInTheDocument();
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/images/build"));
    expect(post?.body).toEqual({
      source: "dockerfile",
      dockerfile: "FROM alpine:3\nRUN true\n",
      tags: ["app:1", "app:latest"],
      args: { VERSION: "1" },
      noCache: false,
      pull: false,
      push: true,
    });
    expect(post?.query.get("namespace")).toBe("default");
  });

  it("warns when BuildKit is not running", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/images": () =>
        json(200, { namespace: "default", images: [] }),
      "GET /api/v1/environments/local/images/builder": () =>
        json(200, { available: false, error: "buildkitd is not running (connection refused)" }),
    });
    renderApp("/env/local/images");
    fireEvent.click(await screen.findByRole("button", { name: "Build" }));
    const dialog = await screen.findByRole("dialog", { name: "Build image" });
    expect(await within(dialog).findByText("BuildKit is not available")).toBeInTheDocument();
    expect(within(dialog).getByText(/connection refused/)).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "How to set up BuildKit" })).toHaveAttribute(
      "href",
      "https://github.com/pessom/nerdui/blob/main/docs/image-builds.md#requirements",
    );

    // A Git repository needs its URL.
    fireEvent.click(within(dialog).getByText("Git repository"));
    expect(within(dialog).getByLabelText(/^Repository URL/)).toBeInTheDocument();
    await waitFor(() =>
      expect(within(dialog).getByRole("button", { name: "Build" })).toBeDisabled(),
    );
  });
});
