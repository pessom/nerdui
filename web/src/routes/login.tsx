// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, Divider, PasswordInput, Stack, TextInput } from "@mantine/core";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { ApiError, api, setCsrfToken, unwrap } from "../api/client";
import { keys, setupQuery } from "../api/queries";
import type { components } from "../api/schema";
import { AuthCard } from "../components/AuthCard";
import { withBase } from "../lib/base";
import { safeRedirect } from "../lib/redirect";

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>): { redirect?: string; sso_error?: string } => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
    // Set by the single sign-on callback when signing in failed (#76).
    sso_error: typeof search.sso_error === "string" ? search.sso_error : undefined,
  }),
  beforeLoad: async ({ context }) => {
    const setup = await context.queryClient.fetchQuery(setupQuery).catch(() => null);
    if (setup?.required) throw redirect({ to: "/setup" });
  },
  component: LoginPage,
});

function LoginPage() {
  const { redirect: target, sso_error: ssoError } = Route.useSearch();
  const sso = useQuery({
    queryKey: ["auth", "oidc"],
    queryFn: () => unwrap(api.GET("/api/v1/auth/oidc")),
    retry: false,
  });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  // Set after the password when the account uses two-factor authentication (#75).
  const [mfaToken, setMfaToken] = useState<string>();
  const [code, setCode] = useState("");

  const finish = async (session: Session) => {
    setCsrfToken(session.csrfToken);
    queryClient.setQueryData(keys.me, session);
    const safe = safeRedirect(target);
    if (session.user.mustChangePassword) {
      await navigate({ to: "/change-password", search: { redirect: safe } });
    } else if (session.mfaSetupRequired) {
      await navigate({ to: "/setup-mfa", search: { redirect: safe } });
    } else {
      await navigate({ to: safe });
    }
  };

  const login = useMutation({
    mutationFn: () => unwrap(api.POST("/api/v1/auth/login", { body: { username, password } })),
    onSuccess: async (res) => {
      if ("mfaRequired" in res) {
        setMfaToken(res.mfaToken);
        return;
      }
      await finish(res);
    },
  });
  const second = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/auth/login/mfa", {
          body: { mfaToken: mfaToken ?? "", code: code.trim() },
        }),
      ),
    onSuccess: finish,
    onError: (err) => {
      // An expired or overused attempt starts over with the password.
      if (err instanceof ApiError && err.message.includes("expired")) setMfaToken(undefined);
    },
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate();
  };

  if (mfaToken) {
    return (
      <AuthCard title="Two-factor authentication">
        <form
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            second.mutate();
          }}
        >
          <Stack>
            {second.error && <Alert color="red">{mfaErrorText(second.error)}</Alert>}
            <TextInput
              label="Code"
              description="From your authenticator app, or one of your backup codes"
              autoComplete="one-time-code"
              inputMode="text"
              required
              autoFocus
              value={code}
              onChange={(e) => setCode(e.currentTarget.value)}
            />
            <Button type="submit" loading={second.isPending}>
              Verify
            </Button>
            <Button
              variant="subtle"
              size="xs"
              onClick={() => {
                setMfaToken(undefined);
                setCode("");
                second.reset();
              }}
            >
              Back
            </Button>
          </Stack>
        </form>
      </AuthCard>
    );
  }

  return (
    <AuthCard title="Sign in to nerdui">
      <form onSubmit={onSubmit}>
        <Stack>
          {ssoError && !login.error && (
            <Alert color="red" title="Single sign-on failed">
              {ssoError}
            </Alert>
          )}
          {sso.data?.enabled && (
            <>
              <Button
                component="a"
                variant="default"
                href={withBase(
                  `/api/v1/auth/oidc/login?redirect=${encodeURIComponent(safeRedirect(target))}`,
                )}
              >
                {sso.data.buttonLabel}
              </Button>
              <Divider label="or with a password" labelPosition="center" />
            </>
          )}
          {login.error && <Alert color="red">{errorText(login.error)}</Alert>}
          <TextInput
            label="Username"
            autoComplete="username"
            required
            autoFocus={!sso.data?.enabled}
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

type Session = components["schemas"]["SessionResponse"];

function mfaErrorText(err: unknown) {
  if (err instanceof ApiError && err.code === "rate_limited") {
    return "Too many attempts. Wait a moment and try again.";
  }
  if (err instanceof ApiError && err.status === 401) {
    return err.message.includes("expired")
      ? "The sign-in attempt expired. Enter your password again."
      : "Invalid or already used code.";
  }
  return err instanceof Error ? err.message : "Verification failed.";
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
