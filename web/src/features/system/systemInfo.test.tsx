// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SystemInfo } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { cgroupLabel, countByStatus, filterPlugins, formatBytes } from "./info";

const info: SystemInfo = {
  id: "c0ffee",
  hostname: "box",
  operatingSystem: "Ubuntu 24.04.2 LTS",
  osType: "linux",
  architecture: "x86_64",
  kernelVersion: "6.8.0-60-generic",
  cpus: 8,
  memoryBytes: 16 * 1024 ** 3,
  cgroupDriver: "systemd",
  cgroupVersion: "2",
  rootless: false,
  snapshotter: "overlayfs",
  snapshotters: ["native", "overlayfs"],
  logDrivers: ["fluentd", "journald", "json-file", "none", "syslog"],
  securityOptions: ["apparmor", "seccomp"],
  features: {
    memoryLimit: true,
    swapLimit: false,
    cpuCfsPeriod: true,
    cpuCfsQuota: true,
    cpuShares: true,
    cpuSet: true,
    pidsLimit: true,
    oomKillDisable: false,
    ipv4Forwarding: true,
  },
  components: [
    { name: "containerd", version: "v2.4.2", commit: "abc" },
    { name: "runc", version: "1.3.0", commit: "v1.3.0-0-g4ca628d1" },
    { name: "buildctl", error: "buildctl not found in PATH" },
  ],
  cni: {
    path: "/opt/cni/bin",
    netConfPath: "/etc/cni/net.d",
    version: "v1.7.1",
    plugins: ["bridge"],
  },
  warnings: ["WARNING: No swap limit support"],
  deprecations: [{ id: "io.containerd.deprecation/x", message: "Old config in use" }],
  plugins: [
    {
      type: "io.containerd.snapshotter.v1",
      id: "overlayfs",
      status: "ok",
      platforms: ["linux/amd64"],
    },
    {
      type: "io.containerd.snapshotter.v1",
      id: "btrfs",
      status: "skip",
      error: "not btrfs: skip plugin",
    },
    { type: "io.containerd.grpc.v1", id: "cri", status: "error", error: "boom" },
  ],
};

describe("system info helpers", () => {
  it("formats values", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(16 * 1024 ** 3)).toBe("16.0 GiB");
    expect(formatBytes(1536 * 1024)).toBe("1.5 MiB");
    expect(cgroupLabel(info)).toBe("v2 (systemd)");
    expect(cgroupLabel({ cgroupVersion: "", cgroupDriver: "" })).toBe("—");
  });

  it("filters and counts plugins", () => {
    expect(countByStatus(info.plugins)).toEqual({ ok: 1, skip: 1, error: 1 });
    expect(filterPlugins(info.plugins, "SNAPSHOT", "all").map((p) => p.id)).toEqual([
      "overlayfs",
      "btrfs",
    ]);
    expect(filterPlugins(info.plugins, "", "error").map((p) => p.id)).toEqual(["cri"]);
    expect(filterPlugins(info.plugins, "btr", "ok")).toEqual([]);
  });
});

describe("overview host information", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("shows host, runtime, warnings and plugins", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/info": () => json(200, info),
    });
    renderApp("/env/local");

    const host = await screen.findByRole("region", { name: "Host" });
    expect(within(host).getByText("6.8.0-60-generic")).toBeInTheDocument();
    expect(within(host).getByText("16.0 GiB")).toBeInTheDocument();
    expect(within(host).getByText("v2 (systemd)")).toBeInTheDocument();
    expect(
      within(host)
        .getAllByTitle("not supported")
        .map((b) => b.textContent),
    ).toEqual(["Swap limit", "OOM kill disable"]);

    const runtime = screen.getByRole("region", { name: "Runtime" });
    expect(within(runtime).getByText("1.3.0")).toBeInTheDocument();
    expect(within(runtime).getByText(/buildctl not found/)).toBeInTheDocument();
    expect(within(runtime).getByText("/opt/cni/bin")).toBeInTheDocument();

    expect(screen.getByText("No swap limit support")).toBeInTheDocument();
    expect(screen.getByText(/Old config in use/)).toBeInTheDocument();

    const plugins = screen.getByRole("region", { name: "containerd plugins" });
    expect(within(plugins).getAllByRole("row")).toHaveLength(4); // header + 3
    fireEvent.click(within(plugins).getByText("error 1"));
    expect(within(plugins).getAllByRole("row")).toHaveLength(2);
    expect(within(plugins).getByText("boom")).toBeInTheDocument();

    fireEvent.click(within(plugins).getByText("All 3"));
    fireEvent.change(within(plugins).getByLabelText("Filter plugins"), {
      target: { value: "nothing" },
    });
    expect(within(plugins).getByText("No plugins match")).toBeInTheDocument();
  });
});
