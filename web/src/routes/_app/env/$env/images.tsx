// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Group, Loader, Title, Tooltip } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconDownload } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { envStatusQuery, imagesQuery } from "../../../../api/queries";
import { ImagesTable } from "../../../../features/images/ImagesTable";
import { PullDialog } from "../../../../features/images/PullDialog";

const envRoute = getRouteApi("/_app/env/$env");

/** Image list (#42): live via environment events, filters and sorting client-side. */
export const Route = createFileRoute("/_app/env/$env/images")({
  component: ImagesPage,
});

function ImagesPage() {
  const { env } = Route.useParams();
  const { ns } = envRoute.useSearch();
  const status = useQuery(envStatusQuery(env));
  const namespace = ns ?? status.data?.defaultNamespace ?? "default";
  const list = useQuery(imagesQuery(env, namespace));
  const [pulling, { open, close }] = useDisclosure();
  const nerdctl = status.data?.runtime?.nerdctl;
  // Read-only mode (ADR-0011): the server would answer 503 nerdctl_incompatible.
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;

  return (
    <>
      <Group justify="space-between" mb="md">
        <Group gap="xs">
          <Title order={2}>Images</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
          <Button leftSection={<IconDownload size={16} />} onClick={open} disabled={readOnly}>
            Pull image
          </Button>
        </Tooltip>
      </Group>
      <PullDialog env={env} namespace={namespace} opened={pulling} onClose={close} />
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot list images">
          {list.error.message}
        </Alert>
      )}
      {list.data && <ImagesTable images={list.data.images} />}
    </>
  );
}
