// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Modal,
  Paper,
  PasswordInput,
  Stack,
  Text,
  Title,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys } from "../../api/queries";
import { BackupCodes, MFASetup } from "../../features/mfa/MFASetup";

/** Own two-factor authentication (#75, ADR-0037). */
export const Route = createFileRoute("/_app/security")({
  component: SecurityPage,
});

const mfaKey = ["auth", "mfa"] as const;

function SecurityPage() {
  const queryClient = useQueryClient();
  const status = useQuery({
    queryKey: mfaKey,
    queryFn: () => unwrap(api.GET("/api/v1/auth/mfa")),
  });
  const [mode, setMode] = useState<"setup" | "codes" | "disable" | null>(null);
  const [codes, setCodes] = useState<string[]>();
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: mfaKey });
    void queryClient.invalidateQueries({ queryKey: keys.me });
  };
  const close = () => {
    setMode(null);
    setCodes(undefined);
    refresh();
  };

  return (
    <Stack maw={720}>
      <Title order={2}>Account security</Title>
      <Paper withBorder p="md" component="section" aria-label="Two-factor authentication">
        <Stack gap="sm">
          <Group justify="space-between">
            <Title order={3}>Two-factor authentication</Title>
            {status.data &&
              (status.data.enabled ? (
                <Badge color="teal">On</Badge>
              ) : (
                <Badge color="gray">Off</Badge>
              ))}
          </Group>
          <Text size="sm" c="dimmed">
            After your password, nerdui asks for a code from an authenticator app. Backup codes let
            you in if you lose the phone.
          </Text>
          {status.isPending && <Loader aria-label="Loading" />}
          {status.isError && <Alert color="red">{status.error.message}</Alert>}
          {status.data && !status.data.enabled && (
            <Group>
              <Button onClick={() => setMode("setup")}>Set up</Button>
            </Group>
          )}
          {status.data?.enabled && (
            <>
              <Text size="sm">
                {status.data.backupCodesLeft} of 10 backup codes left.
                {status.data.backupCodesLeft < 3 && " Create new ones soon."}
              </Text>
              <Group>
                <Button variant="default" onClick={() => setMode("codes")}>
                  New backup codes
                </Button>
                <Button
                  variant="default"
                  color="red"
                  onClick={() => setMode("disable")}
                  disabled={status.data.required}
                  title={status.data.required ? "Required by your administrator" : undefined}
                >
                  Turn off
                </Button>
              </Group>
              {status.data.required && (
                <Text size="xs" c="dimmed">
                  Your administrator requires two-factor authentication.
                </Text>
              )}
            </>
          )}
        </Stack>
      </Paper>

      <Modal opened={mode === "setup"} onClose={close} title="Set up two-factor authentication">
        {mode === "setup" && <MFASetup onDone={close} />}
      </Modal>
      <Modal opened={mode === "codes"} onClose={close} title="New backup codes">
        {mode === "codes" &&
          (codes ? (
            <BackupCodes codes={codes} onDone={close} />
          ) : (
            <PasswordStep
              text="Your current backup codes stop working."
              action="Create codes"
              run={(password) =>
                unwrap(api.POST("/api/v1/auth/mfa/backup-codes", { body: { password } })).then(
                  (r) => setCodes(r.backupCodes),
                )
              }
            />
          ))}
      </Modal>
      <Modal opened={mode === "disable"} onClose={close} title="Turn off two-factor authentication">
        {mode === "disable" && (
          <PasswordStep
            text="Signing in will only need your password."
            action="Turn off"
            danger
            run={(password) =>
              unwrapEmpty(api.POST("/api/v1/auth/mfa/disable", { body: { password } })).then(() => {
                notifications.show({ message: "Two-factor authentication is off" });
                close();
              })
            }
          />
        )}
      </Modal>
    </Stack>
  );
}

function PasswordStep({
  text,
  action,
  danger,
  run,
}: {
  text: string;
  action: string;
  danger?: boolean;
  run: (password: string) => Promise<unknown>;
}) {
  const [password, setPassword] = useState("");
  const m = useMutation({ mutationFn: () => run(password) });
  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        m.mutate();
      }}
    >
      <Stack>
        <Text size="sm">{text}</Text>
        {m.error && <Alert color="red">{m.error.message}</Alert>}
        <PasswordInput
          label="Current password"
          autoComplete="current-password"
          required
          autoFocus
          value={password}
          onChange={(e) => setPassword(e.currentTarget.value)}
        />
        <Group justify="flex-end">
          <Button type="submit" color={danger ? "red" : undefined} loading={m.isPending}>
            {action}
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
