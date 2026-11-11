import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const versionResponse = {
  nerdui: {
    version: "v0.1.0",
    commit: "abc",
    date: "now",
    goVersion: "go",
    platform: "linux/amd64",
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
});
