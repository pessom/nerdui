// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from "vitest";
import { absoluteURL, basePath, withBase } from "./base";
import { streamURL } from "./ws";

describe("base path", () => {
  it("is empty without a <base> prefix", () => {
    expect(basePath).toBe("");
    expect(withBase("/api/v1/version")).toBe("/api/v1/version");
    expect(absoluteURL("/api/v1/hooks/x")).toBe(`${window.location.origin}/api/v1/hooks/x`);
    expect(streamURL("/api/v1/x", { protocol: "https:", host: "h:1" })).toBe("wss://h:1/api/v1/x");
  });
});
