// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Button, Tooltip } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconRefreshDot } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import type { ImageUpdateOverview } from "../../api/queries";
import { formatTime, usePreferences } from "../preferences/context";

/** Checks the images used by containers for newer registry digests now (#53). */
export function CheckUpdatesButton({
  env,
  overview,
}: {
  env: string;
  overview?: ImageUpdateOverview;
}) {
  const queryClient = useQueryClient();
  const { prefs } = usePreferences();
  const check = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/image-updates/check", {
          params: { path: { env } },
          body: {},
        }),
      ),
    onSuccess: (r) => {
      notifications.show({
        color: r.updates > 0 ? "teal" : undefined,
        title: "Update check finished",
        message:
          `${r.checked} image${r.checked === 1 ? "" : "s"} checked, ` +
          `${r.updates} with updates` +
          (r.failed > 0 ? `, ${r.failed} could not be checked` : ""),
      });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Update check failed", message: e.message }),
    onSettled: () =>
      void queryClient.invalidateQueries({ queryKey: ["env", env, "image-updates"] }),
  });
  const last = overview?.lastCheck
    ? `Last check ${formatTime(overview.lastCheck, prefs.dateFormat).text}`
    : "Not checked yet";
  return (
    <Tooltip label={`${last}. Asks the registries of images used by containers.`}>
      <Button
        variant="default"
        leftSection={<IconRefreshDot size={16} />}
        loading={check.isPending || overview?.running}
        onClick={() => check.mutate()}
      >
        Check for updates
      </Button>
    </Tooltip>
  );
}
