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
  { value: ALL, label: "All platforms" },
];

export type ImageJobKind = "pull" | "push";

const texts = {
  pull: {
    title: "Pull image",
    button: "Pull",
    hint: "Pulled into namespace {ns} with the host's nerdctl registry settings.",
  },
  push: {
    title: "Push image",
    button: "Push",
    hint: "Pushed from namespace {ns} with the host's registry settings and credentials (sudo nerdctl login).",
  },
};

/**
 * Starts a pull (#43) or push (#44) job and opens it in the jobs panel. initialReference is
 * read on mount: give the dialog a key that changes with it.
 */
export function ImageJobDialog({
  kind,
  env,
  namespace,
  opened,
  initialReference = "",
  onClose,
}: {
  kind: ImageJobKind;
  env: string;
  namespace: string;
  opened: boolean;
  initialReference?: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const panel = useJobsPanel();
  const [reference, setReference] = useState(initialReference);
  const [platform, setPlatform] = useState("");
  const t = texts[kind];
  const pull = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST(`/api/v1/environments/{env}/images/${kind}`, {
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
    <Modal opened={opened} onClose={close} title={t.title}>
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
            {t.hint.replace("{ns}", namespace)} The job continues in the background; follow it in
            Jobs.
          </Text>
          <Button type="submit" loading={pull.isPending} disabled={!reference.trim()}>
            {t.button}
          </Button>
        </Stack>
      </form>
    </Modal>
  );
}
