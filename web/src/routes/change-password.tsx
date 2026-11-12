// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Text } from "@mantine/core";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { ApiError } from "../api/client";
import { meQuery } from "../api/queries";
import { AuthCard } from "../components/AuthCard";
import { ChangePasswordForm } from "../features/users/ChangePasswordForm";
import { safeRedirect } from "../lib/redirect";

/** Forced change of a temporary password set by an administrator (#74). */
export const Route = createFileRoute("/change-password")({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const session = await context.queryClient.ensureQueryData(meQuery).catch((err: unknown) => {
      if (err instanceof ApiError && err.status === 401) throw redirect({ to: "/login" });
      throw err;
    });
    if (!session.user.mustChangePassword) throw redirect({ to: safeRedirect(search.redirect) });
  },
  component: ChangePasswordPage,
});

function ChangePasswordPage() {
  const { redirect: target } = Route.useSearch();
  const navigate = useNavigate();
  return (
    <AuthCard title="Choose a new password">
      <Text size="sm">
        An administrator set a temporary password for your account. Replace it with your own to
        continue.
      </Text>
      <ChangePasswordForm
        currentLabel="Temporary password"
        submitLabel="Set password and continue"
        onDone={() => navigate({ to: safeRedirect(target) })}
      />
    </AuthCard>
  );
}
