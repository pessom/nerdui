// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Code, Group, Modal, Stack, Text, TextInput, Textarea } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, type Volume } from "../../api/queries";
import { parseLabels } from "./labels";

export function CreateVolumeDialog({
  env,
  namespace,
  opened,
  onClose,
}: {
  env: string;
  namespace: string;
  opened: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [labelText, setLabelText] = useState("");
  const parsed = parseLabels(labelText);
  const close = () => {
    setName("");
    setLabelText("");
    create.reset();
    onClose();
  };
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/volumes", {
          params: { path: { env }, query: { namespace } },
          body: { name: name.trim(), labels: parsed.labels },
        }),
      ),
    onSuccess: (v) => {
      notifications.show({ color: "teal", message: `Volume ${v.name} created` });
      void queryClient.invalidateQueries({ queryKey: keys.volumes(env, namespace) });
      close();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!parsed.error) create.mutate();
  };
  return (
    <Modal opened={opened} onClose={close} title="Create volume">
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Name"
            required
            data-autofocus
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <Textarea
            label="Labels"
            description="One key=value per line"
            rows={3}
            value={labelText}
            onChange={(e) => setLabelText(e.currentTarget.value)}
            error={parsed.error}
          />
          {create.isError && (
            <Alert color="red" title="Cannot create volume">
              {create.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
              Create
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}

/** Removal deletes the data: the name must be typed to confirm. */
export function RemoveVolumeDialog({
  env,
  namespace,
  volume,
  onClose,
}: {
  env: string;
  namespace: string;
  volume?: Volume;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [typed, setTyped] = useState("");
  const close = () => {
    setTyped("");
    remove.reset();
    onClose();
  };
  const remove = useMutation({
    mutationFn: (name: string) =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/volumes/{name}", {
          params: { path: { env, name }, query: { namespace } },
        }),
      ),
    onSuccess: (_, name) => {
      notifications.show({ message: `Volume ${name} removed` });
      void queryClient.invalidateQueries({ queryKey: keys.volumes(env, namespace) });
      close();
    },
  });
  return (
    <Modal opened={!!volume} onClose={close} title="Remove volume">
      {volume && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (typed === volume.name) remove.mutate(volume.name);
          }}
        >
          <Stack gap="sm">
            <Text size="sm">
              The volume <Code>{volume.name}</Code> and <b>all its data</b> will be deleted. This
              cannot be undone.
            </Text>
            <TextInput
              label={`Type ${volume.name} to confirm`}
              data-autofocus
              value={typed}
              onChange={(e) => setTyped(e.currentTarget.value)}
            />
            {remove.isError && (
              <Alert color="red" title="Cannot remove volume">
                {remove.error.message}
              </Alert>
            )}
            <Group justify="flex-end">
              <Button variant="default" onClick={close}>
                Cancel
              </Button>
              <Button
                type="submit"
                color="red"
                loading={remove.isPending}
                disabled={typed !== volume.name}
              >
                Remove volume
              </Button>
            </Group>
          </Stack>
        </form>
      )}
    </Modal>
  );
}
