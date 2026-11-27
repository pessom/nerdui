// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Stack, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { envStatusQuery } from "../../../../api/queries";
import { WebhooksSettingsCard } from "../../../../features/settings/WebhooksSettingsCard";
import { GitCredentialsSettingsCard } from "../../../../features/settings/GitCredentialsSettingsCard";
import { NotificationsSettingsCard } from "../../../../features/settings/NotificationsSettingsCard";
import { ImageUpdatesSettingsCard } from "../../../../features/settings/ImageUpdatesSettingsCard";
import { SecuritySettingsCard } from "../../../../features/settings/SecuritySettingsCard";
import { OIDCSettingsCard } from "../../../../features/settings/OIDCSettingsCard";
import { BackupSettingsCard } from "../../../../features/settings/BackupSettingsCard";
import { VariablesSettingsCard } from "../../../../features/settings/VariablesSettingsCard";
import { BackupTargetsSettingsCard } from "../../../../features/settings/BackupTargetsSettingsCard";
import { PrometheusSettingsCard } from "../../../../features/settings/PrometheusSettingsCard";
import { HostAlertsSettingsCard } from "../../../../features/settings/HostAlertsSettingsCard";
import { ReadOnlyUnless } from "../../../../components/ReadOnlyUnless";

const envRoute = getRouteApi("/_app/env/$env");

/** Settings of the environment (#176, #53) and notifications (#86), Git credentials (#93), incoming webhooks (#85). */
export const Route = createFileRoute("/_app/env/$env/settings")({
  component: SettingsPage,
});

function SettingsPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  return (
    <Stack>
      <Title order={2}>Settings</Title>
      <ReadOnlyUnless action="settings:write">
        <PrometheusSettingsCard env={env} />
        <ImageUpdatesSettingsCard env={env} namespace={namespace} />
        <NotificationsSettingsCard />
        <HostAlertsSettingsCard />
        <VariablesSettingsCard />
        <GitCredentialsSettingsCard />
        <WebhooksSettingsCard env={env} namespace={namespace} />
        <SecuritySettingsCard />
        <OIDCSettingsCard />
        <BackupSettingsCard />
        <BackupTargetsSettingsCard />
      </ReadOnlyUnless>
    </Stack>
  );
}
