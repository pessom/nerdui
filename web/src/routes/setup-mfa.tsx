// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { SegmentedControl, Text } from "@mantine/core";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { ApiError } from "../api/client";
import { keys, meQuery } from "../api/queries";
import { AuthCard } from "../components/AuthCard";
import { MFASetup } from "../features/mfa/MFASetup";
import { PasskeyAdd } from "../features/mfa/Passkeys";
import { passkeysSupported } from "../lib/webauthn";
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
  const [kind, setKind] = useState<"app" | "passkey">("app");
  const done = () => {
    // No component observes the session here, so invalidating would only mark it stale
    // and the app would send the user back with the cached one: fetch it again.
    void queryClient
      .refetchQueries({ queryKey: keys.me })
      .then(() => navigate({ to: safeRedirect(target) }));
  };
  return (
    <AuthCard title="Set up two-factor authentication">
      <Text size="sm">
        Your administrator requires two-factor authentication. Set it up to continue.
      </Text>
      {passkeysSupported() && (
        <SegmentedControl
          fullWidth
          value={kind}
          onChange={(v) => setKind(v as "app" | "passkey")}
          data={[
            { value: "app", label: "Authenticator app" },
            { value: "passkey", label: "Passkey" },
          ]}
        />
      )}
      {kind === "passkey" ? <PasskeyAdd onDone={done} /> : <MFASetup onDone={done} />}
    </AuthCard>
  );
}
