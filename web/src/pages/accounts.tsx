import { ExternalLink } from "lucide-react";
import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { keys, queryClient, useForges, useJob, useJobs } from "@/api/queries";
import { verb } from "@/api/request";
import { jobStartError } from "@/api/run-job";
import type { Forge, ForgeLogin, ForgeStatus, Job } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { useConfirm } from "@/components/confirm";
import { DeviceBox } from "@/components/device-box";
import { Facts, Mono } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { CardGrid, Page, PageHeader, SectionTitle } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { forgeJobId, tokenPage } from "./accounts-logic";

/** The CLIs that sign in to one host with a device code. */
const DEVICE_FORGES = [
  { id: "gh", name: "GitHub" },
  { id: "glab", name: "GitLab" },
] as const;

type DeviceForge = (typeof DEVICE_FORGES)[number];

const DEFAULT_GITEA = "https://gitea.com";

// Accounts: git identity, and the GitHub, GitLab and Gitea CLIs that sign
// git in over HTTPS.
export function AccountsPage() {
  useTitle("Accounts");
  const forges = useForges();
  let body = <LoadingRows n={3} />;
  if (forges.isError) body = <InlineAlert tone="bad">{errorText(forges.error)}</InlineAlert>;
  else if (forges.data) body = <Accounts data={forges.data} />;
  return (
    <Page narrow>
      <PageHeader
        title="Accounts"
        description="Your git identity and code-host sign-ins. Tokens stay in your home volume."
      />
      {body}
    </Page>
  );
}

function Accounts({ data }: { data: ForgeStatus }) {
  const [confirmDialog, ask] = useConfirm();
  // name picks one of tea's logins; gh and glab have one host each.
  const signOut = (forge: string, host: string, body: string, name = "") =>
    ask({
      title: `Sign out of ${host}?`,
      body,
      confirm: "Sign out",
      danger: true,
      action: async () => {
        try {
          await verb("forge.logout", name ? { forge, name } : { forge });
          notify(`Signed out of ${host}.`);
          void queryClient.invalidateQueries({ queryKey: keys.forges });
        } catch (err) {
          notify(errorText(err), "bad");
          return false;
        }
      },
    });
  return (
    <>
      <GitCard initial={data.git} />
      <section className="flex flex-col gap-3" aria-labelledby="code-hosts">
        <div className="flex flex-col gap-0.5">
          <SectionTitle>
            <span id="code-hosts">Code hosts</span>
          </SectionTitle>
          <p className="text-fg-muted">
            Each CLI works in every terminal and signs git push and clone in over HTTPS.
          </p>
        </div>
        <CardGrid>
          {DEVICE_FORGES.map((f) => (
            <ForgeCard
              key={f.id}
              spec={f}
              forge={data.forges[f.id]}
              onSignOut={() =>
                signOut(
                  f.id,
                  f.name,
                  `${f.id} forgets its token for ${data.forges[f.id]?.host ?? f.name}. The token stays valid until you revoke it on ${f.name}.`,
                )
              }
            />
          ))}
        </CardGrid>
        <TeaCard
          forge={data.forges.tea}
          onSignOut={(l) =>
            signOut(
              "tea",
              l.name,
              `tea forgets its token for ${l.name}. The token stays valid until you delete it on the server.`,
              l.name,
            )
          }
        />
      </section>
      {confirmDialog}
    </>
  );
}

function GitCard({ initial }: { initial: { name: string; email: string } }) {
  const [name, setName] = useState(initial.name);
  const [email, setEmail] = useState(initial.email);
  const [saved, setSaved] = useState(initial);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const nameId = useId();
  const emailId = useId();
  const unchanged = name.trim() === saved.name && email.trim() === saved.email;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const next = { name: name.trim(), email: email.trim() };
      await verb("git.identity", next);
      setSaved(next);
      setError("");
      notify("Git identity saved.");
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };
  return (
    <PanelCard
      title="Git identity"
      badge={saved.name && saved.email ? null : <StatusBadge state="not_set" />}
    >
      <form
        noValidate
        onSubmit={(e) => void submit(e)}
        className="flex max-w-(--form-max) flex-col gap-4"
      >
        <p className="text-fg-muted">Every commit you or an agent makes carries this name.</p>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
          <FormField id={nameId} label="Name">
            <Input
              {...fieldProps(nameId)}
              autoComplete="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </FormField>
          <FormField id={emailId} label="Email">
            <Input
              {...fieldProps(emailId)}
              type="email"
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </FormField>
        </div>
        <div aria-live="polite">{error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}</div>
        <div>
          <BusyButton type="submit" busy={busy} busyLabel="Saving…" disabled={unchanged}>
            Save
          </BusyButton>
        </div>
      </form>
    </PanelCard>
  );
}

function ForgeCard({
  spec,
  forge,
  onSignOut,
}: {
  spec: DeviceForge;
  forge: Forge | undefined;
  onSignOut: () => void;
}) {
  const { id, name } = spec;
  const jobs = useJobs();
  const [jobId, setJobId] = useState<number | null>(null);
  const [starting, setStarting] = useState(false);
  // Resume a sign-in started earlier, in this tab or another.
  const runningId = forgeJobId(jobs.data?.jobs, id);
  if (jobId === null && runningId !== null) setJobId(runningId);
  const watched: Job | undefined = useJob(jobId).data?.job;
  const live = jobId !== null && (!watched || watched.state === "running");
  const last = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (last.current === "running" && watched && watched.state !== "running") {
      const ok = watched.state === "succeeded";
      notify(
        ok ? `${name}: signed in.` : `${name}: sign-in did not complete; see Activity.`,
        ok ? "ok" : "bad",
      );
      void queryClient.invalidateQueries({ queryKey: keys.forges });
    }
    last.current = watched?.state;
  }, [watched, name]);

  const start = async () => {
    setStarting(true);
    try {
      const r = await verb<{ job: Job }>("forge.login", { forge: id });
      setJobId(r.job.id);
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
    } catch (err) {
      notify(jobStartError(err), "bad");
    }
    setStarting(false);
  };

  const cancel = async () => {
    if (jobId === null) return;
    try {
      await verb("login.cancel", { job: jobId });
    } catch (err) {
      notify(errorText(err), "bad");
    }
  };

  const signedIn = Boolean(forge?.signed_in);
  let actions: ReactNode;
  if (live) {
    actions = (
      <Button
        size="sm"
        variant="ghost"
        aria-label={`Cancel the ${name} sign-in`}
        onClick={() => void cancel()}
      >
        Cancel
      </Button>
    );
  } else if (signedIn) {
    actions = (
      <>
        <Button
          size="sm"
          variant="ghost"
          aria-label={`Sign in to ${name} again`}
          disabled={starting}
          onClick={() => void start()}
        >
          Sign in again
        </Button>
        <Button size="sm" variant="outline" aria-label={`Sign out of ${name}`} onClick={onSignOut}>
          Sign out
        </Button>
      </>
    );
  } else {
    actions = (
      <Button
        size="sm"
        aria-label={`Sign in to ${name}`}
        disabled={starting}
        onClick={() => void start()}
      >
        Sign in
      </Button>
    );
  }
  return (
    <PanelCard
      title={name}
      badge={<StatusBadge state={signedIn ? "signed_in" : "signed_out"} />}
      footer={<div className="flex flex-wrap gap-2">{actions}</div>}
    >
      <Facts
        rows={[
          ["Account", signedIn ? forge?.user || "Signed in" : "—"],
          ["Host", <Mono key="host">{forge?.host ?? "—"}</Mono>],
          ["CLI", <Mono key="cli">{id}</Mono>],
        ]}
      />
      {live && watched?.device ? <DeviceBox device={watched.device} /> : null}
      {live && !watched?.device ? (
        <div className="flex items-center gap-2">
          <Spinner className="size-3" />
          <span>Starting the sign-in…</span>
        </div>
      ) : null}
      {!signedIn && !live ? (
        <p className="text-xs text-fg-muted">
          Signs in with a one-time code on {forge?.host ?? name}; no token to paste.
        </p>
      ) : null}
    </PanelCard>
  );
}

// tea signs in with an access token, to any number of Gitea or Forgejo servers.
export function TeaCard({
  forge,
  onSignOut,
}: {
  forge: Forge | undefined;
  onSignOut: (login: ForgeLogin) => void;
}) {
  const logins = forge?.logins ?? [];
  const [server, setServer] = useState(logins.length ? "" : DEFAULT_GITEA);
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const serverId = useId();
  const tokenId = useId();
  const page = tokenPage(server);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const r = await verb<{ name: string }>("forge.token", {
        forge: "tea",
        url: server.trim(),
        token,
      });
      setToken("");
      setServer("");
      setError("");
      notify(`Gitea: signed in to ${r.name}.`);
      void queryClient.invalidateQueries({ queryKey: keys.forges });
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };
  return (
    <PanelCard
      title="Gitea and Forgejo"
      badge={<StatusBadge state={logins.length ? "signed_in" : "signed_out"} />}
    >
      <Facts rows={[["CLI", <Mono key="cli">tea</Mono>]]} />
      {logins.length ? (
        <ul className="m-0 flex list-none flex-col divide-y rounded-md border p-0">
          {logins.map((l) => (
            <li key={l.name} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2">
              <span className="font-mono text-xs font-semibold wrap-anywhere">{l.name}</span>
              <span className="text-fg-muted">{l.user ? `as ${l.user}` : null}</span>
              {l.default && logins.length > 1 ? (
                <span className="text-xs text-fg-muted">default</span>
              ) : null}
              <span className="flex-1" />
              <Button
                size="xs"
                variant="outline"
                aria-label={`Sign out of ${l.name}`}
                onClick={() => onSignOut(l)}
              >
                Sign out
              </Button>
            </li>
          ))}
        </ul>
      ) : null}
      <form
        noValidate
        onSubmit={(e) => void submit(e)}
        className="flex max-w-(--form-max) flex-col gap-4"
        aria-label={logins.length ? "Add a server" : "Sign in to a server"}
      >
        {logins.length ? <h3 className="text-sm font-semibold">Add a server</h3> : null}
        <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
          <FormField id={serverId} label="Server" hint="Gitea, Forgejo or Codeberg, over https.">
            <Input
              {...fieldProps(serverId)}
              type="url"
              inputMode="url"
              autoComplete="url"
              spellCheck={false}
              placeholder={DEFAULT_GITEA}
              value={server}
              onChange={(e) => setServer(e.target.value)}
            />
          </FormField>
          <FormField
            id={tokenId}
            label="Access token"
            hint="Needs read and write on repositories and your user."
          >
            <Input
              {...fieldProps(tokenId)}
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={token}
              onChange={(e) => setToken(e.target.value)}
            />
          </FormField>
        </div>
        <div aria-live="polite">{error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}</div>
        <div className="flex flex-wrap items-center gap-2">
          <BusyButton
            type="submit"
            variant={logins.length ? "outline" : "default"}
            busy={busy}
            busyLabel="Signing in…"
            disabled={!page || !token}
          >
            Sign in
          </BusyButton>
          {page ? (
            <a
              href={page}
              target="_blank"
              rel="noopener noreferrer"
              className={buttonVariants({ variant: "ghost", size: "sm" })}
            >
              <ExternalLink aria-hidden />
              Create a token on {new URL(page).host}
            </a>
          ) : null}
        </div>
      </form>
    </PanelCard>
  );
}
