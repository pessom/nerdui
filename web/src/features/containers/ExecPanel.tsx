// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Paper,
  Select,
  Stack,
  Text,
  TextInput,
  useComputedColorScheme,
} from "@mantine/core";
import { IconPlayerPlay, IconPlayerStop } from "@tabler/icons-react";
import { Suspense, lazy, useCallback, useState } from "react";
import type { ExecState } from "./ExecTerminal";
import { execPath, splitCommand } from "./exec";

const ExecTerminal = lazy(() => import("./ExecTerminal"));

const shells = [
  { value: "", label: "Auto (bash, else sh)" },
  { value: "/bin/bash", label: "/bin/bash" },
  { value: "/bin/sh", label: "/bin/sh" },
  { value: "/bin/ash", label: "/bin/ash" },
  { value: "custom", label: "Custom command…" },
];

/** Terminal tab of the container page (#15). */
export function ExecPanel({
  env,
  id,
  namespace,
  running,
  enabled,
}: {
  env: string;
  id: string;
  namespace: string;
  running: boolean;
  /** False when the server runs with --disable-exec. */
  enabled: boolean;
}) {
  const dark = useComputedColorScheme("light") === "dark";
  const [shell, setShell] = useState("");
  const [custom, setCustom] = useState("");
  const [user, setUser] = useState("");
  const [workdir, setWorkdir] = useState("");
  // A new key starts a new session.
  const [session, setSession] = useState<{ key: number; path: string }>();
  const [state, setState] = useState<ExecState>({ status: "closed" });
  const onState = useCallback((s: ExecState) => setState(s), []);

  if (!enabled) {
    return (
      <Alert color="gray" title="Terminal disabled">
        The web terminal is turned off on this server (--disable-exec).
      </Alert>
    );
  }
  if (!running) {
    return (
      <Alert color="gray" title="Container is not running">
        A terminal can be opened while the container runs.
      </Alert>
    );
  }
  const cmd = shell === "custom" ? splitCommand(custom) : shell ? [shell] : [];
  const connect = () =>
    setSession((s) => ({
      key: (s?.key ?? 0) + 1,
      path: execPath(env, id, namespace, { cmd, user: user.trim(), workdir: workdir.trim() }),
    }));
  const live = session && (state.status === "open" || state.status === "connecting");

  return (
    <Stack gap="xs">
      <Group gap="xs" align="flex-end" wrap="wrap">
        <Select
          label="Command"
          size="xs"
          w={200}
          data={shells}
          value={shell}
          onChange={(v) => setShell(v ?? "")}
          allowDeselect={false}
        />
        {shell === "custom" && (
          <TextInput
            label="Custom command"
            size="xs"
            w={220}
            placeholder="e.g. psql -U postgres"
            value={custom}
            onChange={(e) => setCustom(e.currentTarget.value)}
          />
        )}
        <TextInput
          label="User"
          size="xs"
          w={140}
          placeholder="container default"
          value={user}
          onChange={(e) => setUser(e.currentTarget.value)}
        />
        <TextInput
          label="Working directory"
          size="xs"
          w={180}
          placeholder="container default"
          value={workdir}
          onChange={(e) => setWorkdir(e.currentTarget.value)}
        />
        {live ? (
          <Button
            size="xs"
            color="red"
            variant="light"
            leftSection={<IconPlayerStop size={14} />}
            onClick={() => {
              setSession(undefined);
              setState({ status: "closed" });
            }}
          >
            Disconnect
          </Button>
        ) : (
          <Button
            size="xs"
            leftSection={<IconPlayerPlay size={14} />}
            onClick={connect}
            disabled={shell === "custom" && cmd.length === 0}
          >
            {session ? "New session" : "Connect"}
          </Button>
        )}
        <Badge
          variant="light"
          color={
            state.status === "open"
              ? "teal"
              : state.status === "error"
                ? "red"
                : state.status === "exited" && state.exitCode !== 0
                  ? "orange"
                  : "gray"
          }
        >
          {state.status === "exited" ? `exited (${state.exitCode})` : state.status}
        </Badge>
      </Group>
      <Text size="xs" c="dimmed">
        The process ends when you disconnect or leave the page. Sessions are logged with your user
        name.
      </Text>
      <Paper withBorder p={4} h="60vh" style={{ overflow: "hidden" }}>
        {session ? (
          <Suspense fallback={<Loader aria-label="Loading terminal" />}>
            <ExecTerminal key={session.key} path={session.path} dark={dark} onState={onState} />
          </Suspense>
        ) : (
          <Text c="dimmed" ta="center" mt="xl">
            Choose a command and connect.
          </Text>
        )}
      </Paper>
    </Stack>
  );
}
