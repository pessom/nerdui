// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useState } from "react";
import { api, type components } from "./api/client";
import { Footer } from "./Footer";

type VersionResponse = components["schemas"]["VersionResponse"];

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
      </main>
      <Footer build={info?.nerdui} />
    </>
  );
}
