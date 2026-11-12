// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { createContext, useContext } from "react";

export interface JobsPanel {
  /** Opens the jobs panel, optionally with one job expanded. */
  open: (jobId?: string) => void;
}

export const JobsContext = createContext<JobsPanel>({ open: () => {} });

export function useJobsPanel(): JobsPanel {
  return useContext(JobsContext);
}
