// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Code,
  CopyButton,
  Group,
  Loader,
  Modal,
  Paper,
  PasswordInput,
  SegmentedControl,
  Stack,
  Table,
  Text,
  Textarea,
  TextInput,
  Title,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconCheck, IconCopy, IconPencil, IconTrash } from "@tabler/icons-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, unwrap, unwrapEmpty } from "../../api/client";
import {
  gitCredentialsQuery,
  gitKnownHostsQuery,
  type GitCredential,
  type GitKnownHost,
} from "../../api/queries";
import type { components } from "../../api/schema";
import { formatTime, usePreferences } from "../preferences/context";

type Kind = GitCredential["kind"];
type Input = components["schemas"]["GitCredentialInput"];

/** Saved Git credentials (#93) and trusted SSH host keys of Git stacks (#37, ADR-0039). */
export function GitCredentialsSettingsCard() {
  const queryClient = useQueryClient();
  const { prefs } = usePreferences();
  const credentials = useQuery(gitCredentialsQuery);
  const hosts = useQuery(gitKnownHostsQuery);
  const [editing, setEditing] = useState<GitCredential | "new" | null>(null);
  const [removing, setRemoving] = useState<GitCredential | null>(null);
  const [forgetting, setForgetting] = useState<GitKnownHost | null>(null);
  const remove = useMutation({
    mutationFn: (id: number) =>
      unwrapEmpty(api.DELETE("/api/v1/git/credentials/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setRemoving(null);
      void queryClient.invalidateQueries({ queryKey: gitCredentialsQuery.queryKey });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot remove", message: e.message }),
  });
  const forget = useMutation({
    mutationFn: (host: string) =>
      unwrapEmpty(api.DELETE("/api/v1/git/known-hosts", { params: { query: { host } } })),
    onSuccess: () => {
      setForgetting(null);
      void queryClient.invalidateQueries({ queryKey: gitKnownHostsQuery.queryKey });
    },
    onError: (e) =>
      notifications.show({ color: "red", title: "Cannot forget the host", message: e.message }),
  });

  return (
    <Paper withBorder p="md" component="section" aria-label="Git credentials">
      <Stack gap="sm">
        <Group justify="space-between" align="flex-start">
          <div>
            <Title order={3}>Git credentials</Title>
            <Text size="sm" c="dimmed">
              Tokens and SSH keys for private repositories of Git stacks. Secrets are stored in
              nerdui&apos;s database and are never shown again.
            </Text>
          </div>
          <Button onClick={() => setEditing("new")}>Add credential</Button>
        </Group>
        {credentials.isPending && <Loader aria-label="Loading credentials" />}
        {credentials.isError && (
          <Alert color="red" title="Cannot read credentials">
            {credentials.error.message}
          </Alert>
        )}
        {credentials.data && credentials.data.credentials.length === 0 && (
          <Text size="sm" c="dimmed">
            No credentials yet. Public repositories need none.
          </Text>
        )}
        {credentials.data && credentials.data.credentials.length > 0 && (
          <Table.ScrollContainer minWidth={560}>
            <Table verticalSpacing={4} fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Kind</Table.Th>
                  <Table.Th>Stacks</Table.Th>
                  <Table.Th>Updated</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {credentials.data.credentials.map((c) => (
                  <Table.Tr key={c.id}>
                    <Table.Td>{c.name}</Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant="light" color={c.kind === "ssh" ? "grape" : "blue"}>
                        {c.kind === "ssh" ? "SSH key" : "Token"}
                      </Badge>
                    </Table.Td>
                    <Table.Td>{c.stacks}</Table.Td>
                    <Table.Td>{formatTime(c.updatedAt, prefs.dateFormat).text}</Table.Td>
                    <Table.Td>
                      <Group gap={4} justify="flex-end" wrap="nowrap">
                        {c.publicKey && <CopyPublicKey value={c.publicKey} name={c.name} />}
                        <Tooltip label="Edit">
                          <ActionIcon
                            variant="subtle"
                            aria-label={`Edit ${c.name}`}
                            onClick={() => setEditing(c)}
                          >
                            <IconPencil size={16} />
                          </ActionIcon>
                        </Tooltip>
                        <Tooltip label={c.stacks > 0 ? "Used by Git stacks" : "Remove"}>
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label={`Remove ${c.name}`}
                            disabled={c.stacks > 0}
                            onClick={() => setRemoving(c)}
                          >
                            <IconTrash size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}

        <Title order={4} mt="sm">
          Trusted SSH hosts
        </Title>
        <Text size="sm" c="dimmed">
          The first key a Git server presents is trusted; a different key later stops the pull.
          Forget a host only when its key change is expected.
        </Text>
        {hosts.isError && (
          <Alert color="red" title="Cannot read hosts">
            {hosts.error.message}
          </Alert>
        )}
        {hosts.data && hosts.data.hosts.length === 0 && (
          <Text size="sm" c="dimmed">
            No SSH hosts yet.
          </Text>
        )}
        {hosts.data && hosts.data.hosts.length > 0 && (
          <Table.ScrollContainer minWidth={560}>
            <Table verticalSpacing={4} fz="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Host</Table.Th>
                  <Table.Th>Key</Table.Th>
                  <Table.Th>Added</Table.Th>
                  <Table.Th />
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {hosts.data.hosts.map((h) => (
                  <Table.Tr key={`${h.host} ${h.fingerprint}`}>
                    <Table.Td>{h.host}</Table.Td>
                    <Table.Td>
                      <Text size="xs" ff="monospace" style={{ wordBreak: "break-all" }}>
                        {h.type} {h.fingerprint}
                      </Text>
                    </Table.Td>
                    <Table.Td>{formatTime(h.addedAt, prefs.dateFormat).text}</Table.Td>
                    <Table.Td>
                      <Group justify="flex-end">
                        <Tooltip label="Forget">
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            aria-label={`Forget ${h.host}`}
                            onClick={() => setForgetting(h)}
                          >
                            <IconTrash size={16} />
                          </ActionIcon>
                        </Tooltip>
                      </Group>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
      </Stack>

      {editing && (
        <CredentialDialog
          credential={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
        />
      )}
      <Modal opened={!!removing} onClose={() => setRemoving(null)} title="Remove credential">
        {removing && (
          <Stack>
            <Text size="sm">
              Remove <b>{removing.name}</b>?
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setRemoving(null)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={remove.isPending}
                onClick={() => remove.mutate(removing.id)}
              >
                Remove
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
      <Modal opened={!!forgetting} onClose={() => setForgetting(null)} title="Forget host key">
        {forgetting && (
          <Stack>
            <Text size="sm">
              Forget the key of <b>{forgetting.host}</b>? The next connection trusts whatever key
              the host presents.
            </Text>
            <Group justify="flex-end">
              <Button variant="default" onClick={() => setForgetting(null)}>
                Cancel
              </Button>
              <Button
                color="red"
                loading={forget.isPending}
                onClick={() => forget.mutate(forgetting.host)}
              >
                Forget
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </Paper>
  );
}

function CopyPublicKey({ value, name }: { value: string; name: string }) {
  return (
    <CopyButton value={value}>
      {({ copied, copy }) => (
        <Tooltip label={copied ? "Copied" : "Copy the public key (add it as a deploy key)"}>
          <ActionIcon
            variant="subtle"
            color={copied ? "teal" : "gray"}
            aria-label={`Copy the public key of ${name}`}
            onClick={copy}
          >
            {copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
          </ActionIcon>
        </Tooltip>
      )}
    </CopyButton>
  );
}

function CredentialDialog({
  credential,
  onClose,
}: {
  credential: GitCredential | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(credential?.name ?? "");
  const [kind, setKind] = useState<Kind>(credential?.kind ?? "token");
  const [username, setUsername] = useState(credential?.username ?? "");
  const [secret, setSecret] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [generate, setGenerate] = useState(!credential);
  const [saved, setSaved] = useState<GitCredential | null>(null);
  const keepSecret = !!credential && credential.kind === kind;
  const generating = kind === "ssh" && generate;

  const save = useMutation({
    mutationFn: () => {
      const body: Input = {
        name: name.trim(),
        kind,
        username: username.trim(),
        secret: generating ? "" : secret,
        passphrase: generating ? "" : passphrase,
        generate: generating,
      };
      return credential
        ? unwrap(
            api.PUT("/api/v1/git/credentials/{id}", {
              params: { path: { id: credential.id } },
              body,
            }),
          )
        : unwrap(api.POST("/api/v1/git/credentials", { body }));
    },
    onSuccess: (c) => {
      void queryClient.invalidateQueries({ queryKey: gitCredentialsQuery.queryKey });
      // Show a new public key so it can be added as a deploy key right away.
      if (generating && c.publicKey) setSaved(c);
      else onClose();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const needsSecret = !generating && !keepSecret && !secret;

  if (saved?.publicKey) {
    return (
      <Modal opened onClose={onClose} title="SSH key created" size="lg">
        <Stack gap="sm">
          <Text size="sm">
            Add this public key to the repository as a read-only deploy key (GitHub: Settings →
            Deploy keys; GitLab: Settings → Repository → Deploy keys).
          </Text>
          <Group gap="xs" wrap="nowrap">
            <Code fz="xs" style={{ flex: 1, wordBreak: "break-all" }} aria-label="Public key">
              {saved.publicKey}
            </Code>
            <CopyPublicKey value={saved.publicKey} name={saved.name} />
          </Group>
          <Group justify="flex-end">
            <Button onClick={onClose}>Done</Button>
          </Group>
        </Stack>
      </Modal>
    );
  }

  return (
    <Modal
      opened
      onClose={onClose}
      title={credential ? `Edit ${credential.name}` : "Add Git credential"}
      size="lg"
    >
      <form onSubmit={submit}>
        <Stack gap="sm">
          <TextInput
            label="Name"
            required
            data-autofocus
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <SegmentedControl
            value={kind}
            onChange={(v) => setKind(v as Kind)}
            data={[
              { value: "token", label: "Token (HTTPS)" },
              { value: "ssh", label: "SSH key" },
            ]}
          />
          <TextInput
            label="User name"
            description={
              kind === "ssh"
                ? "SSH user; empty: git"
                : "Empty: git (enough for GitHub and GitLab tokens)"
            }
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
          />
          {kind === "token" && (
            <PasswordInput
              label="Token or password"
              description={
                keepSecret ? "Empty: keep the stored token" : "A read-only access token is enough"
              }
              required={!keepSecret}
              value={secret}
              onChange={(e) => setSecret(e.currentTarget.value)}
            />
          )}
          {kind === "ssh" && (
            <>
              <Checkbox
                label={
                  credential ? "Replace with a new generated key" : "Generate a new ed25519 key"
                }
                checked={generate}
                onChange={(e) => setGenerate(e.currentTarget.checked)}
              />
              {!generate && (
                <>
                  <Textarea
                    label="Private key"
                    description={
                      keepSecret
                        ? "Empty: keep the stored key"
                        : "OpenSSH or PEM format, starting with -----BEGIN"
                    }
                    required={!keepSecret}
                    autosize
                    minRows={4}
                    maxRows={10}
                    ff="monospace"
                    value={secret}
                    onChange={(e) => setSecret(e.currentTarget.value)}
                  />
                  <PasswordInput
                    label="Passphrase"
                    description="Only for an encrypted key"
                    value={passphrase}
                    onChange={(e) => setPassphrase(e.currentTarget.value)}
                  />
                </>
              )}
              {credential?.publicKey && !generate && (
                <Text size="xs" c="dimmed" style={{ wordBreak: "break-all" }}>
                  Public key: <Code fz="xs">{credential.publicKey}</Code>
                </Text>
              )}
            </>
          )}
          {credential && credential.stacks > 0 && (
            <Text size="xs" c="dimmed">
              {credential.stacks} Git stack(s) use this credential from their next pull.
            </Text>
          )}
          {save.isError && (
            <Alert color="red" title="Cannot save the credential">
              {save.error.message}
            </Alert>
          )}
          <Group justify="flex-end">
            <Button variant="default" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" loading={save.isPending} disabled={!name.trim() || needsSecret}>
              Save
            </Button>
          </Group>
        </Stack>
      </form>
    </Modal>
  );
}
