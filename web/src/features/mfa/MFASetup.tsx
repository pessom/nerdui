// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Code,
  CopyButton,
  Group,
  Image,
  PasswordInput,
  PinInput,
  SimpleGrid,
  Stack,
  Text,
} from "@mantine/core";
import { useMutation } from "@tanstack/react-query";
import QRCode from "qrcode";
import { useEffect, useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";

type Enrollment = { secret: string; uri: string };

/** Two-factor setup (#75): password, then the QR code, a code from the app, and backup codes. */
export function MFASetup({ onDone }: { onDone: () => void }) {
  const [password, setPassword] = useState("");
  const [enrollment, setEnrollment] = useState<Enrollment>();
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[]>();

  const start = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/auth/mfa/setup", { body: { password } })),
    onSuccess: setEnrollment,
  });
  const enable = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/auth/mfa/enable", { body: { code } })),
    onSuccess: (r) => setCodes(r.backupCodes),
  });

  if (codes) return <BackupCodes codes={codes} onDone={onDone} />;

  if (!enrollment) {
    return (
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          start.mutate();
        }}
      >
        <Stack>
          <Text size="sm">
            Use an authenticator app such as Aegis, 2FAS, Google Authenticator or 1Password. Enter
            your password to begin.
          </Text>
          {start.error && <Alert color="red">{start.error.message}</Alert>}
          <PasswordInput
            label="Current password"
            autoComplete="current-password"
            required
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
          <Group justify="flex-end">
            <Button type="submit" loading={start.isPending}>
              Continue
            </Button>
          </Group>
        </Stack>
      </form>
    );
  }

  return (
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        enable.mutate();
      }}
    >
      <Stack>
        <Text size="sm">
          Scan the QR code with your authenticator app, or enter the key by hand.
        </Text>
        <Group align="flex-start" wrap="wrap">
          <QR value={enrollment.uri} />
          <Stack gap={4}>
            <Text size="sm" fw={500}>
              Key
            </Text>
            <Code fz="sm" style={{ wordBreak: "break-all" }} maw={280}>
              {enrollment.secret.match(/.{1,4}/g)?.join(" ")}
            </Code>
            <CopyButton value={enrollment.secret}>
              {({ copied, copy }) => (
                <Button size="compact-xs" variant="subtle" onClick={copy}>
                  {copied ? "Copied" : "Copy key"}
                </Button>
              )}
            </CopyButton>
          </Stack>
        </Group>
        {enable.error && <Alert color="red">{enable.error.message}</Alert>}
        <Stack gap={4}>
          <Text size="sm" fw={500}>
            Code from the app
          </Text>
          <PinInput
            length={6}
            type="number"
            oneTimeCode
            aria-label="Code from the app"
            value={code}
            onChange={setCode}
            onComplete={() => undefined}
          />
        </Stack>
        <Group justify="flex-end">
          <Button type="submit" loading={enable.isPending} disabled={code.length !== 6}>
            Turn on
          </Button>
        </Group>
      </Stack>
    </form>
  );
}

function QR({ value }: { value: string }) {
  const [src, setSrc] = useState<string>();
  useEffect(() => {
    let live = true;
    QRCode.toDataURL(value, { margin: 1, width: 180 })
      .then((url) => live && setSrc(url))
      .catch(() => live && setSrc(undefined));
    return () => {
      live = false;
    };
  }, [value]);
  return src ? (
    <Image src={src} w={180} h={180} alt="QR code for the authenticator app" />
  ) : (
    <div style={{ width: 180, height: 180 }} />
  );
}

/** Backup codes, shown once (#75). */
export function BackupCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const text = codes.join("\n");
  const download = () => {
    const url = URL.createObjectURL(new Blob([text + "\n"], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "nerdui-backup-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  };
  return (
    <Stack>
      <Alert color="yellow" title="Save your backup codes">
        Each code signs you in once if you lose your phone. They are shown only now.
      </Alert>
      <SimpleGrid cols={2} spacing="xs" aria-label="Backup codes">
        {codes.map((c) => (
          <Code key={c} fz="md" ta="center">
            {c}
          </Code>
        ))}
      </SimpleGrid>
      <Group justify="space-between">
        <Group gap="xs">
          <CopyButton value={text}>
            {({ copied, copy }) => (
              <Button variant="default" size="xs" onClick={copy}>
                {copied ? "Copied" : "Copy"}
              </Button>
            )}
          </CopyButton>
          <Button variant="default" size="xs" onClick={download}>
            Download
          </Button>
        </Group>
        <Button onClick={onDone}>I saved them</Button>
      </Group>
    </Stack>
  );
}
