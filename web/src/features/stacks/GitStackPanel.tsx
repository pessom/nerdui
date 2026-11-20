// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Anchor,
  Button,
  Code,
  Group,
  Menu,
  Modal,
  Paper,
  Stack,
  Text,
  TextInput,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconChevronDown, IconGitPullRequest, IconTrash } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, type Stack as StackT } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";
import { formatTime, usePreferences } from "../preferences/context";
import { GitAutoUpdate } from "./GitAutoUpdate";
import { webURL } from "./gitUrl";

type GitInfo = NonNullable<StackT["git"]>;

/**
 * The repository of a Git stack (#37, ADR-0039): its commit, pull (optionally followed by a
 * redeploy) and removal of the clone.
 */
export function GitStackPanel({
  env,
  namespace,
  name,
  git,
  containers,
  readOnly,
}: {
  env: string;
  namespace: string;
  name: string;
  git: GitInfo;
  /** Containers of the stack: a stack with containers cannot be removed. */
  containers: number;
  readOnly: boolean;
}) {
  const queryClient = useQueryClient();
  const jobs = useJobsPanel();
  const { prefs } = usePreferences();
  const committed = formatTime(git.commit.date, prefs.dateFormat);
  const pulled = formatTime(git.pulledAt, prefs.dateFormat);
  const navigate = useNavigate();
  const [removing, setRemoving] = useState(false);
  const [confirm, setConfirm] = useState("");
  const pull = useMutation({
    mutationFn: (body: { redeploy?: boolean; force?: boolean; pull?: boolean }) =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks/{name}/git/pull", {
          params: { path: { env, name }, query: { namespace } },
          body,
        }),
      ),
    onSuccess: ({ job }) => {
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      jobs.open(job.id);
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot pull the stack", message: e.message }),
  });
  const remove = useMutation({
    mutationFn: () =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/stacks/{name}/git", {
          params: { path: { env, name }, query: { namespace, confirm } },
        }),
      ),
    onSuccess: () => {
      notifications.show({ message: `Stack ${name} removed` });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      void navigate({ to: "/env/$env/stacks", params: { env }, search: { ns: namespace } });
    },
  });
  const closeRemove = () => {
    setRemoving(false);
    setConfirm("");
    remove.reset();
  };
  const browse = webURL(git.url);
  const hint = readOnly ? "Read-only mode: host nerdctl is incompatible" : undefined;

  return (
    <Paper withBorder p="sm" component="section" aria-label="Git repository">
      <Group justify="space-between" align="flex-start" wrap="nowrap">
        <Stack gap={4} style={{ minWidth: 0 }}>
          <Text size="sm" style={{ wordBreak: "break-all" }}>
            {browse ? (
              <Anchor href={browse} target="_blank" rel="noreferrer">
                {git.url}
              </Anchor>
            ) : (
              <Code>{git.url}</Code>
            )}{" "}
            <Code>{git.ref || "default branch"}</Code> · <Code>{git.composePath}</Code>
          </Text>
          <Text size="sm">
            <Code title={git.commit.hash}>{git.commit.hash.slice(0, 7)}</Code> {git.commit.message}
            <Text span c="dimmed" size="sm">
              {" "}
              — {git.commit.author}, <span title={committed.title}>{committed.text}</span>
            </Text>
          </Text>
          <Text size="xs" c="dimmed" title={pulled.title}>
            Pulled {pulled.text}
          </Text>
        </Stack>
        <Group gap="xs" wrap="nowrap">
          <Tooltip label={hint} disabled={!readOnly}>
            <Group gap={0} wrap="nowrap">
              <Button
                variant="default"
                leftSection={<IconGitPullRequest size={16} />}
                loading={pull.isPending}
                disabled={readOnly}
                onClick={() => pull.mutate({ redeploy: true })}
                style={{ borderTopRightRadius: 0, borderBottomRightRadius: 0 }}
              >
                Pull and redeploy
              </Button>
              <Menu position="bottom-end">
                <Menu.Target>
                  <Button
                    variant="default"
                    aria-label="Pull options"
                    disabled={readOnly}
                    px={6}
                    style={{ borderTopLeftRadius: 0, borderBottomLeftRadius: 0, borderLeft: 0 }}
                  >
                    <IconChevronDown size={14} />
                  </Button>
                </Menu.Target>
                <Menu.Dropdown>
                  <Menu.Item onClick={() => pull.mutate({})}>Pull only</Menu.Item>
                  <Menu.Item onClick={() => pull.mutate({ redeploy: true, force: true })}>
                    Pull and redeploy even if unchanged
                  </Menu.Item>
                  <Menu.Item
                    onClick={() => pull.mutate({ redeploy: true, force: true, pull: true })}
                  >
                    Pull, pull images and redeploy
                  </Menu.Item>
                </Menu.Dropdown>
              </Menu>
            </Group>
          </Tooltip>
          <Tooltip
            label={containers > 0 ? "Bring the stack down first" : hint}
            disabled={!readOnly && containers === 0}
          >
            <Button
              variant="default"
              color="red"
              leftSection={<IconTrash size={16} />}
              disabled={readOnly || containers > 0}
              onClick={() => setRemoving(true)}
            >
              Remove
            </Button>
          </Tooltip>
        </Group>
      </Group>
      {git.lastError && (
        <Alert color="red" title="The last pull failed" mt="sm">
          <Text size="sm" style={{ whiteSpace: "pre-wrap" }}>
            {git.lastError}
          </Text>
        </Alert>
      )}
      <GitAutoUpdate env={env} namespace={namespace} name={name} git={git} />
      <Modal opened={removing} onClose={closeRemove} title={`Remove stack ${name}`}>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            remove.mutate();
          }}
        >
          <Stack gap="sm">
            <Text size="sm">
              The clone and its local <Code>.env</Code> are deleted. The repository is not touched.
            </Text>
            <TextInput
              label={`Type ${name} to confirm`}
              data-autofocus
              value={confirm}
              onChange={(e) => setConfirm(e.currentTarget.value)}
            />
            {remove.isError && (
              <Alert color="red" title="Cannot remove the stack">
                {remove.error.message}
              </Alert>
            )}
            <Group justify="flex-end">
              <Button variant="default" onClick={closeRemove}>
                Cancel
              </Button>
              <Button
                type="submit"
                color="red"
                loading={remove.isPending}
                disabled={confirm !== name}
              >
                Remove
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>
    </Paper>
  );
}
