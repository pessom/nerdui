// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Code, Group, List, Modal, Stack, Text, TextInput } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys } from "../../api/queries";
import { WELL_KNOWN, holders, nameError, type NamespaceSummary } from "./namespaces";

/** Creates a namespace (#195). */
export function CreateNamespaceDialog({
  env,
  opened,
  existing,
  onClose,
}: {
  env: string;
  opened: boolean;
  existing: string[];
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: (n: string) =>
      unwrap(
        api.POST("/api/v1/environments/{env}/namespaces", {
          params: { path: { env } },
          body: { name: n },
        }),
      ),
    onSuccess: (_, n) => {
      notifications.show({ message: `Namespace ${n} created` });
      void queryClient.invalidateQueries({ queryKey: keys.env(env) });
      close();
    },
  });
  const close = () => {
    setName("");
    create.reset();
    onClose();
  };
  const trimmed = name.trim();
  const error = existing.includes(trimmed) ? "Already exists" : nameError(trimmed);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (trimmed && !error) create.mutate(trimmed);
  };
  return (
    <Modal opened={opened} onClose={close} title="Create namespace">
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Name"
            description="Isolates containers, images, volumes and networks, like nerdctl -n"
            required
            data-autofocus
            value={name}
            error={error}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          {create.isError && (
            <Alert color="red" title="Cannot create the namespace">
              {create.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!trimmed || !!error}>
              Create
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}

/** Removes an empty namespace (#195); well-known ones need the name typed. */
export function RemoveNamespaceDialog({
  env,
  namespace,
  isDefault,
  onClose,
  onRemoved,
}: {
  env: string;
  namespace?: NamespaceSummary;
  /** The environment's default namespace. */
  isDefault: boolean;
  onClose: () => void;
  onRemoved: (name: string) => void;
}) {
  const queryClient = useQueryClient();
  const [typed, setTyped] = useState("");
  const remove = useMutation({
    mutationFn: (name: string) =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/namespaces/{name}", {
          params: { path: { env, name } },
        }),
      ),
    onSuccess: (_, name) => {
      notifications.show({ message: `Namespace ${name} removed` });
      void queryClient.invalidateQueries({ queryKey: keys.env(env) });
      onRemoved(name);
      close();
    },
  });
  const close = () => {
    setTyped("");
    remove.reset();
    onClose();
  };
  const held = namespace ? holders(namespace) : [];
  const guarded = !!namespace && (isDefault || WELL_KNOWN.includes(namespace.name));
  const confirmed = !guarded || typed === namespace?.name;
  return (
    <Modal opened={!!namespace} onClose={close} title="Remove namespace">
      {namespace && (
        <Stack gap="sm">
          {held.length > 0 ? (
            <Alert color="yellow" title="The namespace is not empty">
              Remove these first:
              <List size="sm" mt={4}>
                {held.map((h) => (
                  <List.Item key={h}>{h}</List.Item>
                ))}
              </List>
            </Alert>
          ) : (
            <Text size="sm">
              Remove the empty namespace <Code>{namespace.name}</Code>?
            </Text>
          )}
          {guarded && held.length === 0 && (
            <>
              <Alert color="orange">
                {isDefault
                  ? "This is the environment's default namespace."
                  : "Other tools use this namespace (Kubernetes, Docker, BuildKit)."}{" "}
                They recreate it when needed, but may expect what they kept there.
              </Alert>
              <TextInput
                label={`Type ${namespace.name} to confirm`}
                value={typed}
                onChange={(e) => setTyped(e.currentTarget.value)}
              />
            </>
          )}
          {remove.isError && (
            <Alert color="red" title="Cannot remove the namespace">
              {remove.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button
              color="red"
              loading={remove.isPending}
              disabled={held.length > 0 || !confirmed}
              onClick={() => remove.mutate(namespace.name)}
            >
              Remove
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  );
}
