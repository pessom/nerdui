// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Button,
  Code,
  Group,
  Loader,
  Modal,
  PasswordInput,
  Stack,
  Table,
  Text,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { notifications } from "@mantine/notifications";
import { IconLogout, IconPlus } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../../../api/client";
import { envStatusQuery, keys, registriesQuery, type Registry } from "../../../../api/queries";

/** Registry logins of the host (#51, ADR-0019), shared with `nerdctl login`. */
export const Route = createFileRoute("/_app/env/$env/registries")({
  component: RegistriesPage,
});

/** "https://index.docker.io/v1/" → "Docker Hub"; other keys as stored. */
function registryLabel(server: string): string {
  return /index\.docker\.io/.test(server) ? "Docker Hub" : server;
}

function RegistriesPage() {
  const { env } = Route.useParams();
  const list = useQuery(registriesQuery(env));
  const status = useQuery(envStatusQuery(env));
  const nerdctl = status.data?.runtime?.nerdctl;
  const readOnly = !!nerdctl && !nerdctl.compatible && !nerdctl.checkSkipped;
  const [adding, { open, close }] = useDisclosure();
  const [leaving, setLeaving] = useState<Registry>();

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>Registries</Title>
        <Tooltip label="Read-only mode: host nerdctl is incompatible" disabled={!readOnly}>
          <Button leftSection={<IconPlus size={16} />} onClick={open} disabled={readOnly}>
            Log in to registry
          </Button>
        </Tooltip>
      </Group>
      <Text size="sm" c="dimmed">
        Logins are stored in the docker config of the user running nerdui (as with{" "}
        <Code>sudo nerdctl login</Code>) and used for pulls, pushes and compose.
      </Text>
      {list.isPending && <Loader aria-label="Loading" />}
      {list.isError && (
        <Alert color="red" title="Cannot read registries">
          {list.error.message}
        </Alert>
      )}
      {list.data && (
        <Table striped verticalSpacing="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Registry</Table.Th>
              <Table.Th>Username</Table.Th>
              <Table.Th w={48} />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {list.data.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={3}>
                  <Text c="dimmed" ta="center" py="md">
                    Not logged in to any registry. Public images need no login.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {list.data.map((r) => (
              <Table.Tr key={r.server}>
                <Table.Td>
                  <Text size="sm" fw={500} title={r.server}>
                    {registryLabel(r.server)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm">{r.username || "—"}</Text>
                </Table.Td>
                <Table.Td>
                  <ActionIcon
                    variant="subtle"
                    color="gray"
                    aria-label={`Log out of ${registryLabel(r.server)}`}
                    disabled={readOnly}
                    onClick={() => setLeaving(r)}
                  >
                    <IconLogout size={16} />
                  </ActionIcon>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}
      <LoginDialog env={env} opened={adding} onClose={close} />
      <LogoutDialog env={env} registry={leaving} onClose={() => setLeaving(undefined)} />
    </Stack>
  );
}

function LoginDialog({
  env,
  opened,
  onClose,
}: {
  env: string;
  opened: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [server, setServer] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/registries", {
          params: { path: { env } },
          body: { server: server.trim(), username: username.trim(), password },
        }),
      ),
    onSuccess: (res) => {
      notifications.show({ color: "teal", message: `Logged in to ${registryLabel(res.server)}` });
      if (res.warning) {
        notifications.show({ color: "yellow", title: "Stored unencrypted", message: res.warning });
      }
      void queryClient.invalidateQueries({ queryKey: keys.registries(env) });
      close();
    },
  });
  const close = () => {
    setServer("");
    setUsername("");
    setPassword("");
    login.reset();
    onClose();
  };
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (server.trim() && username.trim() && password) login.mutate();
  };
  return (
    <Modal opened={opened} onClose={close} title="Log in to registry">
      <form onSubmit={onSubmit}>
        <Stack>
          {login.error && <Alert color="red">{login.error.message}</Alert>}
          <TextInput
            label="Registry"
            description="Host and port, e.g. ghcr.io or registry.local:5000; docker.io for Docker Hub"
            required
            autoFocus
            autoComplete="off"
            value={server}
            onChange={(e) => setServer(e.currentTarget.value)}
          />
          <TextInput
            label="Username"
            required
            autoComplete="off"
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
          />
          <PasswordInput
            label="Password or access token"
            required
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
          <Text size="xs" c="dimmed">
            The registry checks the credentials before they are saved.
          </Text>
          <Button
            type="submit"
            loading={login.isPending}
            disabled={!server.trim() || !username.trim() || !password}
          >
            Log in
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}

function LogoutDialog({
  env,
  registry,
  onClose,
}: {
  env: string;
  registry?: Registry;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const logout = useMutation({
    mutationFn: (server: string) =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/registries", {
          params: { path: { env }, query: { server } },
        }),
      ),
    onSuccess: (_d, server) => {
      notifications.show({ color: "teal", message: `Logged out of ${registryLabel(server)}` });
      void queryClient.invalidateQueries({ queryKey: keys.registries(env) });
      onClose();
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Logout failed", message: e.message }),
  });
  return (
    <Modal
      opened={!!registry}
      onClose={onClose}
      title={registry && `Log out of ${registryLabel(registry.server)}?`}
    >
      <Stack>
        <Text size="sm">
          The stored credentials are removed from the host; pulls and pushes of private images from
          this registry fail until you log in again, also for <Code>nerdctl</Code> on the host.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button
            color="red"
            loading={logout.isPending}
            onClick={() => registry && logout.mutate(registry.server)}
          >
            Log out
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
