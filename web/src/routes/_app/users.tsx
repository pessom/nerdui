// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Menu,
  Modal,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
  VisuallyHidden,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { notifications } from "@mantine/notifications";
import {
  IconDots,
  IconKey,
  IconPlayerPlay,
  IconPlayerStop,
  IconPlus,
  IconTrash,
} from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, usersQuery, type UserDetail } from "../../api/queries";
import { CreateUserModal, ResetPasswordModal } from "../../features/users/PasswordDialogs";
import { timeAgo } from "../../lib/time";

/** Local users (#74, ADR-0016). */
export const Route = createFileRoute("/_app/users")({
  component: UsersPage,
});

type Pending = { kind: "disable" | "enable" | "delete"; user: UserDetail };

function UsersPage() {
  const { session } = Route.useRouteContext();
  const users = useQuery(usersQuery);
  const queryClient = useQueryClient();
  const [creating, { open: openCreate, close: closeCreate }] = useDisclosure();
  const [resetting, setResetting] = useState<UserDetail>();
  const [pending, setPending] = useState<Pending>();

  const mutate = useMutation({
    mutationFn: async ({ kind, user }: Pending) => {
      const path = { params: { path: { id: user.id } } };
      if (kind === "delete") await unwrapEmpty(api.DELETE("/api/v1/users/{id}", path));
      else
        await unwrap(
          api.PATCH("/api/v1/users/{id}", { ...path, body: { disabled: kind === "disable" } }),
        );
    },
    onSuccess: (_d, { kind, user }) => {
      setPending(undefined);
      notifications.show({ color: "teal", message: `${user.username} ${pastTense[kind]}` });
    },
    onError: (error, { kind, user }) =>
      notifications.show({
        color: "red",
        title: `Cannot ${kind} ${user.username}`,
        message: error.message,
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.users }),
  });

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Users</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={openCreate}>
          Add user
        </Button>
      </Group>
      {users.isPending && <Loader aria-label="Loading" />}
      {users.isError && (
        <Alert color="red" title="Cannot list users">
          {users.error.message}
        </Alert>
      )}
      {users.data && (
        <Table.ScrollContainer minWidth={560}>
          <Table highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Username</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Created</Table.Th>
                <Table.Th>Last sign-in</Table.Th>
                <Table.Th w={48}>
                  <VisuallyHidden>Actions</VisuallyHidden>
                </Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {users.data.map((u) => (
                <UserRow
                  key={u.id}
                  user={u}
                  self={u.id === session.user.id}
                  onReset={() => setResetting(u)}
                  onAction={(kind) => setPending({ kind, user: u })}
                />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}

      <CreateUserModal opened={creating} onClose={closeCreate} />
      <ResetPasswordModal user={resetting} onClose={() => setResetting(undefined)} />
      <Modal
        opened={!!pending}
        onClose={() => setPending(undefined)}
        title={pending && `${capitalize(pending.kind)} ${pending.user.username}?`}
      >
        {pending && (
          <Stack>
            <Text size="sm">{confirmText[pending.kind]}</Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setPending(undefined)}>
                Cancel
              </Button>
              <Button
                color={pending.kind === "enable" ? "blue" : "red"}
                loading={mutate.isPending}
                onClick={() => mutate.mutate(pending)}
              >
                {capitalize(pending.kind)}
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Stack>
  );
}

const pastTense = { disable: "disabled", enable: "enabled", delete: "deleted" };
const confirmText = {
  disable: "The user is signed out everywhere and cannot sign in until enabled again.",
  enable: "The user can sign in again with their current password.",
  delete: "The account and its sessions are removed. This cannot be undone.",
};

function capitalize(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function UserRow({
  user: u,
  self,
  onReset,
  onAction,
}: {
  user: UserDetail;
  self: boolean;
  onReset: () => void;
  onAction: (kind: Pending["kind"]) => void;
}) {
  return (
    <Table.Tr>
      <Table.Td>
        <Group gap="xs">
          <Text size="sm" fw={500}>
            {u.username}
          </Text>
          {self && (
            <Badge size="xs" variant="outline">
              you
            </Badge>
          )}
        </Group>
      </Table.Td>
      <Table.Td>
        <Group gap={4}>
          <Badge size="sm" variant="light" color={u.disabled ? "gray" : "teal"}>
            {u.disabled ? "disabled" : "active"}
          </Badge>
          {u.mustChangePassword && (
            <Badge size="sm" variant="light" color="yellow">
              temporary password
            </Badge>
          )}
        </Group>
      </Table.Td>
      <Table.Td>
        <Text size="sm" title={u.createdAt}>
          {timeAgo(u.createdAt)}
        </Text>
      </Table.Td>
      <Table.Td>
        <Text size="sm" c={u.lastLoginAt ? undefined : "dimmed"} title={u.lastLoginAt}>
          {u.lastLoginAt ? timeAgo(u.lastLoginAt) : "never"}
        </Text>
      </Table.Td>
      <Table.Td>
        {self ? (
          <Tooltip label="Manage your own account from the user menu">
            <ActionIcon
              variant="subtle"
              color="gray"
              disabled
              aria-label={`Actions for ${u.username}`}
            >
              <IconDots size={16} />
            </ActionIcon>
          </Tooltip>
        ) : (
          <Menu position="bottom-end" withArrow>
            <Menu.Target>
              <ActionIcon variant="subtle" color="gray" aria-label={`Actions for ${u.username}`}>
                <IconDots size={16} />
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item leftSection={<IconKey size={16} />} onClick={onReset}>
                Set temporary password
              </Menu.Item>
              {u.disabled ? (
                <Menu.Item
                  leftSection={<IconPlayerPlay size={16} />}
                  onClick={() => onAction("enable")}
                >
                  Enable
                </Menu.Item>
              ) : (
                <Menu.Item
                  leftSection={<IconPlayerStop size={16} />}
                  onClick={() => onAction("disable")}
                >
                  Disable
                </Menu.Item>
              )}
              <Menu.Divider />
              <Menu.Item
                color="red"
                leftSection={<IconTrash size={16} />}
                onClick={() => onAction("delete")}
              >
                Delete
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        )}
      </Table.Td>
    </Table.Tr>
  );
}
