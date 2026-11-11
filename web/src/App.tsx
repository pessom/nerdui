import { useEffect, useState } from "react";
import { api, type components } from "./api/client";

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
    <main>
      <h1>nerdui</h1>
      <p>Web UI for nerdctl / containerd</p>
      {error && <p role="alert">Error: {error}</p>}
      {info && (
        <dl>
          <dt>nerdui</dt>
          <dd>{info.nerdui.version}</dd>
          <dt>containerd</dt>
          <dd>{info.runtime.containerd || "not connected"}</dd>
          <dt>nerdctl library</dt>
          <dd>{info.runtime.nerdctlLibrary}</dd>
        </dl>
      )}
      {info?.error && <p role="alert">Runtime: {info.error}</p>}
    </main>
  );
}
