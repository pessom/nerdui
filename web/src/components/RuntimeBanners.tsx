// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Stack } from "@mantine/core";
import { IconAlertTriangle, IconLock, IconPlugConnectedX } from "@tabler/icons-react";
import type { components } from "../api/client";

type RuntimeVersions = components["schemas"]["RuntimeVersions"];

/** "2.3.5" → "2.3"; empty for unknown versions. */
function hostMinor(version: string | undefined): string {
  return /^(\d+\.\d+)\./.exec(version ?? "")?.[1] ?? "";
}

/** Warnings for the runtime support policy (ADR-0011, ADR-0012, ADR-0017) and connectivity. */
export function RuntimeBanners({
  runtime,
  containerdDown,
}: {
  runtime?: RuntimeVersions;
  containerdDown: boolean;
}) {
  const n = runtime?.nerdctl;
  const readOnly = n && !n.compatible && !n.checkSkipped;
  const skipped = n && n.checkSkipped && !n.compatible;
  const unsupported = runtime?.containerd && !runtime.containerdSupported;
  if (!containerdDown && !readOnly && !skipped && !unsupported) return null;
  return (
    <Stack gap="xs" mb="md">
      {containerdDown && (
        <Alert color="red" icon={<IconPlugConnectedX />} title="containerd is not reachable">
          nerdui keeps retrying; data shown may be outdated.
        </Alert>
      )}
      {readOnly && (
        <Alert color="orange" icon={<IconLock />} title="Read-only mode">
          Host nerdctl {n.version || "was not found"}
          {n.version ? " is incompatible" : ""}; this nerdui build requires nerdctl {n.required}.x.
          Changes are disabled until it is installed
          {hostMinor(n.version) && (
            <>
              , or use the nerdui build for nerdctl {hostMinor(n.version)} (release file names and
              image tags contain <code>nerdctl{hostMinor(n.version)}</code>) if it is published
            </>
          )}
          .
        </Alert>
      )}
      {skipped && (
        <Alert color="yellow" icon={<IconAlertTriangle />} title="Unsupported configuration">
          The nerdctl version check is disabled (host {n.version || "?"}, required {n.required}.x).
        </Alert>
      )}
      {unsupported && (
        <Alert color="yellow" icon={<IconAlertTriangle />} title="Unsupported containerd version">
          containerd {runtime.containerd} is not tested with this nerdui build, which supports
          containerd {runtime.supportedContainerd.join(", ") || "—"} (nerdctl{" "}
          {runtime.nerdctlLibrary}).
        </Alert>
      )}
    </Stack>
  );
}
