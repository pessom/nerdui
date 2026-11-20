// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Paper, Stack, Switch, Text, Title } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";

const securityKey = ["settings", "security"] as const;

/** Sign-in rules of the server (#75): require two-factor authentication. */
export function SecuritySettingsCard() {
  const queryClient = useQueryClient();
  const settings = useQuery({
    queryKey: securityKey,
    queryFn: () => unwrap(api.GET("/api/v1/settings/security")),
  });
  const save = useMutation({
    mutationFn: (requireMfa: boolean) =>
      unwrap(api.PUT("/api/v1/settings/security", { body: { requireMfa } })),
    onSuccess: (data) => {
      queryClient.setQueryData(securityKey, data);
      // Without two-factor authentication yourself, you are asked to set it up next.
      void queryClient.invalidateQueries({ queryKey: keys.me });
      notifications.show({ message: "Security settings saved" });
    },
    onError: (e) => notifications.show({ color: "red", title: "Cannot save", message: e.message }),
  });
  return (
    <Paper withBorder p="md" component="section" aria-label="Sign-in security">
      <Stack gap="sm">
        <Title order={3}>Sign-in security</Title>
        {settings.isError && <Alert color="red">{settings.error.message}</Alert>}
        {settings.data && (
          <Switch
            label="Require two-factor authentication for every user"
            description="Users without it can only set it up after signing in. API keys are not affected."
            checked={settings.data.requireMfa}
            disabled={save.isPending}
            onChange={(e) => save.mutate(e.currentTarget.checked)}
          />
        )}
        <Text size="xs" c="dimmed">
          Each user sets up two-factor authentication from their account menu.
        </Text>
      </Stack>
    </Paper>
  );
}
