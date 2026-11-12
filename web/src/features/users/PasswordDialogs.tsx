// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Button,
  Code,
  CopyButton,
  Group,
  Modal,
  PasswordInput,
  SegmentedControl,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { IconCheck, IconCopy } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { keys, type UserDetail } from "../../api/queries";
import { MIN_PASSWORD, USERNAME_RE, tooShort } from "./password";

type Mode = "generate" | "manual";

/** Choice between a generated and a typed temporary password. */
function TemporaryPassword({
  mode,
  setMode,
  password,
  setPassword,
}: {
  mode: Mode;
  setMode: (m: Mode) => void;
  password: string;
  setPassword: (p: string) => void;
}) {
  return (
    <Stack gap="xs">
      <SegmentedControl
        aria-label="Temporary password"
        value={mode}
        onChange={(v) => setMode(v as Mode)}
        data={[
          { value: "generate", label: "Generate" },
          { value: "manual", label: "Enter manually" },
        ]}
      />
      {mode === "manual" && (
        <PasswordInput
          label="Temporary password"
          description={`At least ${MIN_PASSWORD} characters`}
          required
          autoComplete="new-password"
          value={password}
          error={tooShort(password) ? `Use at least ${MIN_PASSWORD} characters` : undefined}
          onChange={(e) => setPassword(e.currentTarget.value)}
        />
      )}
      <Text size="xs" c="dimmed">
        The user must replace it with their own password at the next sign-in.
      </Text>
    </Stack>
  );
}

/** Shows a generated password once, with a copy button. */
function GeneratedPassword({ username, password }: { username: string; password: string }) {
  return (
    <Stack gap="xs">
      <Text size="sm">
        Temporary password for <b>{username}</b>. It is shown only once: pass it on securely.
      </Text>
      <Group gap="xs" wrap="nowrap">
        <Code fz="md" style={{ flex: 1, wordBreak: "break-all" }} aria-label="Generated password">
          {password}
        </Code>
        <CopyButton value={password}>
          {({ copied, copy }) => (
            <Tooltip label={copied ? "Copied" : "Copy"}>
              <ActionIcon
                variant="subtle"
                color={copied ? "teal" : "gray"}
                onClick={copy}
                aria-label="Copy password"
              >
                {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
              </ActionIcon>
            </Tooltip>
          )}
        </CopyButton>
      </Group>
    </Stack>
  );
}

export function CreateUserModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [mode, setMode] = useState<Mode>("generate");
  const [password, setPassword] = useState("");
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/users", {
          body: { username, ...(mode === "manual" ? { password } : {}) },
        }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.users }),
  });
  const close = () => {
    setUsername("");
    setMode("generate");
    setPassword("");
    create.reset();
    onClose();
  };
  const badName = username.length > 0 && !USERNAME_RE.test(username);
  const invalid =
    !username || badName || (mode === "manual" && [...password].length < MIN_PASSWORD);
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!invalid) create.mutate();
  };

  return (
    <Modal opened={opened} onClose={close} title="Add user">
      {create.data ? (
        <Stack>
          {create.data.generatedPassword ? (
            <GeneratedPassword username={username} password={create.data.generatedPassword} />
          ) : (
            <Text size="sm">
              User <b>{username}</b> created. They must change the temporary password at the first
              sign-in.
            </Text>
          )}
          <Button onClick={close}>Done</Button>
        </Stack>
      ) : (
        <form onSubmit={onSubmit}>
          <Stack>
            {create.error && <Alert color="red">{create.error.message}</Alert>}
            <Text size="xs" c="dimmed">
              Every user is an administrator until roles are available.
            </Text>
            <TextInput
              label="Username"
              required
              autoFocus
              autoComplete="off"
              value={username}
              error={badName ? "Letters, digits, '.', '_' or '-' (up to 64)" : undefined}
              onChange={(e) => setUsername(e.currentTarget.value)}
            />
            <TemporaryPassword
              mode={mode}
              setMode={setMode}
              password={password}
              setPassword={setPassword}
            />
            <Button type="submit" loading={create.isPending} disabled={invalid}>
              Create user
            </Button>
          </Stack>
        </form>
      )}
    </Modal>
  );
}

export function ResetPasswordModal({
  user,
  onClose,
}: {
  user: UserDetail | undefined;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<Mode>("generate");
  const [password, setPassword] = useState("");
  const reset = useMutation({
    mutationFn: (id: number) =>
      unwrap(
        api.POST("/api/v1/users/{id}/password", {
          params: { path: { id } },
          body: mode === "manual" ? { password } : {},
        }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.users }),
  });
  const close = () => {
    setMode("generate");
    setPassword("");
    reset.reset();
    onClose();
  };
  const invalid = mode === "manual" && [...password].length < MIN_PASSWORD;
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (user && !invalid) reset.mutate(user.id);
  };

  return (
    <Modal opened={!!user} onClose={close} title={`Set temporary password for ${user?.username}`}>
      {reset.data ? (
        <Stack>
          {reset.data.generatedPassword ? (
            <GeneratedPassword
              username={user?.username ?? ""}
              password={reset.data.generatedPassword}
            />
          ) : (
            <Text size="sm">Temporary password set.</Text>
          )}
          <Button onClick={close}>Done</Button>
        </Stack>
      ) : (
        <form onSubmit={onSubmit}>
          <Stack>
            {reset.error && <Alert color="red">{reset.error.message}</Alert>}
            <Text size="sm">
              {user?.username} is signed out everywhere and must choose a new password at the next
              sign-in.
            </Text>
            <TemporaryPassword
              mode={mode}
              setMode={setMode}
              password={password}
              setPassword={setPassword}
            />
            <Button type="submit" loading={reset.isPending} disabled={invalid}>
              Set password
            </Button>
          </Stack>
        </form>
      )}
    </Modal>
  );
}
