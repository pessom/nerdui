// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Stack, Title } from "@mantine/core";
import { createFileRoute } from "@tanstack/react-router";
import { PrometheusSettingsCard } from "../../../../features/settings/PrometheusSettingsCard";

/** Settings of the environment (#176). */
export const Route = createFileRoute("/_app/env/$env/settings")({
  component: SettingsPage,
});

function SettingsPage() {
  const { env } = Route.useParams();
  return (
    <Stack>
      <Title order={2}>Settings</Title>
      <PrometheusSettingsCard env={env} />
    </Stack>
  );
}
