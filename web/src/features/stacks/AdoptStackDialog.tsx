// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Badge,
  Button,
  Code,
  Divider,
  Group,
  Loader,
  Modal,
  Paper,
  SegmentedControl,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { stackCandidatesQuery } from "../../api/queries";

type Mode = "in-place" | "copy";

/**
 * Adopt an existing compose project (#25): a project found in the scan directories, or any
 * directory typed in. In place keeps the files where they are; copy moves the compose and .env
 * files into the stacks directory.
 */
export function AdoptStackDialog({
  env,
  namespace,
  opened,
  onClose,
  initialName = "",
}: {
  env: string;
  namespace: string;
  opened: boolean;
  onClose: () => void;
  /** Name of an unmanaged project to adopt. */
  initialName?: string;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const candidates = useQuery({ ...stackCandidatesQuery(env), enabled: opened });
  const [name, setName] = useState(initialName);
  const [path, setPath] = useState("");
  const [mode, setMode] = useState<Mode>("in-place");
  const close = () => {
    setName(initialName);
    setPath("");
    setMode("in-place");
    adopt.reset();
    onClose();
  };
  const adopt = useMutation({
    mutationFn: (req: { name: string; path: string; mode: Mode }) =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks/adopt", {
          params: { path: { env }, query: { namespace } },
          body: req,
        }),
      ),
    onSuccess: (s) => {
      notifications.show({ color: "teal", message: `Stack ${s.name} adopted` });
      void queryClient.invalidateQueries({ queryKey: ["env", env] });
      close();
      void navigate({
        to: "/env/$env/stacks/$name",
        params: { env, name: s.name },
        search: { ns: namespace },
      });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    adopt.mutate({ name: name.trim(), path: path.trim(), mode });
  };
  const found = candidates.data?.candidates ?? [];
  const scanDirs = candidates.data?.scanDirs ?? [];

  return (
    <Modal opened={opened} onClose={close} title="Adopt a compose project" size="lg">
      <Stack gap="sm">
        <Text size="sm" c="dimmed">
          The project&apos;s containers keep running; nerdui starts managing its compose file.
        </Text>
        {adopt.isError && (
          <Alert color="red" title="Cannot adopt">
            {adopt.error.message}
          </Alert>
        )}
        <Text fw={500} size="sm">
          Found in scan directories
        </Text>
        {candidates.isPending && <Loader size="sm" aria-label="Scanning" />}
        {candidates.isError && (
          <Alert color="red" title="Cannot scan">
            {candidates.error.message}
          </Alert>
        )}
        {candidates.data && scanDirs.length === 0 && (
          <Text size="sm" c="dimmed">
            No scan directories: start nerdui with <Code>--stacks-scan-dirs</Code> to find projects
            automatically, or enter a directory below.
          </Text>
        )}
        {candidates.data && scanDirs.length > 0 && found.length === 0 && (
          <Text size="sm" c="dimmed">
            No new compose projects in {scanDirs.join(", ")}.
          </Text>
        )}
        {found.map((c) => {
          const blocked = c.taken || !!c.error;
          return (
            <Paper key={c.path} withBorder p="xs" component="section" aria-label={c.path}>
              <Group justify="space-between" wrap="nowrap" align="flex-start">
                <div style={{ minWidth: 0 }}>
                  <Group gap={6}>
                    <Text fw={500} size="sm">
                      {c.name}
                    </Text>
                    {c.taken && (
                      <Badge size="xs" color="orange" variant="light">
                        name taken
                      </Badge>
                    )}
                  </Group>
                  <Text size="xs" c="dimmed" ff="monospace" style={{ wordBreak: "break-all" }}>
                    {c.path}/{c.composeFile}
                  </Text>
                  {c.error ? (
                    <Text size="xs" c="red">
                      {c.error}
                    </Text>
                  ) : (
                    <Text size="xs">services: {c.services.join(", ") || "—"}</Text>
                  )}
                </div>
                <Group gap={4} wrap="nowrap">
                  <Button
                    size="compact-xs"
                    disabled={blocked}
                    loading={adopt.isPending && adopt.variables?.path === c.path}
                    onClick={() => adopt.mutate({ name: c.name, path: c.path, mode: "in-place" })}
                  >
                    In place
                  </Button>
                  <Tooltip
                    label={
                      c.localPaths?.length
                        ? `Uses files of its directory: ${c.localPaths.join(", ")}`
                        : "Copy the compose and .env files into the stacks directory"
                    }
                    multiline
                    w={320}
                  >
                    <Button
                      size="compact-xs"
                      variant="default"
                      disabled={blocked || !!c.localPaths?.length}
                      onClick={() => adopt.mutate({ name: c.name, path: c.path, mode: "copy" })}
                    >
                      Copy
                    </Button>
                  </Tooltip>
                </Group>
              </Group>
            </Paper>
          );
        })}
        <Divider label="or enter a directory" labelPosition="center" />
        <form onSubmit={submit}>
          <Stack gap="sm">
            <Group grow align="flex-start">
              <TextInput
                label="Stack name"
                description="The compose project name of its containers"
                required
                value={name}
                onChange={(e) => setName(e.currentTarget.value)}
              />
              <TextInput
                label="Directory"
                description="Absolute path with a compose file"
                placeholder="/srv/apps/blog"
                required
                value={path}
                onChange={(e) => setPath(e.currentTarget.value)}
              />
            </Group>
            <Group justify="space-between">
              <SegmentedControl
                size="xs"
                aria-label="Mode"
                value={mode}
                onChange={(v) => setMode(v as Mode)}
                data={[
                  { value: "in-place", label: "In place" },
                  { value: "copy", label: "Copy files" },
                ]}
              />
              <Button
                type="submit"
                loading={adopt.isPending && adopt.variables?.path === path.trim()}
                disabled={!name.trim() || !path.trim()}
              >
                Adopt
              </Button>
            </Group>
          </Stack>
        </form>
      </Stack>
    </Modal>
  );
}
