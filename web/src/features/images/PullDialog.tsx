// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Modal, Select, Stack, Text, TextInput } from "@mantine/core";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { useJobsPanel } from "../jobs/JobsContext";

const ALL = "all";
const platformOptions = [
  { value: "", label: "Host platform" },
  { value: "linux/amd64", label: "linux/amd64" },
  { value: "linux/arm64", label: "linux/arm64" },
  { value: "linux/arm/v7", label: "linux/arm/v7" },
  { value: ALL, label: "All platforms (no unpack)" },
];

/** Starts a pull job (#43) and opens it in the jobs panel. */
export function PullDialog({
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
  const panel = useJobsPanel();
  const [reference, setReference] = useState("");
  const [platform, setPlatform] = useState("");
  const pull = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/images/pull", {
          params: { path: { env }, query: { namespace } },
          body: {
            reference: reference.trim(),
            ...(platform === ALL ? { allPlatforms: true } : platform ? { platform } : {}),
          },
        }),
      ),
    onSuccess: async ({ job }) => {
      await queryClient.invalidateQueries({ queryKey: keys.jobs });
      close();
      panel.open(job.id);
    },
  });
  const close = () => {
    setReference("");
    setPlatform("");
    pull.reset();
    onClose();
  };
  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (reference.trim()) pull.mutate();
  };
  return (
    <Modal opened={opened} onClose={close} title="Pull image">
      <form onSubmit={onSubmit}>
        <Stack>
          {pull.error && <Alert color="red">{pull.error.message}</Alert>}
          <TextInput
            label="Image"
            description="e.g. nginx:1.27, ghcr.io/owner/app:tag"
            required
            autoFocus
            autoComplete="off"
            value={reference}
            onChange={(e) => setReference(e.currentTarget.value)}
          />
          <Select
            label="Platform"
            data={platformOptions}
            value={platform}
            onChange={(v) => setPlatform(v ?? "")}
            allowDeselect={false}
          />
          <Text size="xs" c="dimmed">
            Pulled into namespace {namespace} with the host&apos;s nerdctl registry settings. The
            pull continues in the background; follow it in Jobs.
          </Text>
          <Button type="submit" loading={pull.isPending} disabled={!reference.trim()}>
            Pull
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}
