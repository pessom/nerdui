// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { api, type components } from "./api/client";
import { Footer } from "./Footer";

type VersionResponse = components["schemas"]["VersionResponse"];

type RuntimeVersions = components["schemas"]["RuntimeVersions"];

/** Warnings for the runtime support policy (ADR-0011, ADR-0012). */
function RuntimeBanners({ runtime }: { runtime: RuntimeVersions }) {
  const n = runtime.nerdctl;
  return (
    <>
      {!n.compatible && !n.checkSkipped && (
        <p role="alert">
          Read-only mode: host nerdctl {n.version || "is missing"}
          {n.version ? " is incompatible" : ""}; nerdctl {n.required}.x is required.
        </p>
      )}
      {n.checkSkipped && !n.compatible && (
        <p role="alert">
          Unsupported configuration: nerdctl version check is disabled (host {n.version || "?"},
          required {n.required}.x).
        </p>
      )}
      {runtime.containerd && !runtime.containerdSupported && (
        <p role="alert">
          containerd {runtime.containerd} is not a supported version; nerdui is not tested with it.
        </p>
      )}
    </>
  );
}

export default function App() {
  const [info, setInfo] = useState<VersionResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .GET("/api/v1/version")
      .then(({ data, error: apiError }) => {
        if (data) setInfo(data);
        else setError(apiError ? JSON.stringify(apiError) : "unknown error");
      })
      .catch((e: unknown) => setError(String(e)));
  }, []);

  return (
    <>
      <main>
        <h1>nerdui</h1>
        <p>Web UI for nerdctl / containerd</p>
        {error && <p role="alert">Error: {error}</p>}
        {info && (
          <dl>
            <dt>nerdui</dt>
            <dd>{info.nerdui.version}</dd>
            {info.runtime && (
              <>
                <dt>containerd</dt>
                <dd>{info.runtime.containerd || "not connected"}</dd>
                <dt>nerdctl library</dt>
                <dd>{info.runtime.nerdctlLibrary}</dd>
              </>
            )}
          </dl>
        )}
        {info && !info.runtime && <p>Sign in to see runtime details.</p>}
        {info?.error && <p role="alert">Runtime: {info.error}</p>}
        {info?.runtime && <RuntimeBanners runtime={info.runtime} />}
      </main>
      <Footer build={info?.nerdui} />
    </>
  );
}
