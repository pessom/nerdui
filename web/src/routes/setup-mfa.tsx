// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Text } from "@mantine/core";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { ApiError } from "../api/client";
import { keys, meQuery } from "../api/queries";
import { AuthCard } from "../components/AuthCard";
import { MFASetup } from "../features/mfa/MFASetup";
import { safeRedirect } from "../lib/redirect";

/** Required two-factor setup (#75): the administrator requires it from every user. */
export const Route = createFileRoute("/setup-mfa")({
  validateSearch: (search: Record<string, unknown>): { redirect?: string } => ({
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const session = await context.queryClient.ensureQueryData(meQuery).catch((err: unknown) => {
      if (err instanceof ApiError && err.status === 401) throw redirect({ to: "/login" });
      throw err;
    });
    if (!session.mfaSetupRequired) throw redirect({ to: safeRedirect(search.redirect) });
  },
  component: SetupMFAPage,
});

function SetupMFAPage() {
  const { redirect: target } = Route.useSearch();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  return (
    <AuthCard title="Set up two-factor authentication">
      <Text size="sm">
        Your administrator requires two-factor authentication. Set it up to continue.
      </Text>
      <MFASetup
        onDone={() => {
          void queryClient
            .invalidateQueries({ queryKey: keys.me })
            .then(() => navigate({ to: safeRedirect(target) }));
        }}
      />
    </AuthCard>
  );
}
