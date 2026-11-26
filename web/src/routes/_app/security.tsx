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
import { IconKey } from "@tabler/icons-react";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys } from "../../api/queries";
import type { components } from "../../api/schema";
import { BackupCodes, MFASetup } from "../../features/mfa/MFASetup";
import { PasskeyAdd, PasskeyList } from "../../features/mfa/Passkeys";

/** Own two-factor authentication (#75, ADR-0037) and passkeys (#226, ADR-0057). */
export const Route = createFileRoute("/_app/security")({
  component: SecurityPage,
});

const mfaKey = ["auth", "mfa"] as const;
const passkeysKey = ["auth", "passkeys"] as const;

type Passkey = components["schemas"]["Passkey"];

function SecurityPage() {
  const queryClient = useQueryClient();
  const status = useQuery({
    queryKey: mfaKey,
    queryFn: () => unwrap(api.GET("/api/v1/auth/mfa")),
  });
  const passkeys = useQuery({
    queryKey: passkeysKey,
    queryFn: () => unwrap(api.GET("/api/v1/auth/passkeys")),
  });
  const [mode, setMode] = useState<"setup" | "codes" | "disable" | "passkey" | null>(null);
  const [removing, setRemoving] = useState<Passkey>();
  const [codes, setCodes] = useState<string[]>();
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: mfaKey });
    void queryClient.invalidateQueries({ queryKey: passkeysKey });
    void queryClient.invalidateQueries({ queryKey: keys.me });
  };
  const close = () => {
    setMode(null);
    setRemoving(undefined);
    setCodes(undefined);
    refresh();
  };
  const st = status.data;
  // The administrator's requirement keeps the last second factor.
  const lastFactor = (passkeys: number, totp: boolean) =>
    st?.required && passkeys + (totp ? 1 : 0) <= 1 ? "Required by your administrator" : undefined;

  return (
    <Stack maw={720}>
      <Title order={2}>Account security</Title>
      <Paper withBorder p="md" component="section" aria-label="Two-factor authentication">
        <Stack gap="sm">
          <Group justify="space-between">
            <Title order={3}>Two-factor authentication</Title>
            {st && (st.enabled ? <Badge color="teal">On</Badge> : <Badge color="gray">Off</Badge>)}
          </Group>
          <Text size="sm" c="dimmed">
            After your password, nerdui asks for a code from an authenticator app or for a passkey.
            Backup codes let you in if you lose them.
          </Text>
          {status.isPending && <Loader aria-label="Loading" />}
          {status.isError && <Alert color="red">{status.error.message}</Alert>}
          {st && (
            <>
              <Group justify="space-between">
                <Text size="sm" fw={500}>
                  Authenticator app: {st.totp ? "on" : "off"}
                </Text>
                {st.totp ? (
                  <Button
                    variant="default"
                    color="red"
                    size="xs"
                    onClick={() => setMode("disable")}
                    disabled={!!lastFactor(st.passkeys, true)}
                    title={lastFactor(st.passkeys, true)}
                  >
                    Turn off
                  </Button>
                ) : (
                  <Button size="xs" onClick={() => setMode("setup")}>
                    Set up
                  </Button>
                )}
              </Group>
              {st.enabled && (
                <Group justify="space-between">
                  <Text size="sm">
                    {st.backupCodesLeft} of 10 backup codes left.
                    {st.backupCodesLeft < 3 && " Create new ones soon."}
                  </Text>
                  <Button variant="default" size="xs" onClick={() => setMode("codes")}>
                    New backup codes
                  </Button>
                </Group>
              )}
              {st.required && (
                <Text size="xs" c="dimmed">
                  Your administrator requires two-factor authentication.
                </Text>
              )}
            </>
          )}
        </Stack>
      </Paper>

      <Paper withBorder p="md" component="section" aria-label="Passkeys">
        <Stack gap="sm">
          <Group justify="space-between">
            <Title order={3}>Passkeys</Title>
            <Button
              size="xs"
              leftSection={<IconKey size={14} />}
              onClick={() => setMode("passkey")}
            >
              Add passkey
            </Button>
          </Group>
          <Text size="sm" c="dimmed">
            A passkey is a second factor, and signs you in without a password too.
          </Text>
          {passkeys.isError && <Alert color="red">{passkeys.error.message}</Alert>}
          {passkeys.data && st && (
            <PasskeyList
              passkeys={passkeys.data}
              onRemove={setRemoving}
              removeDisabled={lastFactor(st.passkeys, st.totp)}
            />
          )}
        </Stack>
      </Paper>

      <Modal opened={mode === "setup"} onClose={close} title="Set up an authenticator app">
        {mode === "setup" && <MFASetup onDone={close} />}
      </Modal>
      <Modal opened={mode === "passkey"} onClose={close} title="Add a passkey">
        {mode === "passkey" && <PasskeyAdd onDone={close} />}
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
      <Modal opened={mode === "disable"} onClose={close} title="Turn off the authenticator app">
        {mode === "disable" && (
          <PasswordStep
            text={
              st?.passkeys
                ? "Your passkeys remain your second factor."
                : "Signing in will only need your password."
            }
            action="Turn off"
            danger
            run={(password) =>
              unwrapEmpty(api.POST("/api/v1/auth/mfa/disable", { body: { password } })).then(() => {
                notifications.show({ message: "The authenticator app is off" });
                close();
              })
            }
          />
        )}
      </Modal>
      <Modal opened={!!removing} onClose={close} title={`Remove passkey ${removing?.name ?? ""}`}>
        {removing && (
          <PasswordStep
            text={
              st && !st.totp && st.passkeys <= 1
                ? "This is your last second factor: signing in will only need your password, and the backup codes stop working."
                : "The passkey stops working for nerdui. Remove it from your device too."
            }
            action="Remove"
            danger
            run={(password) =>
              unwrapEmpty(
                api.DELETE("/api/v1/auth/passkeys/{id}", {
                  params: { path: { id: removing.id } },
                  body: { password },
                }),
              ).then(() => {
                notifications.show({ message: `Passkey ${removing.name} removed` });
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
