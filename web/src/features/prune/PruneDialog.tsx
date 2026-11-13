// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Checkbox,
  Group,
  List,
  Loader,
  Modal,
  ScrollArea,
  Select,
  Stack,
  Text,
} from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap } from "../../api/client";
import { keys, namespacesQuery } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";
import { formatBytes } from "../system/info";

/**
 * Removes unused resources of a namespace (#52, ADR-0020) as a background job, after an
 * estimate of what goes. Volumes are never pruned.
 */
export function PruneDialog({
  env,
  defaultNamespace,
  opened,
  onClose,
}: {
  env: string;
  defaultNamespace: string;
  opened: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const panel = useJobsPanel();
  const namespaces = useQuery({ ...namespacesQuery(env), enabled: opened });
  const [namespace, setNamespace] = useState(defaultNamespace);
  const [containers, setContainers] = useState(true);
  const [images, setImages] = useState(true);
  const [allImages, setAllImages] = useState(false);
  const [networks, setNetworks] = useState(true);
  const [buildCache, setBuildCache] = useState(true);

  const preview = useQuery({
    queryKey: [...keys.env(env), "prune", namespace, allImages],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/system/prune", {
          params: { path: { env }, query: { namespace, allImages } },
        }),
      ),
    enabled: opened,
  });
  const prune = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/system/prune", {
          params: { path: { env }, query: { namespace } },
          body: { containers, images, allImages: images && allImages, networks, buildCache },
        }),
      ),
    onSuccess: async ({ job }) => {
      await queryClient.invalidateQueries({ queryKey: keys.jobs });
      close();
      panel.open(job.id);
    },
  });
  const close = () => {
    prune.reset();
    onClose();
  };
  const p = preview.data;
  const nothing = !containers && !images && !networks && !buildCache;

  return (
    <Modal opened={opened} onClose={close} title="Clean up unused resources" size="lg">
      <Stack gap="sm">
        {prune.error && <Alert color="red">{prune.error.message}</Alert>}
        <Select
          label="Namespace"
          data={namespaces.data?.namespaces ?? [namespace]}
          value={namespace}
          onChange={(v) => v && setNamespace(v)}
          allowDeselect={false}
          w={240}
        />
        <Checkbox
          label={`Stopped containers${p ? ` (${p.containers.length})` : ""}`}
          checked={containers}
          onChange={(e) => setContainers(e.currentTarget.checked)}
        />
        <Checkbox
          label={
            p
              ? `${allImages ? "Unused" : "Dangling"} images (${p.images.length}, up to ${formatBytes(p.imagesSize)})`
              : "Images"
          }
          checked={images}
          onChange={(e) => setImages(e.currentTarget.checked)}
        />
        {images && (
          <Checkbox
            ml="xl"
            size="xs"
            label="All images not used by a container, not only dangling ones"
            checked={allImages}
            onChange={(e) => setAllImages(e.currentTarget.checked)}
          />
        )}
        <Checkbox
          label="Unused networks (not counted in advance)"
          checked={networks}
          onChange={(e) => setNetworks(e.currentTarget.checked)}
        />
        <Checkbox
          label={
            p && !p.buildKit ? "Build cache (no BuildKit daemon found: skipped)" : "Build cache"
          }
          checked={buildCache}
          onChange={(e) => setBuildCache(e.currentTarget.checked)}
        />
        {preview.isPending && <Loader size="sm" aria-label="Loading preview" />}
        {preview.isError && <Text c="red">{preview.error.message}</Text>}
        {p && (containers || images) && (p.containers.length > 0 || p.images.length > 0) && (
          <ScrollArea.Autosize mah={180}>
            <List size="xs" aria-label="Will be removed">
              {containers &&
                p.containers.map((c) => (
                  <List.Item key={c.id}>container {c.name || c.id}</List.Item>
                ))}
              {images &&
                p.images.map((i) => (
                  <List.Item key={i.name}>
                    image {i.name}
                    {i.size ? ` (${formatBytes(i.size)})` : ""}
                  </List.Item>
                ))}
            </List>
          </ScrollArea.Autosize>
        )}
        <Text size="xs" c="dimmed">
          The list is an estimate made now; nerdctl decides when the cleanup runs. Volumes are never
          removed here. Image sizes overlap where images share layers.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>
            Cancel
          </Button>
          <Button
            color="red"
            loading={prune.isPending}
            disabled={nothing}
            onClick={() => prune.mutate()}
          >
            Clean up
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
