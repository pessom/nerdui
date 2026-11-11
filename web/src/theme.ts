// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createTheme } from "@mantine/core";

/** Mantine theme (ADR-0010). Color scheme (auto/light/dark) is chosen by the user (#90). */
export const theme = createTheme({
  primaryColor: "teal",
  defaultRadius: "md",
  fontFamily:
    "Inter, system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
  fontFamilyMonospace: "'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace",
});
