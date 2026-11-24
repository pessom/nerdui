// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitCredential, Job } from "../../api/queries";
import { renderApp, signedIn } from "../../test/render";
import { SilentWebSocket, fakeApi, json } from "../../test/server";
import { isSSH, repoName, webURL } from "./gitUrl";

vi.mock("../jobs/JobTerminal", () => ({
  default: ({ jobId }: { jobId: string }) => <pre aria-label="Job output">output of {jobId}</pre>,
}));

const base = "/api/v1/environments/local/stacks";

const job = (id: string, kind: string, title: string): Job => ({
  id,
  kind,
  title,
  environment: "local",
  namespace: "default",
  status: "running",
  startedAt: "2027-03-01T10:00:00Z",
  user: "admin",
});

const [github, deployKey]: [GitCredential, GitCredential] = [
  {
    id: 1,
    name: "github",
    kind: "token",
    username: "me",
    passphrase: false,
    stacks: 1,
    updatedAt: "2027-03-01T10:00:00Z",
  },
  {
    id: 2,
    name: "deploy-key",
    kind: "ssh",
    publicKey: "ssh-ed25519 AAAA nerdui",
    passphrase: false,
    stacks: 0,
    updatedAt: "2027-03-01T10:00:00Z",
  },
];

const web = {
  name: "web",
  mode: "git",
  path: "/var/lib/nerdui/git/web/deploy",
  composeFile: "compose.yaml",
  status: "stopped",
  containers: 0,
  running: 0,
  services: [{ name: "web", defined: true, running: 0, containers: [] }],
  git: {
    url: "https://github.com/acme/web.git",
    ref: "main",
    fullRef: "refs/heads/main",
    composePath: "deploy/compose.yaml",
    credentialId: 1,
    commit: {
      hash: "0123456789abcdef0123456789abcdef01234567",
      message: "Bump nginx",
      author: "Dev",
      date: "2027-03-01T09:00:00Z",
    },
    pulledAt: "2027-03-01T10:00:00Z",
    lastError: "authentication required",
    autoUpdate: "off",
    pollInterval: 0,
    webhook: false,
  },
};

describe("Git URL helpers", () => {
  it("suggests names, detects SSH and links to the web page", () => {
    expect(repoName("https://github.com/acme/My_App.git/")).toBe("my_app");
    expect(repoName("git@github.com:acme/web.git")).toBe("web");
    expect(isSSH("git@github.com:acme/web.git")).toBe(true);
    expect(isSSH("ssh://git@host/x.git")).toBe(true);
    expect(isSSH("https://github.com/acme/web.git")).toBe(false);
    expect(webURL("git@github.com:acme/web.git")).toBe("https://github.com/acme/web");
    expect(webURL("https://user:tok@github.com/acme/web.git")).toBe("https://github.com/acme/web");
    expect(webURL("ssh://git@host/x.git")).toBeUndefined();
  });
});

describe("Git stacks", () => {
  beforeEach(() => vi.stubGlobal("WebSocket", SilentWebSocket));
  afterEach(() => vi.unstubAllGlobals());

  it("clones a repository as a job with a matching credential", async () => {
    const started: Job[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () => json(200, { namespace: "default", stacks: [] }),
      "GET /api/v1/git/credentials": () => json(200, { credentials: [github, deployKey] }),
      "GET /api/v1/jobs": () => json(200, { jobs: started }),
      [`POST ${base}/git`]: () => {
        started.push(job("j1", "stack.git-clone", "Clone stack web"));
        return json(202, { job: started[0] });
      },
    });
    renderApp("/env/local/stacks");

    fireEvent.click(await screen.findByRole("button", { name: "From Git" }));
    const dialog = await screen.findByRole("dialog", { name: "New stack from Git" });
    fireEvent.change(within(dialog).getByLabelText(/^Repository URL/), {
      target: { value: "https://github.com/acme/web.git" },
    });
    expect(within(dialog).getByLabelText(/^Name/)).toHaveValue("web");
    fireEvent.change(within(dialog).getByLabelText(/^Compose file/), {
      target: { value: "deploy" },
    });
    // HTTPS URLs offer only token credentials.
    fireEvent.click(await within(dialog).findByLabelText(/^Credential/, { selector: "input" }));
    expect(await screen.findByRole("option", { name: "github", hidden: true })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "deploy-key", hidden: true }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: "github", hidden: true }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Clone and deploy" }));

    const panel = await screen.findByRole("dialog", { name: "Jobs" });
    expect(
      await within(panel).findByRole("region", { name: "Clone stack web" }),
    ).toBeInTheDocument();
    const post = calls.find((c) => c.method === "POST" && c.path === `${base}/git`);
    expect(post?.body).toEqual({
      name: "web",
      url: "https://github.com/acme/web.git",
      ref: "",
      composePath: "deploy",
      credentialId: 1,
      deploy: true,
    });
  });

  it("shows the commit, pulls and removes a Git stack; its compose file is read-only", async () => {
    const started: Job[] = [];
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}`]: () => json(200, { namespace: "default", stacks: [] }),
      [`GET ${base}/web`]: () => json(200, web),
      [`GET ${base}/web/files/compose`]: () =>
        json(200, {
          kind: "compose",
          name: "compose.yaml",
          content: "services: {}\n",
          version: "v1",
          exists: true,
        }),
      "GET /api/v1/jobs": () => json(200, { jobs: started }),
      [`POST ${base}/web/git/pull`]: () => {
        started.push(job("j1", "stack.git-pull", "Pull stack web"));
        return json(202, { job: started[0] });
      },
      [`DELETE ${base}/web/git`]: () => new Response(null, { status: 204 }),
    });
    renderApp("/env/local/stacks/web");

    const repo = await screen.findByRole("region", { name: "Git repository" });
    expect(within(repo).getByRole("link", { name: web.git.url })).toHaveAttribute(
      "href",
      "https://github.com/acme/web",
    );
    expect(within(repo).getByText("0123456")).toBeInTheDocument();
    expect(within(repo).getByText("authentication required")).toBeInTheDocument();

    fireEvent.click(within(repo).getByRole("button", { name: "Pull and redeploy" }));
    const panel = await screen.findByRole("dialog", { name: "Jobs" });
    expect(
      await within(panel).findByRole("region", { name: "Pull stack web" }),
    ).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${base}/web/git/pull`)?.body).toEqual({
      redeploy: true,
    });
    fireEvent.keyDown(panel, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Jobs" })).not.toBeInTheDocument(),
    );

    fireEvent.click(screen.getByRole("tab", { name: "compose.yaml" }));
    expect(await screen.findByText("From the Git repository")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /review and save/i })).not.toBeInTheDocument();

    fireEvent.click(within(repo).getByRole("button", { name: "Remove" }));
    const remove = await screen.findByRole("dialog", { name: "Remove stack web" });
    const confirm = within(remove).getByRole("button", { name: "Remove" });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(remove).getByLabelText("Type web to confirm"), {
      target: { value: "web" },
    });
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(
        calls
          .find((c) => c.method === "DELETE" && c.path === `${base}/web/git`)
          ?.query.get("confirm"),
      ).toBe("web"),
    );
    expect(await screen.findByRole("heading", { name: "Stacks" })).toBeInTheDocument();
  });
});

describe("Git credentials", () => {
  it("generates an SSH key, shows its public key and forgets a host", async () => {
    let list: GitCredential[] = [github];
    let hosts = [
      {
        host: "github.com:22",
        type: "ssh-ed25519",
        fingerprint: "SHA256:abc",
        addedAt: "2027-03-01T10:00:00Z",
      },
    ];
    const { calls } = fakeApi({
      ...signedIn,
      "GET /api/v1/git/credentials": () => json(200, { credentials: list }),
      "GET /api/v1/git/known-hosts": () => json(200, { hosts }),
      "POST /api/v1/git/credentials": () => {
        list = [...list, deployKey];
        return json(201, deployKey);
      },
      "DELETE /api/v1/git/known-hosts": () => {
        hosts = [];
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/env/local/settings");

    const card = await screen.findByRole("region", { name: "Git credentials" });
    expect(await within(card).findByText("github")).toBeInTheDocument();
    // In use: cannot be removed.
    expect(within(card).getByRole("button", { name: "Remove github" })).toBeDisabled();

    fireEvent.click(within(card).getByRole("button", { name: "Add credential" }));
    const dialog = await screen.findByRole("dialog", { name: "Add Git credential" });
    fireEvent.change(within(dialog).getByLabelText(/^Name/), { target: { value: "deploy-key" } });
    fireEvent.click(within(dialog).getByText("SSH key"));
    expect(within(dialog).getByLabelText("Generate a new ed25519 key")).toBeChecked();
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
    const created = await screen.findByRole("dialog", { name: "SSH key created" });
    expect(within(created).getByLabelText("Public key")).toHaveTextContent(
      "ssh-ed25519 AAAA nerdui",
    );
    expect(
      calls.find((c) => c.method === "POST" && c.path === "/api/v1/git/credentials")?.body,
    ).toEqual({
      name: "deploy-key",
      kind: "ssh",
      username: "",
      secret: "",
      passphrase: "",
      generate: true,
    });
    fireEvent.click(within(created).getByRole("button", { name: "Done" }));
    expect(await within(card).findByText("deploy-key")).toBeInTheDocument();

    fireEvent.click(within(card).getByRole("button", { name: "Forget github.com:22" }));
    const forget = await screen.findByRole("dialog", { name: "Forget host key" });
    fireEvent.click(within(forget).getByRole("button", { name: "Forget" }));
    expect(await within(card).findByText("No SSH hosts yet.")).toBeInTheDocument();
    expect(
      calls
        .find((c) => c.method === "DELETE" && c.path === "/api/v1/git/known-hosts")
        ?.query.get("host"),
    ).toBe("github.com:22");
  });
});

describe("Git stack automatic updates", () => {
  it("saves the mode and interval and shows a new webhook URL once", async () => {
    let git: Record<string, unknown> = { ...web.git, lastError: undefined };
    const { calls } = fakeApi({
      ...signedIn,
      [`GET ${base}/web`]: () => json(200, { ...web, git }),
      [`PUT ${base}/web/git/auto`]: (c) => {
        git = { ...git, ...(c.body as object) };
        return new Response(null, { status: 204 });
      },
      [`POST ${base}/web/git/webhook`]: () => {
        git = { ...git, webhook: true };
        return json(201, { path: "/api/v1/hooks/git/tok123" });
      },
      [`DELETE ${base}/web/git/webhook`]: () => {
        git = { ...git, webhook: false };
        return new Response(null, { status: 204 });
      },
    });
    renderApp("/env/local/stacks/web");

    const auto = await screen.findByRole("region", { name: "Automatic updates" });
    const interval = within(auto).getByLabelText("Check the repository", { selector: "input" });
    expect(interval).toBeDisabled();
    fireEvent.click(within(auto).getByLabelText("On a new commit", { selector: "input" }));
    fireEvent.click(await screen.findByRole("option", { name: "Pull and redeploy", hidden: true }));
    await waitFor(() =>
      expect(calls.find((c) => c.path === `${base}/web/git/auto`)?.body).toEqual({
        autoUpdate: "deploy",
        pollInterval: 0,
      }),
    );
    await waitFor(() => expect(interval).toBeEnabled());
    fireEvent.click(interval);
    fireEvent.click(await screen.findByRole("option", { name: "Every 15 minutes", hidden: true }));
    await waitFor(() =>
      expect(calls.filter((c) => c.path === `${base}/web/git/auto`).at(-1)?.body).toEqual({
        autoUpdate: "deploy",
        pollInterval: 900,
      }),
    );

    fireEvent.click(within(auto).getByRole("button", { name: "Create webhook" }));
    expect(await within(auto).findByLabelText("Webhook URL")).toHaveTextContent(
      `${window.location.origin}/api/v1/hooks/git/tok123`,
    );
    expect(await within(auto).findByText("active")).toBeInTheDocument();
    fireEvent.click(within(auto).getByRole("button", { name: "Remove" }));
    expect(await within(auto).findByRole("button", { name: "Create webhook" })).toBeInTheDocument();
    expect(within(auto).queryByLabelText("Webhook URL")).not.toBeInTheDocument();
  });
});
