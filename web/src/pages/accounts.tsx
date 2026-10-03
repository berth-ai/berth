import { Copy, ExternalLink } from "lucide-react";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { keys, queryClient, useForges, useJob, useJobs } from "@/api/queries";
import { verb } from "@/api/request";
import { jobStartError } from "@/api/run-job";
import type { Device, Forge, ForgeStatus, Job } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { CardGrid, Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { copyText } from "@/lib/copy";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { deviceHost, forgeJobId } from "./accounts-logic";

const FORGES = [
  ["gh", "GitHub CLI"],
  ["glab", "GitLab CLI"],
] as const;

// Accounts: git identity and the GitHub and GitLab CLIs (device sign-in).
export function AccountsPage() {
  useTitle("Accounts");
  const forges = useForges();
  let body = <LoadingRows n={2} />;
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
  return (
    <>
      <GitCard initial={data.git} />
      <CardGrid>
        {FORGES.map(([id, label]) => (
          <ForgeCard key={id} id={id} label={label} forge={data.forges[id]} />
        ))}
      </CardGrid>
    </>
  );
}

function GitCard({ initial }: { initial: { name: string; email: string } }) {
  const [name, setName] = useState(initial.name);
  const [email, setEmail] = useState(initial.email);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const nameId = useId();
  const emailId = useId();
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await verb("git.identity", { name: name.trim(), email: email.trim() });
      setError("");
      notify("Git identity saved.");
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };
  return (
    <PanelCard title="Git identity">
      <form
        noValidate
        onSubmit={(e) => void submit(e)}
        className="flex max-w-(--form-max) flex-col gap-4"
      >
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
          <BusyButton type="submit" busy={busy} busyLabel="Saving…">
            Save
          </BusyButton>
        </div>
      </form>
    </PanelCard>
  );
}

function ForgeCard({ id, label, forge }: { id: string; label: string; forge: Forge | undefined }) {
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
        ok ? `${label}: signed in.` : `${label}: sign-in did not complete; see Activity.`,
        ok ? "ok" : "bad",
      );
      void queryClient.invalidateQueries({ queryKey: keys.forges });
    }
    last.current = watched?.state;
  }, [watched, label]);

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

  const signedIn = Boolean(forge?.signed_in);
  const facts = signedIn
    ? `${forge?.user || "Signed in"} on ${forge?.host}`
    : `Not signed in to ${forge ? forge.host : ""}.`;
  return (
    <PanelCard
      title={label}
      badge={<StatusBadge state={signedIn ? "signed_in" : "signed_out"} />}
      footer={
        <Button
          size="sm"
          variant={signedIn ? "outline" : "default"}
          disabled={starting || live}
          onClick={() => void start()}
        >
          {signedIn ? "Sign in again" : "Sign in"}
        </Button>
      }
    >
      <p className="text-fg-muted">{facts}</p>
      {live && watched?.device ? <DeviceBox device={watched.device} /> : null}
      {live && !watched?.device ? (
        <div className="flex items-center gap-2">
          <Spinner className="size-3" />
          <span>Starting the sign-in…</span>
        </div>
      ) : null}
    </PanelCard>
  );
}

function DeviceBox({ device }: { device: Device }) {
  return (
    <div className="flex flex-col gap-2 rounded-md bg-info-soft p-4">
      <span className="text-fg-muted">Enter this code on the code host:</span>
      <span className="font-mono text-2xl font-semibold tracking-[.08em] wrap-anywhere text-fg">
        {device.code}
      </span>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            void copyText(device.code, "Code copied.", "Copy failed; type the code instead.")
          }
        >
          <Copy aria-hidden />
          Copy code
        </Button>
        <a
          href={device.url}
          target="_blank"
          rel="noopener noreferrer"
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          <ExternalLink aria-hidden />
          Open {deviceHost(device.url)}
        </a>
      </div>
      <div className="flex items-center gap-2">
        <Spinner className="size-3" />
        <StatusBadge state="waiting" detail="for approval" />
      </div>
    </div>
  );
}
