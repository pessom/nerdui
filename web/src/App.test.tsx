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
  runtime: { containerd: "v2.4.2", containerdRevision: "rev", nerdctlLibrary: "v2.4.2" },
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
