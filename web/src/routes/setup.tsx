// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Code, PasswordInput, Stack, Text, TextInput } from "@mantine/core";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, setCsrfToken, unwrap } from "../api/client";
import { keys, setupQuery } from "../api/queries";
import { AuthCard } from "../components/AuthCard";

const MIN_PASSWORD = 12;

export const Route = createFileRoute("/setup")({
  beforeLoad: async ({ context }) => {
    const setup = await context.queryClient.fetchQuery(setupQuery);
    if (!setup.required) throw redirect({ to: "/login" });
  },
  component: SetupPage,
});

function SetupPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [token, setToken] = useState("");
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");

  const tooShort = password.length > 0 && [...password].length < MIN_PASSWORD;
  const mismatch = confirm.length > 0 && confirm !== password;

  const setup = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/api/v1/setup", { body: { token: token.trim(), username, password } })),
    onSuccess: async (session) => {
      setCsrfToken(session.csrfToken);
      queryClient.setQueryData(keys.me, session);
      queryClient.setQueryData(keys.setup, { required: false });
      await navigate({ to: "/" });
    },
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!tooShort && !mismatch) setup.mutate();
  };

  return (
    <AuthCard title="Set up nerdui">
      <form onSubmit={onSubmit}>
        <Stack>
          <Text size="sm">
            Create the administrator account. The one-time setup token is printed in the server log,
            e.g. <Code>journalctl -u nerdui | grep setup_token</Code>.
          </Text>
          {setup.error && <Alert color="red">{setup.error.message}</Alert>}
          <TextInput
            label="Setup token"
            required
            autoFocus
            autoComplete="off"
            value={token}
            onChange={(e) => setToken(e.currentTarget.value)}
          />
          <TextInput
            label="Admin username"
            required
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
          />
          <PasswordInput
            label="Password"
            description={`At least ${MIN_PASSWORD} characters`}
            required
            autoComplete="new-password"
            value={password}
            error={tooShort ? `Use at least ${MIN_PASSWORD} characters` : undefined}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
          <PasswordInput
            label="Repeat password"
            required
            autoComplete="new-password"
            value={confirm}
            error={mismatch ? "Passwords do not match" : undefined}
            onChange={(e) => setConfirm(e.currentTarget.value)}
          />
          <Button type="submit" loading={setup.isPending} disabled={tooShort || mismatch}>
            Create admin account
          </Button>
        </Stack>
      </form>
    </AuthCard>
  );
}
