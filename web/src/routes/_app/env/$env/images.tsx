// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Group, Loader, Title } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { envStatusQuery, imagesQuery } from "../../../../api/queries";
import { ImagesTable } from "../../../../features/images/ImagesTable";

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

  return (
    <>
      <Group justify="space-between" mb="md">
        <Title order={2}>Images</Title>
        {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
      </Group>
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
