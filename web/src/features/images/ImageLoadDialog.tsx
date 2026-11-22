// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, FileInput, Group, List, Modal, Progress, Stack, Text } from "@mantine/core";
import { IconFileZip } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { keys } from "../../api/queries";
import { formatBytes } from "../system/info";
import { uploadImages } from "./archive";

/** Loads images from a tar archive (#46), for hosts without registry access. */
export function ImageLoadDialog({
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
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState(0);
  const abort = useRef<AbortController>(undefined);
  const load = useMutation({
    mutationFn: (f: File) => {
      abort.current = new AbortController();
      return uploadImages(env, namespace, f, setProgress, abort.current.signal);
    },
    onSettled: () => void queryClient.invalidateQueries({ queryKey: keys.images(env, namespace) }),
  });
  const close = () => {
    abort.current?.abort();
    setFile(null);
    setProgress(0);
    load.reset();
    onClose();
  };
  const uploaded = progress >= 1;
  return (
    <Modal opened={opened} onClose={close} title="Load images">
      <Stack>
        <Text size="sm">
          A tar archive from <code>nerdctl save</code>, <code>docker save</code> or Save as tar,
          optionally gzip-compressed. Images are loaded for the host&apos;s platform and unpacked.
        </Text>
        <FileInput
          label="Archive"
          placeholder="Choose a .tar file"
          accept=".tar,.tar.gz,.tgz,application/x-tar,application/gzip"
          leftSection={<IconFileZip size={16} />}
          value={file}
          onChange={(f) => {
            setFile(f);
            load.reset();
          }}
          disabled={load.isPending}
          description={file ? formatBytes(file.size) : undefined}
          clearable
        />
        {load.isPending && (
          <Stack gap={4}>
            <Progress value={progress * 100} animated={uploaded} aria-label="Upload progress" />
            <Text size="xs" c="dimmed">
              {uploaded ? "Importing and unpacking…" : `Uploading ${Math.round(progress * 100)}%`}
            </Text>
          </Stack>
        )}
        {load.error && load.error.name !== "AbortError" && (
          <Alert color="red" title="Cannot load the archive">
            {load.error.message}
          </Alert>
        )}
        {load.data && (
          <Alert
            color="teal"
            title={`Loaded ${load.data.length} image${load.data.length === 1 ? "" : "s"}`}
          >
            <List size="sm">
              {load.data.map((n) => (
                <List.Item key={n} ff="monospace">
                  {n}
                </List.Item>
              ))}
            </List>
          </Alert>
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={close}>
            {load.data ? "Close" : "Cancel"}
          </Button>
          {!load.data && (
            <Button
              loading={load.isPending}
              disabled={!file}
              onClick={() => file && load.mutate(file)}
            >
              Load
            </Button>
          )}
        </Group>
      </Stack>
    </Modal>
  );
}
