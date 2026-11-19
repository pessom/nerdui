// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Group, Loader, Paper, Select, Stack, Text, Title } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { imageUpdatesQuery } from "../../api/queries";
import { formatTime, usePreferences } from "../preferences/context";
import { AutoUpdateLog } from "../updates/AutoUpdateLog";
import { CheckUpdatesButton } from "../updates/CheckUpdatesButton";

const intervals = [
  { value: "0", label: "Off (only on request)" },
  { value: "3600", label: "Every hour" },
  { value: "21600", label: "Every 6 hours" },
  { value: "43200", label: "Every 12 hours" },
  { value: "86400", label: "Every day" },
];

/** Schedule of image update checks (#53, ADR-0030). */
export function ImageUpdatesSettingsCard({ env, namespace }: { env: string; namespace: string }) {
  const queryClient = useQueryClient();
  const { prefs } = usePreferences();
  const settingsKey = ["env", env, "settings", "image-updates"] as const;
  const settings = useQuery({
    queryKey: settingsKey,
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/settings/image-updates", { params: { path: { env } } }),
      ),
  });
  const overview = useQuery(imageUpdatesQuery(env, namespace));
  const save = useMutation({
    mutationFn: (intervalSeconds: number) =>
      unwrap(
        api.PUT("/api/v1/environments/{env}/settings/image-updates", {
          params: { path: { env } },
          body: { intervalSeconds: intervalSeconds as 0 | 3600 | 21600 | 43200 | 86400 },
        }),
      ),
    onSuccess: (data) => {
      queryClient.setQueryData(settingsKey, data);
      void queryClient.invalidateQueries({ queryKey: ["env", env, "image-updates"] });
      notifications.show({ message: "Update check schedule saved" });
    },
    onError: (e) => notifications.show({ color: "red", title: "Cannot save", message: e.message }),
  });
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
        {settings.isPending && <Loader aria-label="Loading settings" />}
        {settings.isError && (
          <Alert color="red" title="Cannot read settings">
            {settings.error.message}
          </Alert>
        )}
        {settings.data && (
          <Group align="flex-end" gap="md" wrap="wrap">
            <Select
              label="Check automatically"
              w={240}
              data={intervals}
              value={String(settings.data.intervalSeconds)}
              onChange={(v) => v !== null && save.mutate(Number(v))}
              allowDeselect={false}
              disabled={save.isPending}
            />
            <CheckUpdatesButton env={env} overview={overview.data} />
          </Group>
        )}
        {overview.data && (
          <Text size="sm" c="dimmed">
            {overview.data.lastCheck
              ? `Last check ${when(overview.data.lastCheck)}`
              : "Not checked yet"}
            {overview.data.nextCheck && `, next ${when(overview.data.nextCheck)}`}.
          </Text>
        )}
        <AutoUpdateLog env={env} namespace={namespace} />
      </Stack>
    </Paper>
  );
}
