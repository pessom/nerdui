// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Group, Modal, Stack, Text, TextInput } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";

/**
 * Creates a stack in nerdui's stacks directory with a starter compose.yaml (#26) and opens it
 * in the editor.
 */
export function NewStackDialog({
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
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const close = () => {
    setName("");
    create.reset();
    onClose();
  };
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks", {
          params: { path: { env }, query: { namespace } },
          body: { name: name.trim() },
        }),
      ),
    onSuccess: (s) => {
      notifications.show({ color: "teal", message: `Stack ${s.name} created` });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      close();
      void navigate({
        to: "/env/$env/stacks/$name",
        params: { env, name: s.name },
        search: { ns: namespace, tab: "compose" },
      });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  return (
    <Modal opened={opened} onClose={close} title="New stack">
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Name"
            description="The compose project name: letters, digits, '-', '_' and '.'"
            required
            data-autofocus
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <Text size="sm" c="dimmed">
            A directory with a starter compose.yaml is created in the stacks directory and opened in
            the editor. Nothing is started until you deploy it.
          </Text>
          {create.isError && (
            <Alert color="red" title="Cannot create the stack">
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
