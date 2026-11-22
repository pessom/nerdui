// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Code,
  Group,
  Paper,
  PasswordInput,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconDownload } from "@tabler/icons-react";
import { useMutation } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { ApiError, api, type ErrorBody } from "../../api/client";
import { attachmentName } from "../../lib/download";

const MIN_PASSWORD = 12;

/** Downloads a password-encrypted backup of nerdui's data (#89, ADR-0047). */
export function BackupSettingsCard() {
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const create = useMutation({
    mutationFn: async () => {
      const { data, error, response } = await api.POST("/api/v1/backup", {
        body: { password },
        parseAs: "blob",
      });
      if (!response.ok || !data) {
        const body = error as Partial<ErrorBody> | undefined;
        throw new ApiError(
          response.status,
          body?.error?.code ?? "unknown",
          body?.error?.message ?? `${response.status} ${response.statusText}`,
        );
      }
      const url = URL.createObjectURL(data);
      const a = document.createElement("a");
      a.href = url;
      a.download = attachmentName(
        response.headers.get("Content-Disposition"),
        "nerdui-backup.tar.gz.age",
      );
      document.body.append(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 10_000);
    },
    onSuccess: () => {
      setPassword("");
      setRepeat("");
      notifications.show({ color: "teal", message: "Backup downloaded" });
    },
  });
  const short = password.length > 0 && [...password].length < MIN_PASSWORD;
  const mismatch = repeat.length > 0 && repeat !== password;
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!short && !mismatch && password && repeat === password) create.mutate();
  };
  return (
    <Paper withBorder p="md" component="section" aria-label="Backup">
      <form onSubmit={onSubmit}>
        <Stack gap="sm">
          <Title order={3}>Backup</Title>
          <Text size="sm">
            Downloads nerdui&apos;s own data: users, settings, credentials, notification channels,
            webhooks and the files of managed and Git stacks. Containers, images and volumes are not
            included. The archive is encrypted with the password; without it the backup cannot be
            restored.
          </Text>
          {create.error && <Alert color="red">{create.error.message}</Alert>}
          <Group grow align="flex-start">
            <PasswordInput
              label="Backup password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
              error={short ? `At least ${MIN_PASSWORD} characters` : undefined}
            />
            <PasswordInput
              label="Repeat password"
              autoComplete="new-password"
              value={repeat}
              onChange={(e) => setRepeat(e.currentTarget.value)}
              error={mismatch ? "Passwords do not match" : undefined}
            />
          </Group>
          <Group>
            <Button
              type="submit"
              leftSection={<IconDownload size={16} />}
              loading={create.isPending}
              disabled={!password || short || repeat !== password}
            >
              Download backup
            </Button>
          </Group>
          <Text size="xs" c="dimmed">
            To restore, stop nerdui on the target host and run{" "}
            <Code>nerdui admin restore nerdui-backup-….tar.gz.age</Code>, then start it again.
          </Text>
        </Stack>
      </form>
    </Paper>
  );
}
