// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Anchor,
  Breadcrumbs,
  Button,
  Code,
  Group,
  Loader,
  Modal,
  ScrollArea,
  Stack,
  Table,
  Text,
  Tooltip,
} from "@mantine/core";
import {
  IconArrowLeft,
  IconDownload,
  IconFile,
  IconFolder,
  IconLink,
  IconQuestionMark,
} from "@tabler/icons-react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, type components } from "../../api/client";
import { Time } from "../../components/Time";
import { formatBytes } from "../system/info";
import { volumeDownloadUrl } from "./files";

type Entry = components["schemas"]["VolumeEntry"];

function join(dir: string, name: string) {
  return dir === "/" ? `/${name}` : `${dir}/${name}`;
}

const icons = {
  dir: IconFolder,
  file: IconFile,
  symlink: IconLink,
  other: IconQuestionMark,
} as const;

/** Read-only browser of a volume's files with previews and downloads (#58, ADR-0045). */
export function VolumeBrowser({
  env,
  namespace,
  volume,
  onClose,
}: {
  env: string;
  namespace: string;
  volume?: string;
  onClose: () => void;
}) {
  const [dir, setDir] = useState("/");
  const [file, setFile] = useState<string>();
  const close = () => {
    setDir("/");
    setFile(undefined);
    onClose();
  };
  const listing = useQuery({
    queryKey: ["env", env, "volumes", namespace, volume, "files", dir],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/volumes/{name}/files", {
          params: { path: { env, name: volume ?? "" }, query: { namespace, path: dir } },
        }),
      ),
    enabled: !!volume,
  });
  const preview = useQuery({
    queryKey: ["env", env, "volumes", namespace, volume, "preview", file],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/volumes/{name}/files/preview", {
          params: { path: { env, name: volume ?? "" }, query: { namespace, path: file ?? "" } },
        }),
      ),
    enabled: !!volume && !!file,
  });
  const parts = dir.split("/").filter(Boolean);
  const download = (path: string) => volumeDownloadUrl(env, namespace, volume ?? "", path);

  const open = (e: Entry) => {
    const p = join(dir, e.name);
    // The server follows symlinks that stay inside the volume.
    const type = e.type === "symlink" ? e.targetType : e.type;
    if (type === "dir") setDir(p);
    else if (type === "file") setFile(p);
  };

  return (
    <Modal opened={!!volume} onClose={close} size="xl" title={volume && `Files of ${volume}`}>
      {file ? (
        <Stack>
          <Group justify="space-between">
            <Button
              variant="subtle"
              leftSection={<IconArrowLeft size={16} />}
              onClick={() => setFile(undefined)}
            >
              Back to {dir}
            </Button>
            <Button
              component="a"
              href={download(file)}
              download
              variant="default"
              leftSection={<IconDownload size={16} />}
            >
              Download
            </Button>
          </Group>
          <Text ff="monospace" size="sm" style={{ wordBreak: "break-all" }}>
            {file}
          </Text>
          {preview.isPending && <Loader size="sm" aria-label="Loading" />}
          {preview.isError && <Alert color="red">{preview.error.message}</Alert>}
          {preview.data &&
            (preview.data.binary ? (
              <Text size="sm" c="dimmed">
                Binary file, {formatBytes(preview.data.size)}. Download it to open it.
              </Text>
            ) : (
              <>
                <ScrollArea.Autosize mah="60vh">
                  <Code block aria-label="File content">
                    {preview.data.content}
                  </Code>
                </ScrollArea.Autosize>
                {preview.data.truncated && (
                  <Text size="xs" c="dimmed">
                    Showing the first 256 KiB of {formatBytes(preview.data.size)}.
                  </Text>
                )}
              </>
            ))}
        </Stack>
      ) : (
        <Stack>
          <Group justify="space-between" wrap="nowrap">
            <Breadcrumbs separator="/" style={{ flexWrap: "wrap" }}>
              <Anchor component="button" size="sm" onClick={() => setDir("/")}>
                {volume}
              </Anchor>
              {parts.map((p, i) => (
                <Anchor
                  key={i}
                  component="button"
                  size="sm"
                  onClick={() => setDir("/" + parts.slice(0, i + 1).join("/"))}
                >
                  {p}
                </Anchor>
              ))}
            </Breadcrumbs>
            <Button
              component="a"
              href={download(dir)}
              download
              size="xs"
              variant="default"
              leftSection={<IconDownload size={14} />}
            >
              {dir === "/" ? "Download volume" : "Download folder"}
            </Button>
          </Group>
          <Text size="xs" c="dimmed">
            Read-only. Files are read while containers may write them: stop a database&apos;s
            container for a consistent copy.
          </Text>
          {listing.isPending && <Loader size="sm" aria-label="Loading" />}
          {listing.isError && <Alert color="red">{listing.error.message}</Alert>}
          {listing.data && (
            <ScrollArea.Autosize mah="60vh">
              <Table striped highlightOnHover verticalSpacing={4} aria-label="Volume files">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Name</Table.Th>
                    <Table.Th>Size</Table.Th>
                    <Table.Th>Mode</Table.Th>
                    <Table.Th>Modified</Table.Th>
                    <Table.Th w={40} />
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {listing.data.entries.length === 0 && (
                    <Table.Tr>
                      <Table.Td colSpan={5}>
                        <Text size="sm" c="dimmed" ta="center">
                          Empty directory.
                        </Text>
                      </Table.Td>
                    </Table.Tr>
                  )}
                  {listing.data.entries.map((e) => {
                    const Icon = icons[e.type];
                    const p = join(dir, e.name);
                    return (
                      <Table.Tr key={e.name}>
                        <Table.Td>
                          <Group gap={6} wrap="nowrap">
                            <Icon size={16} aria-hidden />
                            {e.type === "other" || (e.type === "symlink" && !e.targetType) ? (
                              <Text size="sm">{e.name}</Text>
                            ) : (
                              <Anchor
                                component="button"
                                size="sm"
                                onClick={() => open(e)}
                                style={{ wordBreak: "break-all", textAlign: "left" }}
                              >
                                {e.name}
                              </Anchor>
                            )}
                            {e.target && (
                              <Text size="xs" c="dimmed" ff="monospace">
                                → {e.target}
                              </Text>
                            )}
                          </Group>
                        </Table.Td>
                        <Table.Td>
                          <Text size="sm">{e.type === "file" ? formatBytes(e.size) : ""}</Text>
                        </Table.Td>
                        <Table.Td>
                          <Text size="xs" ff="monospace">
                            {e.mode}
                          </Text>
                        </Table.Td>
                        <Table.Td>
                          <Time value={e.modTime} />
                        </Table.Td>
                        <Table.Td>
                          {(e.type === "file" || e.type === "dir") && (
                            <Tooltip label={e.type === "dir" ? "Download as .tar.gz" : "Download"}>
                              <ActionIcon
                                component="a"
                                href={download(p)}
                                download
                                variant="subtle"
                                color="gray"
                                aria-label={`Download ${e.name}`}
                              >
                                <IconDownload size={16} />
                              </ActionIcon>
                            </Tooltip>
                          )}
                        </Table.Td>
                      </Table.Tr>
                    );
                  })}
                </Table.Tbody>
              </Table>
              {listing.data.truncated && (
                <Text size="xs" c="dimmed" mt="xs">
                  Only the first 5000 entries are shown.
                </Text>
              )}
            </ScrollArea.Autosize>
          )}
        </Stack>
      )}
    </Modal>
  );
}
