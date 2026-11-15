// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  Alert,
  Button,
  Code,
  Group,
  List,
  Modal,
  SegmentedControl,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  TextInput,
  Textarea,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import { keys, type Network, type NetworkSpec } from "../../api/queries";
import { parseLabels } from "../../lib/labels";

type Driver = NonNullable<NetworkSpec["driver"]>;

const empty = { name: "", parent: "", subnet: "", gateway: "", ipRange: "", labels: "" };

export function CreateNetworkDialog({
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
  const [form, setForm] = useState(empty);
  const [driver, setDriver] = useState<Driver>("bridge");
  const [internal, setInternal] = useState(false);
  const parsed = parseLabels(form.labels);
  // Read the value now: currentTarget is gone when the state updater runs.
  const set = (k: keyof typeof empty) => (e: { currentTarget: { value: string } }) => {
    const value = e.currentTarget.value;
    setForm((f) => ({ ...f, [k]: value }));
  };
  const close = () => {
    setForm(empty);
    setDriver("bridge");
    setInternal(false);
    create.reset();
    onClose();
  };
  const spec = (): NetworkSpec => ({
    name: form.name.trim(),
    driver,
    subnet: form.subnet.trim() || undefined,
    gateway: form.gateway.trim() || undefined,
    ipRange: form.ipRange.trim() || undefined,
    parent: driver === "bridge" ? undefined : form.parent.trim(),
    internal: driver === "bridge" ? internal : undefined,
    labels: parsed.labels,
  });
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/environments/{env}/networks", {
          params: { path: { env }, query: { namespace } },
          body: spec(),
        }),
      ),
    onSuccess: (n) => {
      notifications.show({ color: "teal", message: `Network ${n.name} created` });
      void queryClient.invalidateQueries({ queryKey: keys.networks(env, namespace) });
      close();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!parsed.error) create.mutate();
  };
  const needsParent = driver !== "bridge";
  return (
    <Modal opened={opened} onClose={close} title="Create network" size="lg">
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Name"
            required
            data-autofocus
            value={form.name}
            onChange={set("name")}
          />
          <div>
            <Text size="sm" fw={500} mb={4}>
              Driver
            </Text>
            <SegmentedControl
              aria-label="Driver"
              value={driver}
              onChange={(v) => setDriver(v as Driver)}
              data={[
                { value: "bridge", label: "bridge" },
                { value: "macvlan", label: "macvlan" },
                { value: "ipvlan", label: "ipvlan" },
              ]}
            />
            <Text size="xs" c="dimmed" mt={4}>
              {driver === "bridge"
                ? "A private network on this host; published ports reach containers through it."
                : "Containers get addresses in the network of a host interface, like separate machines."}
            </Text>
          </div>
          {needsParent && (
            <TextInput
              label="Parent interface"
              description="Host interface, e.g. eth0"
              required
              value={form.parent}
              onChange={set("parent")}
            />
          )}
          <SimpleGrid cols={{ base: 1, sm: 3 }}>
            <TextInput
              label="Subnet"
              placeholder={needsParent ? "192.168.1.0/24" : "automatic"}
              required={needsParent}
              value={form.subnet}
              onChange={set("subnet")}
            />
            <TextInput
              label="Gateway"
              placeholder="optional"
              value={form.gateway}
              onChange={set("gateway")}
            />
            <TextInput
              label="IP range"
              placeholder="optional, CIDR"
              value={form.ipRange}
              onChange={set("ipRange")}
            />
          </SimpleGrid>
          {driver === "bridge" && (
            <Switch
              label="Internal (no access to the outside)"
              checked={internal}
              onChange={(e) => setInternal(e.currentTarget.checked)}
            />
          )}
          <Textarea
            label="Labels"
            description="One key=value per line"
            rows={2}
            value={form.labels}
            onChange={set("labels")}
            error={parsed.error}
          />
          {create.isError && (
            <Alert color="red" title="Cannot create network">
              {create.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" loading={create.isPending} disabled={!form.name.trim()}>
              Create
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}

export function RemoveNetworkDialog({
  env,
  namespace,
  network,
  onClose,
}: {
  env: string;
  namespace: string;
  network?: Network;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation({
    mutationFn: (name: string) =>
      unwrapEmpty(
        api.DELETE("/api/v1/environments/{env}/networks/{name}", {
          params: { path: { env, name }, query: { namespace } },
        }),
      ),
    onSuccess: (_, name) => {
      notifications.show({ message: `Network ${name} removed` });
      void queryClient.invalidateQueries({ queryKey: keys.networks(env, namespace) });
      close();
    },
  });
  const close = () => {
    remove.reset();
    onClose();
  };
  const stopped = network?.containers.filter((c) => !c.running) ?? [];
  return (
    <Modal opened={!!network} onClose={close} title="Remove network">
      {network && (
        <Stack gap="sm">
          <Text size="sm">
            Remove the network <Code>{network.name}</Code>?
          </Text>
          {stopped.length > 0 && (
            <Alert color="yellow" title="Stopped containers use this network">
              They will not start until they are recreated with another network:
              <List size="sm" mt={4}>
                {stopped.map((c) => (
                  <List.Item key={c.id}>{c.name || c.id.slice(0, 12)}</List.Item>
                ))}
              </List>
            </Alert>
          )}
          {remove.isError && (
            <Alert color="red" title="Cannot remove network">
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
              onClick={() => remove.mutate(network.name)}
            >
              Remove network
            </Button>
          </Group>
        </Stack>
      )}
    </Modal>
  );
}
