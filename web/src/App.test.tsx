// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { DEFAULT_SOURCE_URL } from "./Footer";

const versionResponse = {
  nerdui: {
    version: "v0.1.0",
    commit: "abc",
    date: "now",
    goVersion: "go",
    platform: "linux/amd64",
    source: "https://example.com/fork/nerdui",
    license: "AGPL-3.0-only",
  },
  runtime: {
    containerd: "v2.4.2",
    containerdRevision: "rev",
    containerdSupported: true,
    nerdctlLibrary: "v2.4.2",
    nerdctl: { path: "nerdctl", version: "2.4.2", required: "2.4", compatible: true },
  },
};

describe("App", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("renders versions from the API", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(versionResponse)),
    );

    render(<App />);

    expect(screen.getByRole("heading", { name: "nerdui" })).toBeInTheDocument();
    expect(await screen.findByText("v0.1.0")).toBeInTheDocument();
    expect(screen.getAllByText("v2.4.2")).toHaveLength(2);
  });

  it("shows runtime error when containerd is unreachable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          ...versionResponse,
          runtime: { ...versionResponse.runtime, containerd: "" },
          error: "connection refused",
        }),
      ),
    );

    render(<App />);

    expect(await screen.findByText("not connected")).toBeInTheDocument();
    expect(screen.getByText(/connection refused/)).toBeInTheDocument();
  });

  it("warns about read-only mode and unsupported containerd", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          ...versionResponse,
          runtime: {
            ...versionResponse.runtime,
            containerd: "v2.0.14",
            containerdSupported: false,
            nerdctl: { path: "nerdctl", version: "2.3.5", required: "2.4", compatible: false },
          },
        }),
      ),
    );

    render(<App />);

    expect(await screen.findByText(/Read-only mode: host nerdctl 2.3.5/)).toBeInTheDocument();
    expect(screen.getByText(/containerd v2.0.14 is not a supported version/)).toBeInTheDocument();
  });

  it("shows no runtime warnings for a supported setup", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(versionResponse)),
    );

    render(<App />);

    await screen.findByText("v0.1.0");
    expect(screen.queryByText(/Read-only mode/)).not.toBeInTheDocument();
    expect(screen.queryByText(/not a supported version/)).not.toBeInTheDocument();
  });

  it("hides runtime details for anonymous users", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ nerdui: versionResponse.nerdui })),
    );

    render(<App />);

    expect(await screen.findByText("v0.1.0")).toBeInTheDocument();
    expect(screen.getByText(/Sign in to see runtime details/)).toBeInTheDocument();
    expect(screen.queryByText("containerd")).not.toBeInTheDocument();
  });

  it("links to the source code of the running build (AGPL-3.0 section 13)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(versionResponse)),
    );

    render(<App />);

    const link = screen.getByRole("link", { name: "Source code" });
    expect(link).toHaveAttribute("href", DEFAULT_SOURCE_URL);
    await screen.findByText("v0.1.0");
    expect(link).toHaveAttribute("href", versionResponse.nerdui.source);
  });
});
