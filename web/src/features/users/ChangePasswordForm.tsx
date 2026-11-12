// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Button, PasswordInput, Stack } from "@mantine/core";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { ApiError, api, unwrap } from "../../api/client";
import { keys } from "../../api/queries";
import { MIN_PASSWORD, tooShort } from "./password";

/**
 * Changes the caller's own password (#74). Used on the forced change page after a temporary
 * password and from the user menu. Other sessions of the user are signed out by the server.
 */
export function ChangePasswordForm({
  currentLabel = "Current password",
  submitLabel = "Change password",
  onDone,
}: {
  currentLabel?: string;
  submitLabel?: string;
  onDone: () => void | Promise<void>;
}) {
  const queryClient = useQueryClient();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const short = tooShort(next);
  const mismatch = confirm.length > 0 && confirm !== next;

  const change = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/api/v1/auth/password", {
          body: { currentPassword: current, newPassword: next },
        }),
      ),
    onSuccess: async (session) => {
      queryClient.setQueryData(keys.me, session);
      await onDone();
    },
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!short && !mismatch) change.mutate();
  };

  return (
    <form onSubmit={onSubmit}>
      <Stack>
        {change.error && <Alert color="red">{errorText(change.error)}</Alert>}
        <PasswordInput
          label={currentLabel}
          required
          autoFocus
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.currentTarget.value)}
        />
        <PasswordInput
          label="New password"
          description={`At least ${MIN_PASSWORD} characters`}
          required
          autoComplete="new-password"
          value={next}
          error={short ? `Use at least ${MIN_PASSWORD} characters` : undefined}
          onChange={(e) => setNext(e.currentTarget.value)}
        />
        <PasswordInput
          label="Repeat new password"
          required
          autoComplete="new-password"
          value={confirm}
          error={mismatch ? "Passwords do not match" : undefined}
          onChange={(e) => setConfirm(e.currentTarget.value)}
        />
        <Button type="submit" loading={change.isPending} disabled={short || mismatch}>
          {submitLabel}
        </Button>
      </Stack>
    </form>
  );
}

function errorText(err: Error) {
  if (err instanceof ApiError && err.code === "rate_limited") {
    return "Too many attempts. Wait a moment and try again.";
  }
  return err.message;
}
