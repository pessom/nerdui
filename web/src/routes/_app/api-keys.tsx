// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Button, Code, Group, Stack, Text, Title } from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import { notifications } from "@mantine/notifications";
import { IconPlus } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import {
  APIKeyTable,
  CreateAPIKeyModal,
  KeysLoading,
  RevokeKeyModal,
  type APIKey,
} from "../../features/apikeys/APIKeys";

/** The signed-in user's API keys (#87, ADR-0028). */
export const Route = createFileRoute("/_app/api-keys")({
  component: APIKeysPage,
});

function APIKeysPage() {
  const queryClient = useQueryClient();
  const keys = useQuery({
    queryKey: ["api-keys"],
    queryFn: async () => (await unwrap(api.GET("/api/v1/auth/api-keys"))).keys,
  });
  const [creating, { open, close }] = useDisclosure();
  const [revoking, setRevoking] = useState<APIKey>();
  const revoke = useMutation({
    mutationFn: (k: APIKey) =>
      unwrapEmpty(api.DELETE("/api/v1/auth/api-keys/{key}", { params: { path: { key: k.id } } })),
    onSuccess: (_, k) => {
      notifications.show({ message: `API key ${k.name} revoked` });
      setRevoking(undefined);
      void queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot revoke the key", message: e.message }),
  });

  return (
    <Stack>
      <Group justify="space-between">
        <Title order={2}>API keys</Title>
        <Button leftSection={<IconPlus size={16} />} onClick={open}>
          Create API key
        </Button>
      </Group>
      <Text size="sm" c="dimmed">
        Keys let scripts and integrations use the API as you: send{" "}
        <Code>Authorization: Bearer &lt;key&gt;</Code>. A read-only key can only read. Keys cannot
        change your password or manage keys. See the API documentation for the endpoints.
      </Text>
      <KeysLoading pending={keys.isPending} error={keys.error} />
      {keys.data && <APIKeyTable keys={keys.data} onRevoke={setRevoking} />}
      <CreateAPIKeyModal opened={creating} onClose={close} />
      <RevokeKeyModal
        apiKey={revoking}
        loading={revoke.isPending}
        onClose={() => setRevoking(undefined)}
        onConfirm={(k) => revoke.mutate(k)}
      />
    </Stack>
  );
}
