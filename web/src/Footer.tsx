// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import type { components } from "./api/client";

/** Fallback used until /api/v1/version responds. */
export const DEFAULT_SOURCE_URL = "https://github.com/pessom/nerdui";

type BuildInfo = components["schemas"]["BuildInfo"];

/**
 * Footer with a link to the source code of the running build.
 * Required by AGPL-3.0 section 13 — do not remove; forks must point it to their source.
 */
export function Footer({ build }: { build?: BuildInfo }) {
  const source = build?.source || DEFAULT_SOURCE_URL;
  const license = build?.license || "AGPL-3.0-only";

  return (
    <footer>
      <a href={source} target="_blank" rel="noopener noreferrer">
        Source code
      </a>{" "}
      · Licensed under{" "}
      <a
        href="https://www.gnu.org/licenses/agpl-3.0.html"
        target="_blank"
        rel="noopener noreferrer"
      >
        {license}
      </a>
      {build && <> · {build.version}</>}
    </footer>
  );
}
