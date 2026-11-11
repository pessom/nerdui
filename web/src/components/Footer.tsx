// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Anchor, Group, Text } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { versionQuery } from "../api/queries";

/** Fallback used until /api/v1/version responds. */
export const DEFAULT_SOURCE_URL = "https://github.com/pessom/nerdui";

/**
 * Link to the source code of the running build.
 * Required by AGPL-3.0 section 13 — do not remove; forks must point it to their source.
 */
export function Footer() {
  const { data } = useQuery(versionQuery);
  const build = data?.nerdui;
  return (
    <Group justify="center" gap="xs" component="footer">
      <Anchor
        href={build?.source || DEFAULT_SOURCE_URL}
        target="_blank"
        rel="noopener noreferrer"
        size="xs"
      >
        Source code
      </Anchor>
      <Text size="xs" c="dimmed">
        ·
      </Text>
      <Anchor
        href="https://www.gnu.org/licenses/agpl-3.0.html"
        target="_blank"
        rel="noopener noreferrer"
        size="xs"
      >
        {build?.license || "AGPL-3.0-only"}
      </Anchor>
      {build && (
        <Text size="xs" c="dimmed">
          · nerdui {build.version}
        </Text>
      )}
    </Group>
  );
}
