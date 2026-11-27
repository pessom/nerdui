// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Modal,
  Select,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
  VisuallyHidden,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconEye, IconPencil, IconPlus, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, rolesQuery, type Role, type UserDetail } from "../../api/queries";
import { PermissionPicker } from "../../components/PermissionPicker";
import { useCan, type Permission } from "../../lib/permissions";

const ROLE_RE = /^[a-z][a-z0-9-]{0,31}$/;

/** The caller may grant a role only with every one of its permissions (ADR-0058). */
function useGrantable() {
  const can = useCan();
  return (r: Pick<Role, "permissions">) => r.permissions.every((p) => can(p as Permission));
}

const builtinRoles = ["admin", "editor", "operator", "viewer"];

/**
 * Picks a role; roles with permissions the caller lacks are disabled. Without users:read the
 * list of roles is unavailable, and only the built-in roles are offered.
 */
export function RoleSelect({
  value,
  onChange,
  label = "Role",
  description,
}: {
  value: string;
  onChange: (v: string) => void;
  label?: string;
  description?: string;
}) {
  const can = useCan();
  const roles = useQuery({ ...rolesQuery, enabled: can("users:read") });
  const grantable = useGrantable();
  const current = roles.data?.find((r) => r.name === value);
  const data = roles.data
    ? roles.data.map((r) => ({ value: r.name, label: r.name, disabled: !grantable(r) }))
    : [...new Set([...builtinRoles, value])].map((r) => ({ value: r, label: r }));
  return (
    <Select
      label={label}
      required
      allowDeselect={false}
      data={data}
      value={value}
      onChange={(v) => v && onChange(v)}
      description={description ?? current?.description}
    />
  );
}

/** Gives another user a role. */
export function ChangeRoleModal({
  user,
  onClose,
}: {
  user: UserDetail | undefined;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [role, setRole] = useState<string>();
  const change = useMutation({
    mutationFn: (u: UserDetail) =>
      unwrap(
        api.PATCH("/api/v1/users/{id}", {
          params: { path: { id: u.id } },
          body: { role: role ?? u.role },
        }),
      ),
    onSuccess: (u) => {
      notifications.show({ color: "teal", message: `${u.username} is now ${u.role}` });
      close();
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: keys.users });
      void queryClient.invalidateQueries({ queryKey: keys.roles });
    },
  });
  const close = () => {
    setRole(undefined);
    change.reset();
    onClose();
  };
  return (
    <Modal opened={!!user} onClose={close} title={user && `Role of ${user.username}`}>
      {user && (
        <form
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            change.mutate(user);
          }}
        >
          <Stack>
            {change.error && <Alert color="red">{change.error.message}</Alert>}
            <RoleSelect value={role ?? user.role} onChange={setRole} />
            <Text size="xs" c="dimmed">
              The new permissions apply at once, also to the user&apos;s API keys.
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" loading={change.isPending}>
                Save
              </Button>
            </Group>
          </Stack>
        </form>
      )}
    </Modal>
  );
}

type Editing = { mode: "create" } | { mode: "edit" | "view"; role: Role };

/** Built-in and custom roles (#78, ADR-0058). */
export function RolesSection() {
  const roles = useQuery(rolesQuery);
  const can = useCan();
  const grantable = useGrantable();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<Editing>();
  const [deleting, setDeleting] = useState<Role>();
  const remove = useMutation({
    mutationFn: (r: Role) =>
      unwrapEmpty(api.DELETE("/api/v1/roles/{name}", { params: { path: { name: r.name } } })),
    onSuccess: (_d, r) => {
      setDeleting(undefined);
      notifications.show({ color: "teal", message: `Role ${r.name} deleted` });
    },
    onError: (error, r) =>
      notifications.show({
        color: "red",
        title: `Cannot delete ${r.name}`,
        message: error.message,
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.roles }),
  });
  const write = can("users:write");

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={3}>Roles</Title>
        {write && (
          <Button
            variant="light"
            leftSection={<IconPlus size={16} />}
            onClick={() => setEditing({ mode: "create" })}
          >
            Add role
          </Button>
        )}
      </Group>
      {roles.isPending && <Loader aria-label="Loading roles" />}
      {roles.isError && (
        <Alert color="red" title="Cannot list roles">
          {roles.error.message}
        </Alert>
      )}
      {roles.data && (
        <Table.ScrollContainer minWidth={560}>
          <Table aria-label="Roles">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Role</Table.Th>
                <Table.Th>Description</Table.Th>
                <Table.Th>Permissions</Table.Th>
                <Table.Th>Users</Table.Th>
                <Table.Th w={80}>
                  <VisuallyHidden>Actions</VisuallyHidden>
                </Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {roles.data.map((r) => {
                const editable = write && !r.builtin && grantable(r);
                return (
                  <Table.Tr key={r.name}>
                    <Table.Td>
                      <Group gap="xs" wrap="nowrap">
                        <Text size="sm" fw={500}>
                          {r.name}
                        </Text>
                        {r.builtin && (
                          <Badge size="xs" variant="outline" color="gray">
                            built-in
                          </Badge>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{r.description}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label={r.permissions.join(", ")} multiline w={320}>
                        <Text size="sm">{r.permissions.length}</Text>
                      </Tooltip>
                    </Table.Td>
                    <Table.Td>{r.users}</Table.Td>
                    <Table.Td>
                      <Group gap={4} wrap="nowrap">
                        {editable ? (
                          <ActionIcon
                            variant="subtle"
                            color="gray"
                            aria-label={`Edit ${r.name}`}
                            onClick={() => setEditing({ mode: "edit", role: r })}
                          >
                            <IconPencil size={16} />
                          </ActionIcon>
                        ) : (
                          <ActionIcon
                            variant="subtle"
                            color="gray"
                            aria-label={`View ${r.name}`}
                            onClick={() => setEditing({ mode: "view", role: r })}
                          >
                            <IconEye size={16} />
                          </ActionIcon>
                        )}
                        {editable && (
                          <Tooltip
                            label={
                              r.users > 0 ? "Give its users another role first" : `Delete ${r.name}`
                            }
                          >
                            <ActionIcon
                              variant="subtle"
                              color="red"
                              aria-label={`Delete ${r.name}`}
                              disabled={r.users > 0}
                              onClick={() => setDeleting(r)}
                            >
                              <IconTrash size={16} />
                            </ActionIcon>
                          </Tooltip>
                        )}
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                );
              })}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}
      <RoleModal editing={editing} onClose={() => setEditing(undefined)} />
      <Modal
        opened={!!deleting}
        onClose={() => setDeleting(undefined)}
        title={deleting && `Delete role ${deleting.name}?`}
      >
        {deleting && (
          <Stack>
            <Text size="sm">The role is removed. This cannot be undone.</Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setDeleting(undefined)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={remove.isPending}
                onClick={() => remove.mutate(deleting)}
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

function RoleModal({ editing, onClose }: { editing?: Editing; onClose: () => void }) {
  const title =
    editing?.mode === "create"
      ? "Add role"
      : editing && `${editing.mode === "edit" ? "Edit" : "Role"} ${editing.role.name}`;
  return (
    <Modal opened={!!editing} onClose={onClose} title={title} size="lg">
      {editing && (
        <RoleForm
          key={editing.mode === "create" ? "" : editing.role.name}
          editing={editing}
          onDone={onClose}
        />
      )}
    </Modal>
  );
}

function RoleForm({ editing, onDone }: { editing: Editing; onDone: () => void }) {
  const queryClient = useQueryClient();
  const can = useCan();
  const role = editing.mode === "create" ? undefined : editing.role;
  const [name, setName] = useState(role?.name ?? "");
  const [description, setDescription] = useState(role?.description ?? "");
  const [permissions, setPermissions] = useState<Permission[]>(
    (role?.permissions ?? []) as Permission[],
  );
  const save = useMutation({
    mutationFn: () => {
      const body = { description, permissions };
      return role
        ? unwrap(api.PUT("/api/v1/roles/{name}", { params: { path: { name: role.name } }, body }))
        : unwrap(api.POST("/api/v1/roles", { body: { ...body, name } }));
    },
    onSuccess: (r) => {
      notifications.show({ color: "teal", message: `Role ${r.name} saved` });
      onDone();
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.roles }),
  });
  const readOnly = editing.mode === "view";
  const badName = name.length > 0 && !ROLE_RE.test(name);
  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        if (!readOnly) save.mutate();
      }}
    >
      <Stack>
        {save.error && <Alert color="red">{save.error.message}</Alert>}
        {editing.mode === "create" && (
          <TextInput
            label="Name"
            required
            autoFocus
            value={name}
            error={
              badName ? "Lowercase letters, digits and '-', starting with a letter" : undefined
            }
            onChange={(e) => setName(e.currentTarget.value)}
          />
        )}
        <TextInput
          label="Description"
          maxLength={200}
          readOnly={readOnly}
          value={description}
          onChange={(e) => setDescription(e.currentTarget.value)}
        />
        <div>
          <Text size="sm" fw={500}>
            Permissions
          </Text>
          {!readOnly && (
            <Text size="xs" c="dimmed">
              You can give only permissions you have. Every role can see its own jobs.
            </Text>
          )}
          <PermissionPicker
            value={permissions}
            onChange={setPermissions}
            readOnly={readOnly}
            disabled={(p) => !can(p)}
          />
        </div>
        {!readOnly && (
          <Group justify="flex-end">
            <Button variant="default" onClick={onDone}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={!name || badName}>
              Save
            </Button>
          </Group>
        )}
      </Stack>
    </form>
  );
}
