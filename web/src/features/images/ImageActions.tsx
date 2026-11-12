// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Button,
  Checkbox,
  Group,
  List,
  Menu,
  Modal,
  ScrollArea,
  Stack,
  Text,
  TextInput,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconDots, IconTag, IconTrash, IconUpload } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, type Image } from "../../api/queries";
import { displayName } from "./format";

export interface ImageActionHandlers {
  tag: (img: Image) => void;
  push: (img: Image) => void;
  remove: (imgs: Image[]) => void;
  /** Why actions are unavailable (read-only mode); undefined when they are available. */
  disabled?: string;
}

/** Row menu: tag, push, remove (#44). */
export function ImageRowActions({
  image,
  actions,
}: {
  image: Image;
  actions: ImageActionHandlers;
}) {
  const name = displayName(image);
  return (
    <Menu position="bottom-end" withArrow>
      <Menu.Target>
        <ActionIcon
          variant="subtle"
          color="gray"
          aria-label={`Actions for ${name}`}
          disabled={!!actions.disabled}
          title={actions.disabled}
        >
          <IconDots size={16} />
        </ActionIcon>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Item leftSection={<IconTag size={16} />} onClick={() => actions.tag(image)}>
          Tag…
        </Menu.Item>
        <Menu.Item
          leftSection={<IconUpload size={16} />}
          onClick={() => actions.push(image)}
          disabled={!image.repository}
        >
          Push…
        </Menu.Item>
        <Menu.Divider />
        <Menu.Item
          color="red"
          leftSection={<IconTrash size={16} />}
          onClick={() => actions.remove([image])}
        >
          Remove…
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}

/** Adds a reference to an image. */
export function TagDialog({
  env,
  namespace,
  image,
  onClose,
}: {
  env: string;
  namespace: string;
  image?: Image;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [target, setTarget] = useState("");
  const tag = useMutation({
    mutationFn: (source: string) =>
      unwrapEmpty(
        api.POST("/api/v1/environments/{env}/images/tag", {
          params: { path: { env }, query: { namespace } },
          body: { source, target: target.trim() },
        }),
      ),
    onSuccess: () => {
      notifications.show({ color: "teal", message: `Tagged ${target.trim()}` });
      void queryClient.invalidateQueries({ queryKey: keys.images(env, namespace) });
      close();
    },
  });
  const close = () => {
    setTarget("");
    tag.reset();
    onClose();
  };
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (image && target.trim()) tag.mutate(image.name);
  };
  return (
    <Modal opened={!!image} onClose={close} title={image && `Tag ${displayName(image)}`}>
      <form onSubmit={onSubmit}>
        <Stack>
          {tag.error && <Alert color="red">{tag.error.message}</Alert>}
          <TextInput
            label="New reference"
            description="e.g. registry.local:5000/app:1.0 — push it afterwards to publish"
            required
            autoFocus
            autoComplete="off"
            value={target}
            onChange={(e) => setTarget(e.currentTarget.value)}
          />
          <Button type="submit" loading={tag.isPending} disabled={!target.trim()}>
            Tag
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}

/** Confirms and removes one or more image references, like `nerdctl rmi`. */
export function RemoveImagesDialog({
  env,
  namespace,
  images,
  onClose,
  onRemoved,
}: {
  env: string;
  namespace: string;
  images: Image[];
  onClose: () => void;
  onRemoved?: (names: string[]) => void;
}) {
  const queryClient = useQueryClient();
  const [force, setForce] = useState(false);
  const used = images.filter((i) => i.containers > 0);
  const remove = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/images/remove", {
          params: { path: { env }, query: { namespace } },
          body: { references: images.map((i) => i.name), force },
        }),
      ),
    onSuccess: ({ results }) => {
      const ok = results.filter((r) => r.ok);
      const failed = results.filter((r) => !r.ok);
      if (ok.length > 0) {
        notifications.show({
          color: "teal",
          message: ok.length === 1 ? `Removed ${ok[0]?.reference}` : `Removed ${ok.length} images`,
        });
        onRemoved?.(ok.map((r) => r.reference));
      }
      for (const f of failed) {
        notifications.show({
          color: "red",
          title: `Cannot remove ${f.reference}`,
          message: f.error?.message ?? "unknown error",
          autoClose: false,
        });
      }
      void queryClient.invalidateQueries({ queryKey: keys.images(env, namespace) });
      close();
    },
  });
  const close = () => {
    setForce(false);
    remove.reset();
    onClose();
  };
  const n = images.length;
  return (
    <Modal
      opened={n > 0}
      onClose={close}
      title={n === 1 && images[0] ? `Remove ${displayName(images[0])}?` : `Remove ${n} images?`}
    >
      <Stack gap="sm">
        {remove.error && <Alert color="red">{remove.error.message}</Alert>}
        <ScrollArea.Autosize mah={200}>
          <List size="sm">
            {images.map((i) => (
              <List.Item key={i.name}>
                <Text span ff="monospace" size="sm">
                  {displayName(i)}
                </Text>
                {i.containers > 0 && (
                  <Text span c="orange" size="xs">
                    {" "}
                    used by {i.containers} container{i.containers > 1 ? "s" : ""}
                  </Text>
                )}
              </List.Item>
            ))}
          </List>
        </ScrollArea.Autosize>
        <Text size="sm">
          Only the reference is removed; content shared with other tags stays until nothing uses it.
        </Text>
        {used.length > 0 && (
          <Checkbox
            label="Force: also remove images used by containers"
            description="Images of stopped containers are deleted; images of running containers are only untagged."
            checked={force}
            onChange={(e) => setForce(e.currentTarget.checked)}
          />
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>
            Cancel
          </Button>
          <Button
            color="red"
            loading={remove.isPending}
            disabled={used.length > 0 && !force && used.length === n}
            onClick={() => remove.mutate()}
          >
            Remove
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
