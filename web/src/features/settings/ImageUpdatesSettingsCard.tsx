// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Group, Paper, Stack, Text, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { imageUpdatesQuery } from "../../api/queries";
import { RouterAnchor } from "../../components/RouterAnchor";
import { formatTime, usePreferences } from "../preferences/context";
import { AutoUpdateLog } from "../updates/AutoUpdateLog";
import { CheckUpdatesButton } from "../updates/CheckUpdatesButton";

/** Image update checks (#53, ADR-0030); their schedule is under Schedules (#82). */
export function ImageUpdatesSettingsCard({ env, namespace }: { env: string; namespace: string }) {
  const { prefs } = usePreferences();
  const overview = useQuery(imageUpdatesQuery(env, namespace));
  const when = (iso?: string) => (iso ? formatTime(iso, prefs.dateFormat).text : undefined);
  return (
    <Paper withBorder p="md" component="section" aria-label="Image updates">
      <Stack gap="sm">
        <div>
          <Title order={3}>Image updates</Title>
          <Text size="sm" c="dimmed">
            nerdui asks the registries whether the tags of images used by containers point to a
            newer image, with one HEAD request per tag (Docker Hub does not count it as a pull).
            Images with updates are marked in the containers and images lists. Only containers that
            opted in are updated automatically, below.
          </Text>
        </div>
        <Group align="center" gap="md" wrap="wrap">
          <CheckUpdatesButton env={env} overview={overview.data} />
          {overview.data && (
            <Text size="sm" c="dimmed">
              {overview.data.lastCheck
                ? `Last check ${when(overview.data.lastCheck)}`
                : "Not checked yet"}
              {overview.data.nextCheck
                ? `, next ${when(overview.data.nextCheck)}.`
                : ", no scheduled checks."}
            </Text>
          )}
        </Group>
        <Text size="sm">
          Scheduled checks are set under{" "}
          <RouterAnchor to="/schedules" size="sm">
            Schedules
          </RouterAnchor>{" "}
          (Image update check).
        </Text>
        <AutoUpdateLog env={env} namespace={namespace} />
      </Stack>
    </Paper>
  );
}
