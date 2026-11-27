// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { Alert, Stack } from "@mantine/core";
import type { ReactNode } from "react";
import { useCan, type Permission } from "../lib/permissions";

/**
 * Shows a page's controls disabled, with a note, to a caller without `action` (ADR-0058):
 * a disabled fieldset turns off every input and button inside it.
 */
export function ReadOnlyUnless({ action, children }: { action: Permission; children: ReactNode }) {
  const can = useCan();
  if (can(action)) return <>{children}</>;
  return (
    <>
      <Alert color="gray" variant="light" title="Read only">
        Your role can view these settings but not change them.
      </Alert>
      <fieldset
        disabled
        aria-label="Read only"
        style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}
      >
        <Stack>{children}</Stack>
      </fieldset>
    </>
  );
}
