// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Checkbox,
  Group,
  Modal,
  Select,
  Stack,
  Text,
  TextInput,
} from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { gitCredentialsQuery, keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";
import { isSSH, repoName } from "./gitUrl";

/**
 * Creates a stack from a Git repository (#37, ADR-0039): the clone runs as a job in the Jobs
 * panel, optionally followed by a deploy.
 */
export function GitStackDialog({
  env,
  namespace,
  opened,
  onClose,
}: {
  env: string;
  namespace: string;
  opened: boolean;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const jobs = useJobsPanel();
  const credentials = useQuery({ ...gitCredentialsQuery, enabled: opened });
  const [url, setUrl] = useState("");
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [ref, setRef] = useState("");
  const [composePath, setComposePath] = useState("");
  const [credential, setCredential] = useState<string | null>(null);
  const [deploy, setDeploy] = useState(true);
  const shownName = nameTouched ? name : repoName(url);

  const close = () => {
    setUrl("");
    setName("");
    setNameTouched(false);
    setRef("");
    setComposePath("");
    setCredential(null);
    setDeploy(true);
    create.reset();
    onClose();
  };
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/stacks/git", {
          params: { path: { env }, query: { namespace } },
          body: {
            name: shownName.trim(),
            url: url.trim(),
            ref: ref.trim(),
            composePath: composePath.trim(),
            credentialId: credential ? Number(credential) : undefined,
            deploy,
          },
        }),
      ),
    onSuccess: ({ job }) => {
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
      void queryClient.invalidateQueries({ queryKey: keys.stacks(env, namespace) });
      jobs.open(job.id);
      close();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };
  const ssh = isSSH(url.trim());
  const options = (credentials.data?.credentials ?? [])
    .filter((c) => (ssh ? c.kind === "ssh" : c.kind === "token"))
    .map((c) => ({ value: String(c.id), label: c.name }));

  return (
    <Modal opened={opened} onClose={close} title="New stack from Git" size="lg">
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Repository URL"
            placeholder="https://github.com/owner/repo.git or git@github.com:owner/repo.git"
            required
            data-autofocus
            value={url}
            onChange={(e) => {
              setUrl(e.currentTarget.value);
              setCredential(null);
            }}
          />
          <Group grow align="flex-start">
            <TextInput
              label="Name"
              description="The compose project name"
              required
              value={shownName}
              onChange={(e) => {
                setName(e.currentTarget.value);
                setNameTouched(true);
              }}
            />
            <TextInput
              label="Branch or tag"
              description="Empty: the default branch"
              value={ref}
              onChange={(e) => setRef(e.currentTarget.value)}
            />
          </Group>
          <TextInput
            label="Compose file"
            description="Path of the compose file or its directory in the repository; empty: the root"
            placeholder="deploy/compose.yaml"
            value={composePath}
            onChange={(e) => setComposePath(e.currentTarget.value)}
          />
          <Select
            label="Credential"
            description={
              ssh
                ? "SSH URLs need an SSH key credential (Settings → Git credentials)"
                : "Needed for private repositories (Settings → Git credentials)"
            }
            placeholder={options.length ? "None (public repository)" : "No matching credentials"}
            data={options}
            value={credential}
            onChange={setCredential}
            clearable
          />
          <Checkbox
            label="Deploy after cloning"
            checked={deploy}
            onChange={(e) => setDeploy(e.currentTarget.checked)}
          />
          <Text size="sm" c="dimmed">
            The repository is cloned into nerdui&apos;s data directory. Its compose file is
            read-only here; .env stays local and survives pulls.
          </Text>
          {create.isError && (
            <Alert color="red" title="Cannot create the stack">
              {create.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button
              type="submit"
              loading={create.isPending}
              disabled={!url.trim() || !shownName.trim()}
            >
              {deploy ? "Clone and deploy" : "Clone"}
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
