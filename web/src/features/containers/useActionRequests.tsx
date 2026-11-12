// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useState } from "react";
import { actionInfo, canApply, type ContainerAction } from "./actions";
import { ConfirmDialog, type TableActions } from "./ContainerActions";
import type { Container } from "./format";

/** Asks for confirmation where needed and runs the action on the applicable containers. */
export function useActionRequests(actions: TableActions) {
  const [pending, setPending] = useState<{ action: ContainerAction; containers: Container[] }>();
  const request = (action: ContainerAction, containers: Container[]) => {
    const targets = containers.filter((c) => canApply(action, c));
    if (targets.length === 0) return;
    if (actionInfo[action].confirm) setPending({ action, containers: targets });
    else actions.run({ action, containers: targets });
  };
  const dialog = (
    <ConfirmDialog
      pending={pending}
      onCancel={() => setPending(undefined)}
      onConfirm={(opts) => {
        if (pending) actions.run({ ...pending, ...opts });
        setPending(undefined);
      }}
    />
  );
  return { request, dialog };
}
