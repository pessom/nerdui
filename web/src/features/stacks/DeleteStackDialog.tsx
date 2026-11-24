// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Checkbox, Code, Group, Modal, Stack, Text, TextInput } from "@mantine/core";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";

/**
 * Deletes a stack created in nerdui (#248): a job takes it down, optionally with its volumes,
 * then removes its directory and its variables. The stack name confirms.
 */
export function DeleteStackDialog({
  env,
  namespace,
  name,
  ns,
  opened,
  onClose,
}: {
  env: string;
  namespace: string;
  name: string;
  /** The namespace in the URL, kept when going back to the list. */
  ns?: string;
  opened: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const jobs = useJobsPanel();
  const [volumes, setVolumes] = useState(false);
  const [typed, setTyped] = useState("");
  const remove = useMutation({
    mutationFn: () =>
      unwrap(
        api.DELETE("/api/v1/environments/{env}/stacks/{name}", {
          params: {
            path: { env, name },
            query: { namespace, confirm: typed, volumes: volumes || undefined },
          },
        }),
      ),
    onSuccess: ({ job }) => {
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      jobs.open(job.id);
      void navigate({ to: "/env/$env/stacks", params: { env }, search: { ns } });
    },
  });
  const close = () => {
    setVolumes(false);
    setTyped("");
    remove.reset();
    onClose();
  };
  return (
    <Modal opened={opened} onClose={close} title={`Delete stack ${name}`}>
      <Stack gap="sm">
        <Text size="sm">
          Takes <Code>{name}</Code> down, then deletes its directory with the compose file and{" "}
          <Code>.env</Code>, and its variables and secrets. This cannot be undone.
        </Text>
        <Checkbox
          color="red"
          label="Also remove the stack's volumes and their data"
          checked={volumes}
          onChange={(e) => setVolumes(e.currentTarget.checked)}
        />
        <TextInput
          label={`Type ${name} to confirm`}
          value={typed}
          onChange={(e) => setTyped(e.currentTarget.value)}
        />
        {remove.isError && (
          <Alert color="red" title="Cannot delete the stack">
            {remove.error.message}
          </Alert>
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>
            Cancel
          </Button>
          <Button
            color="red"
            loading={remove.isPending}
            disabled={typed !== name}
            onClick={() => remove.mutate()}
          >
            {volumes ? "Delete stack and volumes" : "Delete stack"}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
