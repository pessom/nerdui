// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Modal, Stack, Text } from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import type { UserDetail } from "../../api/queries";
import { APIKeyTable, KeysLoading, RevokeKeyModal, type APIKey } from "./APIKeys";

/** An administrator's view of a user's API keys, with revoke (#87). */
export function UserKeysModal({ user, onClose }: { user?: UserDetail; onClose: () => void }) {
  const queryClient = useQueryClient();
  const id = user?.id ?? 0;
  const keys = useQuery({
    queryKey: ["users", id, "api-keys"],
    queryFn: async () =>
      (await unwrap(api.GET("/api/v1/users/{id}/api-keys", { params: { path: { id } } }))).keys,
    enabled: !!user,
  });
  const [revoking, setRevoking] = useState<APIKey>();
  const revoke = useMutation({
    mutationFn: (k: APIKey) =>
      unwrapEmpty(
        api.DELETE("/api/v1/users/{id}/api-keys/{key}", { params: { path: { id, key: k.id } } }),
      ),
    onSuccess: (_, k) => {
      notifications.show({ message: `API key ${k.name} of ${user?.username} revoked` });
      setRevoking(undefined);
      void queryClient.invalidateQueries({ queryKey: ["users", id, "api-keys"] });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot revoke the key", message: e.message }),
  });
  return (
    <Modal
      opened={!!user}
      onClose={onClose}
      title={`API keys of ${user?.username ?? ""}`}
      size="xl"
    >
      <Stack gap="sm">
        <Text size="sm" c="dimmed">
          Users create their keys from their user menu. Disabling the user stops their keys.
        </Text>
        <KeysLoading pending={keys.isPending && !!user} error={keys.error} />
        {keys.data && <APIKeyTable keys={keys.data} onRevoke={setRevoking} />}
      </Stack>
      <RevokeKeyModal
        apiKey={revoking}
        loading={revoke.isPending}
        onClose={() => setRevoking(undefined)}
        onConfirm={(k) => revoke.mutate(k)}
      />
    </Modal>
  );
}
