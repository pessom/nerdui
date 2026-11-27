// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Anchor, Stack } from "@mantine/core";
import { IconAlertTriangle, IconLock, IconPlugConnectedX } from "@tabler/icons-react";
import type { components } from "../api/client";

type RuntimeVersions = components["schemas"]["RuntimeVersions"];
type ACMEStatus = components["schemas"]["ACMEStatus"];
type HostWarning = NonNullable<components["schemas"]["EnvironmentStatus"]["warnings"]>[number];

/** The deployment guide's section on the AppArmor signal problem (ADR-0060). */
const apparmorDocs =
  "https://github.com/pessom/nerdui/blob/main/docs/deployment.md#apparmor-on-linux-7";

/** "2.3.5" → "2.3"; empty for unknown versions. */
function hostMinor(version: string | undefined): string {
  return /^(\d+\.\d+)\./.exec(version ?? "")?.[1] ?? "";
}

/**
 * Warnings for the runtime support policy (ADR-0011, ADR-0012, ADR-0017), connectivity and
 * host settings known to break containers (ADR-0060).
 */
export function RuntimeBanners({
  runtime,
  containerdDown,
  acme,
  warnings = [],
}: {
  runtime?: RuntimeVersions;
  containerdDown: boolean;
  acme?: ACMEStatus;
  warnings?: HostWarning[];
}) {
  const acmeFailing = !!acme && !acme.ready && !!acme.error;
  const n = runtime?.nerdctl;
  const readOnly = n && !n.compatible && !n.checkSkipped;
  const skipped = n && n.checkSkipped && !n.compatible;
  const unsupported = runtime?.containerd && !runtime.containerdSupported;
  const apparmor = warnings.includes("apparmor_signals");
  if (!containerdDown && !readOnly && !skipped && !unsupported && !acmeFailing && !apparmor)
    return null;
  return (
    <Stack gap="xs" mb="md">
      {acmeFailing && (
        <Alert color="yellow" icon={<IconLock />} title="No certificate from Let's Encrypt yet">
          nerdui could not get a certificate for {acme.domains.join(", ")} and serves its
          self-signed one meanwhile: {acme.error}. It retries every 10 minutes; check that the
          domain points to this host and port 443 (or the --tls-acme-http port) is reachable.
        </Alert>
      )}
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
      {apparmor && (
        <Alert
          color="yellow"
          icon={<IconAlertTriangle />}
          title="AppArmor may block containers that switch user"
        >
          On this kernel AppArmor with{" "}
          <code>kernel.apparmor_restrict_unprivileged_unconfined=1</code> denies signals inside
          containers. Images whose entrypoint switches user with <code>gosu</code> (MySQL, MariaDB,
          PostgreSQL and many others) then hang at start at 100% CPU; the kernel log shows{" "}
          <code>apparmor=&quot;DENIED&quot; operation=&quot;signal&quot;</code>. Set the sysctl to{" "}
          <code>0</code>, run such a container with <code>apparmor=unconfined</code>, or update the
          kernel once it is fixed: see{" "}
          <Anchor href={apparmorDocs} target="_blank" rel="noreferrer" inherit>
            AppArmor on Linux 7
          </Anchor>{" "}
          in the deployment guide.
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
