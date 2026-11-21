// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Badge, Loader, Modal, Table, Text } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "../../api/client";
import type { Image } from "../../api/queries";
import { formatBytes } from "../system/info";
import { displayName } from "./format";

/** Build steps and layer sizes of an image (#47), newest first. */
export function ImageHistoryDialog({
  env,
  namespace,
  image,
  onClose,
}: {
  env: string;
  namespace: string;
  image: Image | undefined;
  onClose: () => void;
}) {
  const history = useQuery({
    queryKey: ["env", env, "ns", namespace, "image-history", image?.name ?? ""],
    queryFn: () =>
      unwrap(
        api.GET("/api/v1/environments/{env}/images/history", {
          params: { path: { env }, query: { namespace, ref: image?.name ?? "" } },
        }),
      ),
    enabled: !!image,
    staleTime: Infinity, // an image's history never changes
  });
  const compressed = history.data?.history.some((e) => e.compressed);

  return (
    <Modal
      opened={!!image}
      onClose={onClose}
      title={image && `History of ${displayName(image)}`}
      size="xl"
    >
      {history.isPending && <Loader aria-label="Loading history" />}
      {history.isError && (
        <Alert color="red" title="Cannot read the history">
          {history.error.message}
        </Alert>
      )}
      {history.data && (
        <>
          {compressed && (
            <Text size="xs" c="dimmed" mb="xs">
              The image is not unpacked: sizes are compressed sizes.
            </Text>
          )}
          <Table.ScrollContainer minWidth={560}>
            <Table verticalSpacing={4} fz="sm" aria-label="Image history">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Created</Table.Th>
                  <Table.Th>Step</Table.Th>
                  <Table.Th ta="right">Size</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {history.data.history.map((e, i) => (
                  <Table.Tr key={i}>
                    <Table.Td style={{ whiteSpace: "nowrap" }}>
                      {e.created ? new Date(e.created).toLocaleString() : "—"}
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs" ff="monospace" style={{ wordBreak: "break-all" }}>
                        {e.createdBy || "—"}
                      </Text>
                      {e.comment && (
                        <Text size="xs" c="dimmed">
                          {e.comment}
                        </Text>
                      )}
                    </Table.Td>
                    <Table.Td ta="right" style={{ whiteSpace: "nowrap" }}>
                      {e.emptyLayer ? (
                        <Badge size="xs" variant="light" color="gray">
                          no layer
                        </Badge>
                      ) : (
                        formatBytes(e.size)
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </>
      )}
    </Modal>
  );
}
