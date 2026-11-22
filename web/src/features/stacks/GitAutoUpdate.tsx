// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Code,
  CopyButton,
  Group,
  Select,
  Stack,
  Text,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconCheck, IconCopy } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, type Stack as StackT } from "../../api/queries";
import type { components } from "../../api/schema";
import { formatTime, usePreferences } from "../preferences/context";
import { absoluteURL } from "../../lib/base";

type GitInfo = NonNullable<StackT["git"]>;
type AutoRequest = components["schemas"]["GitAutoRequest"];

const modes = [
  { value: "off", label: "Off" },
  { value: "notify", label: "Notify about new commits" },
  { value: "deploy", label: "Pull and redeploy" },
];

const intervals = [
  { value: "0", label: "No polling (webhook only)" },
  { value: "60", label: "Every minute" },
  { value: "300", label: "Every 5 minutes" },
  { value: "900", label: "Every 15 minutes" },
  { value: "3600", label: "Every hour" },
  { value: "21600", label: "Every 6 hours" },
  { value: "86400", label: "Every day" },
];

/**
 * Automatic updates of a Git stack (#38, #39, ADR-0040): polling, the webhook, and what happens
 * on a new commit.
 */
export function GitAutoUpdate({
  env,
  namespace,
  name,
  git,
}: {
  env: string;
  namespace: string;
  name: string;
  git: GitInfo;
}) {
  const queryClient = useQueryClient();
  const { prefs } = usePreferences();
  const [hookURL, setHookURL] = useState<string | null>(null);
  const refresh = () =>
    void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
  const path = { params: { path: { env, name } } };

  const save = useMutation({
    mutationFn: (body: AutoRequest) =>
      unwrapEmpty(api.PUT("/api/v1/environments/{env}/stacks/{name}/git/auto", { ...path, body })),
    onSuccess: refresh,
    onError: (e) => notifications.show({ color: "red", title: "Cannot save", message: e.message }),
  });
  const createHook = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/api/v1/environments/{env}/stacks/{name}/git/webhook", path)),
    onSuccess: ({ path }) => {
      setHookURL(absoluteURL(path));
      refresh();
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot create the webhook", message: e.message }),
  });
  const removeHook = useMutation({
    mutationFn: () =>
      unwrapEmpty(api.DELETE("/api/v1/environments/{env}/stacks/{name}/git/webhook", path)),
    onSuccess: () => {
      setHookURL(null);
      refresh();
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot remove the webhook", message: e.message }),
  });

  const mode = git.autoUpdate;
  const set = (patch: Partial<AutoRequest>) =>
    save.mutate({
      autoUpdate: mode,
      pollInterval: git.pollInterval as AutoRequest["pollInterval"],
      ...patch,
    });
  const checked = git.checkedAt ? formatTime(git.checkedAt, prefs.dateFormat) : null;

  return (
    <Stack gap="xs" mt="sm" component="section" aria-label="Automatic updates">
      <Group gap="xs">
        <Text size="sm" fw={600}>
          Automatic updates
        </Text>
        {git.remoteCommit && (
          <Badge size="sm" color="orange" variant="light" title={git.remoteCommit}>
            new commit {git.remoteCommit.slice(0, 7)}
          </Badge>
        )}
        {checked && (
          <Text size="xs" c="dimmed" title={checked.title}>
            checked {checked.text}
          </Text>
        )}
      </Group>
      <Group grow align="flex-start">
        <Select
          label="On a new commit"
          data={modes}
          value={mode}
          allowDeselect={false}
          disabled={save.isPending}
          onChange={(v) => v && set({ autoUpdate: v as AutoRequest["autoUpdate"] })}
        />
        <Select
          label="Check the repository"
          data={intervals}
          value={String(git.pollInterval)}
          allowDeselect={false}
          disabled={save.isPending || mode === "off"}
          onChange={(v) => v && set({ pollInterval: Number(v) as AutoRequest["pollInterval"] })}
        />
      </Group>
      {mode === "deploy" && (
        <Text size="xs" c="dimmed">
          Redeploys in every namespace where the stack has containers; a stack that is down is only
          pulled.
        </Text>
      )}
      <Group gap="xs">
        <Text size="sm">Webhook:</Text>
        {git.webhook ? (
          <>
            <Badge size="sm" color="teal" variant="light">
              active
            </Badge>
            <Button
              size="xs"
              variant="default"
              loading={createHook.isPending}
              onClick={() => createHook.mutate()}
            >
              New URL
            </Button>
            <Button
              size="xs"
              variant="subtle"
              color="red"
              loading={removeHook.isPending}
              onClick={() => removeHook.mutate()}
            >
              Remove
            </Button>
          </>
        ) : (
          <Button
            size="xs"
            variant="default"
            loading={createHook.isPending}
            onClick={() => createHook.mutate()}
          >
            Create webhook
          </Button>
        )}
      </Group>
      {git.webhook && mode === "off" && !hookURL && (
        <Text size="xs" c="dimmed">
          Calls are refused while automatic updates are off.
        </Text>
      )}
      {hookURL && (
        <Alert
          color="teal"
          title="Webhook created"
          withCloseButton
          onClose={() => setHookURL(null)}
        >
          <Stack gap={6}>
            <Text size="sm">
              Add it as a push webhook (GitHub: Settings → Webhooks, content type application/json;
              GitLab: Settings → Webhooks, push events). It is shown only now.
            </Text>
            <Group gap="xs" wrap="nowrap">
              <Code fz="xs" style={{ flex: 1, wordBreak: "break-all" }} aria-label="Webhook URL">
                {hookURL}
              </Code>
              <CopyButton value={hookURL}>
                {({ copied, copy }) => (
                  <Tooltip label={copied ? "Copied" : "Copy"}>
                    <ActionIcon
                      variant="subtle"
                      color={copied ? "teal" : "gray"}
                      onClick={copy}
                      aria-label="Copy the webhook URL"
                    >
                      {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                    </ActionIcon>
                  </Tooltip>
                )}
              </CopyButton>
            </Group>
          </Stack>
        </Alert>
      )}
    </Stack>
  );
}
