// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

// NERDUI_API_URL points the dev server at a running nerdui backend,
// e.g. NERDUI_API_URL=https://my-server:9443 npm run dev
// `secure: false` accepts nerdui's self-signed development certificate.
const apiTarget = process.env.NERDUI_API_URL ?? "https://localhost:9443";

export default defineConfig({
  // The router plugin generates src/routeTree.gen.ts from src/routes and splits each route
  // into its own chunk (ADR-0010). It must run before the React plugin.
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react()],
  server: {
    proxy: {
      // nerdui rejects requests whose Origin does not match the Host (CSRF protection for
      // POST and WebSocket). The dev server is a different origin, so the proxy presents the
      // backend's origin instead (development only).
      "/api": {
        target: apiTarget,
        changeOrigin: true,
        ws: true,
        secure: false,
        rewriteWsOrigin: true,
        configure: (proxy) => {
          proxy.on("proxyReq", (req) => {
            if (req.getHeader("origin")) req.setHeader("origin", new URL(apiTarget).origin);
          });
        },
      },
      "/healthz": { target: apiTarget, changeOrigin: true, secure: false },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
