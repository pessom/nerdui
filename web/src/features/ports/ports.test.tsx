// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp, signedIn } from "../../test/render";
import { fakeApi, json } from "../../test/server";

const ports = [
  {
    namespace: "default",
    containerId: "a".repeat(64),
    containerName: "web",
    state: "running",
    hostPort: 8080,
    containerPort: 80,
    protocol: "tcp",
    published: true,
    conflicts: ["host process on 0.0.0.0:8080/tcp"],
  },
  {
    namespace: "k8s.io",
    containerId: "b".repeat(64),
    containerName: "dns",
    state: "exited",
    hostIp: "::1",
    hostPort: 53,
    containerPort: 53,
    protocol: "udp",
    published: true,
  },
  {
    namespace: "default",
    containerId: "c".repeat(64),
    containerName: "db",
    state: "running",
    containerPort: 5432,
    protocol: "tcp",
    published: false,
  },
];

describe("ports", () => {
  it("lists ports of all namespaces with conflicts and filters them", async () => {
    fakeApi({
      ...signedIn,
      "GET /api/v1/environments/local/ports": () => json(200, { hostChecked: false, ports }),
    });
    renderApp("/env/local/ports");

    const table = await screen.findByRole("table");
    expect(within(table).getByText("0.0.0.0:8080")).toBeInTheDocument();
    expect(within(table).getByText("[::1]:53")).toBeInTheDocument();
    expect(within(table).getByText("exposed")).toBeInTheDocument();
    expect(within(table).getByText("host process on 0.0.0.0:8080/tcp")).toBeInTheDocument();
    expect(within(table).getByRole("link", { name: "dns" })).toHaveAttribute(
      "href",
      `/env/local/containers/${"b".repeat(64)}?ns=k8s.io`,
    );
    expect(screen.getByText("Host processes were not checked")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("radio", { name: "Conflicts (1)" }));
    expect(within(table).queryByText("dns")).not.toBeInTheDocument();
    expect(within(table).getByText("web")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Published" }));
    expect(within(table).queryByText("exposed")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ports" })).toHaveAttribute("data-active", "true");
  });
});
