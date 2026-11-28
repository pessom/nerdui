// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Group, Loader, Title, Tooltip } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { IconDownload, IconFileImport, IconHammer } from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import {
  envStatusQuery,
  imageUpdatesQuery,
  imagesQuery,
  updatesByRef,
  type Image,
} from "../../../../api/queries";
import { BuildImageDialog } from "../../../../features/images/BuildImageDialog";
import { ImagesTable } from "../../../../features/images/ImagesTable";
import { RemoveImagesDialog, TagDialog } from "../../../../features/images/ImageActions";
import { ImageHistoryDialog } from "../../../../features/images/ImageHistoryDialog";
import { ImageJobDialog } from "../../../../features/images/ImageJobDialog";
import { ImageLoadDialog } from "../../../../features/images/ImageLoadDialog";
import { downloadImages } from "../../../../features/images/archive";
import { CheckUpdatesButton } from "../../../../features/updates/CheckUpdatesButton";
import { useCan } from "../../../../lib/permissions";
import { ImageUpdatesContext } from "../../../../features/updates/context";

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
  const updates = useQuery(imageUpdatesQuery(env, namespace));
  const [pulling, { open, close }] = useDisclosure();
  const [loading, loadDialog] = useDisclosure();
  const [building, buildDialog] = useDisclosure();
  const [history, setHistory] = useState<Image>();
  const [tagging, setTagging] = useState<Image>();
  const [pushing, setPushing] = useState<Image>();
  const [removing, setRemoving] = useState<Image[]>([]);
  const nerdctl = status.data?.runtime?.nerdctl;
  // Read-only mode (ADR-0011): the server would answer 503 nerdctl_incompatible.
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const write = useCan()("images:write");

  return (
    <>
      <Group justify="space-between" mb="md">
        <Group gap="xs">
          <Title order={2}>Images</Title>
          {list.isFetching && !list.isPending && <Loader size="xs" aria-label="Refreshing" />}
        </Group>
        <Group gap="sm">
          {updates.data && <CheckUpdatesButton env={env} overview={updates.data} />}
          {write && (
            <>
              <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
                <Button
                  variant="default"
                  leftSection={<IconHammer size={16} />}
                  onClick={buildDialog.open}
                  disabled={readOnly}
                >
                  Build
                </Button>
              </Tooltip>
              <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
                <Button
                  variant="default"
                  leftSection={<IconFileImport size={16} />}
                  onClick={loadDialog.open}
                  disabled={readOnly}
                >
                  Load
                </Button>
              </Tooltip>
              <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
                <Button leftSection={<IconDownload size={16} />} onClick={open} disabled={readOnly}>
                  Pull image
                </Button>
              </Tooltip>
            </>
          )}
        </Group>
      </Group>
      <ImageJobDialog
        kind="pull"
        env={env}
        namespace={namespace}
        opened={pulling}
        onClose={close}
      />
      <BuildImageDialog
        env={env}
        namespace={namespace}
        opened={building}
        onClose={buildDialog.close}
      />
      <ImageLoadDialog
        env={env}
        namespace={namespace}
        opened={loading}
        onClose={loadDialog.close}
      />
      <ImageJobDialog
        key={pushing?.name ?? ""}
        kind="push"
        env={env}
        namespace={namespace}
        opened={!!pushing}
        initialReference={pushing?.name ?? ""}
        onClose={() => setPushing(undefined)}
      />
      <ImageHistoryDialog
        env={env}
        namespace={namespace}
        image={history}
        onClose={() => setHistory(undefined)}
      />
      <TagDialog
        env={env}
        namespace={namespace}
        image={tagging}
        onClose={() => setTagging(undefined)}
      />
      <RemoveImagesDialog
        env={env}
        namespace={namespace}
        images={removing}
        onClose={() => setRemoving([])}
      />
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot list images">
          {list.error.message}
        </Alert>
      )}
      {list.data && (
        <ImageUpdatesContext.Provider value={updatesByRef(updates.data)}>
          <ImagesTable
            images={list.data.images}
            actions={{
              history: setHistory,
              tag: setTagging,
              push: setPushing,
              remove: setRemoving,
              save: (imgs) =>
                downloadImages(
                  env,
                  namespace,
                  imgs.map((i) => i.name),
                ),
              disabled: readOnly ? "Read-only mode: host nerdctl is incompatible" : undefined,
            }}
          />
        </ImageUpdatesContext.Provider>
      )}
    </>
  );
}
