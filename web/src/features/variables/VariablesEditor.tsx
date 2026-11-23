// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Code,
  Group,
  Loader,
  Modal,
  Stack,
  Table,
  Text,
  Textarea,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconPencil, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { formatTime, usePreferences } from "../preferences/context";
import {
  deleteVariable,
  putVariable,
  variableNameError,
  variablesKey,
  variablesQuery,
  type Variable,
  type VariableScope,
} from "./variables";

/** A table of variables with add, edit and delete (#35, ADR-0049). */
export function VariablesEditor({ scope }: { scope: VariableScope }) {
  const { prefs } = usePreferences();
  const queryClient = useQueryClient();
  const list = useQuery(variablesQuery(scope));
  const [editing, setEditing] = useState<Variable | "new" | null>(null);
  const [removing, setRemoving] = useState<Variable | null>(null);
  const remove = useMutation({
    mutationFn: (name: string) => deleteVariable(scope, name),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: variablesKey(scope) });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot delete", message: e.message }),
  });

  return (
    <Stack gap="sm">
      <Group justify="flex-end">
        <Button onClick={() => setEditing("new")}>Add variable</Button>
      </Group>
      {list.isPending && <Loader aria-label="Loading variables" />}
      {list.isError && (
        <Alert color="red" title="Cannot read variables">
          {list.error.message}
        </Alert>
      )}
      {list.data && list.data.variables.length === 0 && (
        <Text size="sm" c="dimmed">
          No variables yet.
        </Text>
      )}
      {list.data && list.data.variables.length > 0 && (
        <Table.ScrollContainer minWidth={560}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Name</Table.Th>
                <Table.Th>Value</Table.Th>
                <Table.Th>Updated</Table.Th>
                <Table.Th />
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {list.data.variables.map((v) => (
                <Table.Tr key={v.name}>
                  <Table.Td>
                    <Code>{v.name}</Code>
                  </Table.Td>
                  <Table.Td style={{ maxWidth: 360 }}>
                    {v.secret ? (
                      <Badge size="sm" variant="light" color="grape">
                        secret
                      </Badge>
                    ) : (
                      <Text size="sm" ff="monospace" truncate="end" title={v.value}>
                        {v.value || (
                          <Text span c="dimmed">
                            (empty)
                          </Text>
                        )}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    {formatTime(v.updatedAt, prefs.dateFormat).text}
                    {v.updatedBy && (
                      <Text span size="xs" c="dimmed">
                        {" "}
                        by {v.updatedBy}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Group gap={4} justify="flex-end" wrap="nowrap">
                      <Tooltip label="Edit">
                        <ActionIcon
                          variant="subtle"
                          aria-label={`Edit ${v.name}`}
                          onClick={() => setEditing(v)}
                        >
                          <IconPencil size={16} />
                        </ActionIcon>
                      </Tooltip>
                      <Tooltip label="Delete">
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          aria-label={`Delete ${v.name}`}
                          onClick={() => setRemoving(v)}
                        >
                          <IconTrash size={16} />
                        </ActionIcon>
                      </Tooltip>
                    </Group>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      {editing && (
        <VariableDialog
          scope={scope}
          variable={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}
      <Modal opened={!!removing} onClose={() => setRemoving(null)} title="Delete variable">
        {removing && (
          <Stack>
            <Text size="sm">
              Delete <Code>{removing.name}</Code>? Stacks get the change on their next deploy.
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setRemoving(null)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={remove.isPending}
                onClick={() => remove.mutate(removing.name)}
              >
                Delete
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}

function VariableDialog({
  scope,
  variable,
  onClose,
}: {
  scope: VariableScope;
  variable: Variable | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(variable?.name ?? "");
  const [value, setValue] = useState(variable?.value ?? "");
  const [secret, setSecret] = useState(variable?.secret ?? false);
  const [touched, setTouched] = useState(false);
  // A stored secret keeps its value unless a new one is entered.
  const keepSecret = !!variable?.secret && value === "";
  const nameError = variable ? null : variableNameError(name);
  const valueError =
    variable?.secret && !secret && value === "" ? "Enter the value to make it visible" : null;
  const save = useMutation({
    mutationFn: () => putVariable(scope, name, keepSecret ? { secret } : { value, secret }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: variablesKey(scope) });
      onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!nameError && !valueError) save.mutate();
  };

  return (
    <Modal opened onClose={onClose} title={variable ? `Edit ${variable.name}` : "Add variable"}>
      <form onSubmit={submit}>
        <Stack>
          <TextInput
            label="Name"
            description="Use it in compose files as ${NAME}"
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
            disabled={!!variable}
            error={touched && nameError}
            data-autofocus={!variable || undefined}
            ff="monospace"
          />
          <Textarea
            label="Value"
            placeholder={variable?.secret ? "Leave empty to keep the stored value" : undefined}
            value={value}
            onChange={(e) => setValue(e.currentTarget.value)}
            error={touched && valueError}
            rows={3}
            styles={{ input: { fontFamily: "var(--mantine-font-family-monospace)" } }}
            data-autofocus={!!variable || undefined}
          />
          <Checkbox
            label="Secret"
            description="The value is never shown again, only passed to deployments"
            checked={secret}
            onChange={(e) => setSecret(e.currentTarget.checked)}
          />
          {save.isError && (
            <Alert color="red" title="Cannot save">
              {save.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending}>
              Save
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
