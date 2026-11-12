// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, PasswordInput, Stack, TextInput } from "@mantine/core";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ApiError, api, setCsrfToken, unwrap } from "../api/client";
import { keys, setupQuery } from "../api/queries";
import { AuthCard } from "../components/AuthCard";
import { safeRedirect } from "../lib/redirect";

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  beforeLoad: async ({ context }) => {
    const setup = await context.queryClient.fetchQuery(setupQuery).catch(() => null);
    if (setup?.required) throw redirect({ to: "/setup" });
  },
  component: LoginPage,
});

function LoginPage() {
  const { redirect: target } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const login = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/auth/login", { body: { username, password } })),
    onSuccess: async (session) => {
      setCsrfToken(session.csrfToken);
      queryClient.setQueryData(keys.me, session);
      const safe = safeRedirect(target);
      if (session.user.mustChangePassword) {
        await navigate({ to: "/change-password", search: { redirect: safe } });
      } else {
        await navigate({ to: safe });
      }
    },
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  return (
    <AuthCard title="Sign in to nerdui">
      <form onSubmit={onSubmit}>
        <Stack>
          {login.error && <Alert color="red">{errorText(login.error)}</Alert>}
          <TextInput
            label="Username"
            autoComplete="username"
            required
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.currentTarget.value)}
          />
          <PasswordInput
            label="Password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
          <Button type="submit" loading={login.isPending}>
            Sign in
          </Button>
        </Stack>
      </form>
    </AuthCard>
  );
}

function errorText(err: unknown) {
  if (err instanceof ApiError && err.code === "rate_limited") {
    return "Too many attempts. Wait a moment and try again.";
  }
  if (err instanceof ApiError && err.status === 401) return "Invalid username or password.";
  if (err instanceof ApiError && err.status === 403 && err.code === "forbidden") {
    return "This account is disabled. Ask an administrator to enable it.";
  }
  return err instanceof Error ? err.message : "Sign-in failed.";
}
