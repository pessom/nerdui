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
  IconApi,
  IconUserShield,
  IconKey,
  IconLinkOff,
  IconPlayerPlay,
  IconPlayerStop,
  IconPlus,
  IconShieldOff,
  IconTrash,
} from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, usersQuery, type UserDetail } from "../../api/queries";
import { UserKeysModal } from "../../features/apikeys/UserKeysModal";
import { CreateUserModal, ResetPasswordModal } from "../../features/users/PasswordDialogs";
import { ChangeRoleModal, RolesSection } from "../../features/users/Roles";
import { allows } from "../../lib/permissions";
import { Time } from "../../components/Time";

/** Local users (#74, ADR-0016) and roles (#78, ADR-0058). */
export const Route = createFileRoute("/_app/users")({
  component: UsersPage,
});

type Pending = {
  kind: "disable" | "enable" | "delete" | "reset2fa" | "unlinkSso";
  user: UserDetail;
};

function UsersPage() {
  const { session } = Route.useRouteContext();
  const users = useQuery(usersQuery);
  const queryClient = useQueryClient();
  const [creating, { open: openCreate, close: closeCreate }] = useDisclosure();
  const [resetting, setResetting] = useState<UserDetail>();
  const [keysOf, setKeysOf] = useState<UserDetail>();
  const [roleOf, setRoleOf] = useState<UserDetail>();
  const write = allows(session, "users:write");
  const [pending, setPending] = useState<Pending>();

  const mutate = useMutation({
    mutationFn: async ({ kind, user }: Pending) => {
      const path = { params: { path: { id: user.id } } };
      if (kind === "delete") await unwrapEmpty(api.DELETE("/api/v1/users/{id}", path));
      else if (kind === "reset2fa")
        await unwrapEmpty(api.POST("/api/v1/users/{id}/mfa/reset", path));
      else if (kind === "unlinkSso") await unwrap(api.DELETE("/api/v1/users/{id}/oidc", path));
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
        {write && (
          <Button leftSection={<IconPlus size={16} />} onClick={openCreate}>
            Add user
          </Button>
        )}
      </Group>
      {users.isPending && <Loader aria-label="Loading" />}
      {users.isError && (
        <Alert color="red" title="Cannot list users">
          {users.error.message}
        </Alert>
      )}
      {users.data && (
        <Table.ScrollContainer minWidth={560}>
          <Table highlightOnHover aria-label="Users">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Username</Table.Th>
                <Table.Th>Role</Table.Th>
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
                  write={write}
                  onRole={() => setRoleOf(u)}
                  onReset={() => setResetting(u)}
                  onKeys={() => setKeysOf(u)}
                  onAction={(kind) => setPending({ kind, user: u })}
                />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}

      <CreateUserModal opened={creating} onClose={closeCreate} />
      <ResetPasswordModal user={resetting} onClose={() => setResetting(undefined)} />
      <UserKeysModal user={keysOf} onClose={() => setKeysOf(undefined)} />
      <ChangeRoleModal user={roleOf} onClose={() => setRoleOf(undefined)} />
      <Modal
        opened={!!pending}
        onClose={() => setPending(undefined)}
        title={pending && `${actionLabel[pending.kind]} ${pending.user.username}?`}
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
                {actionLabel[pending.kind]}
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
      <RolesSection />
    </Stack>
  );
}

const pastTense = {
  disable: "disabled",
  enable: "enabled",
  delete: "deleted",
  reset2fa: "can sign in with the password alone",
  unlinkSso: "is unlinked from the single sign-on provider",
};
const actionLabel = {
  disable: "Disable",
  enable: "Enable",
  delete: "Delete",
  reset2fa: "Reset two-factor for",
  unlinkSso: "Unlink single sign-on of",
};
const confirmText = {
  reset2fa:
    "The authenticator app, the passkeys and the backup codes are removed, e.g. after a lost phone. The user can set them up again.",
  disable: "The user is signed out everywhere and cannot sign in until enabled again.",
  enable: "The user can sign in again with their current password.",
  delete: "The account and its sessions are removed. This cannot be undone.",
  unlinkSso:
    "The link to the account at the single sign-on provider is removed. The next sign-in through the provider links the account with this username again.",
};

function UserRow({
  user: u,
  self,
  write,
  onRole,
  onReset,
  onKeys,
  onAction,
}: {
  user: UserDetail;
  self: boolean;
  write: boolean;
  onRole: () => void;
  onReset: () => void;
  onKeys: () => void;
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
        <Text size="sm">{u.role}</Text>
      </Table.Td>
      <Table.Td>
        <Group gap={4}>
          <Badge size="sm" variant="light" color={u.disabled ? "gray" : "teal"}>
            {u.disabled ? "disabled" : "active"}
          </Badge>
          {u.mfaEnabled && (
            <Badge size="sm" variant="light" color="blue">
              2FA
            </Badge>
          )}
          {u.sso && (
            <Badge size="sm" variant="light" color="grape">
              SSO
            </Badge>
          )}
          {u.mustChangePassword && (
            <Badge size="sm" variant="light" color="yellow">
              temporary password
            </Badge>
          )}
        </Group>
      </Table.Td>
      <Table.Td>
        <Time value={u.createdAt} />
      </Table.Td>
      <Table.Td>
        {u.lastLoginAt ? (
          <Time value={u.lastLoginAt} />
        ) : (
          <Text size="sm" c="dimmed">
            never
          </Text>
        )}
      </Table.Td>
      <Table.Td>
        {self || !write ? (
          <Tooltip
            label={
              self ? "Manage your own account from the user menu" : "Your role cannot change users"
            }
          >
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
              <Menu.Item leftSection={<IconUserShield size={16} />} onClick={onRole}>
                Change role
              </Menu.Item>
              <Menu.Item leftSection={<IconKey size={16} />} onClick={onReset}>
                Set temporary password
              </Menu.Item>
              <Menu.Item leftSection={<IconApi size={16} />} onClick={onKeys}>
                API keys
              </Menu.Item>
              {u.mfaEnabled && (
                <Menu.Item
                  leftSection={<IconShieldOff size={16} />}
                  onClick={() => onAction("reset2fa")}
                >
                  Reset two-factor
                </Menu.Item>
              )}
              {u.sso && (
                <Menu.Item
                  leftSection={<IconLinkOff size={16} />}
                  onClick={() => onAction("unlinkSso")}
                >
                  Unlink single sign-on
                </Menu.Item>
              )}
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
