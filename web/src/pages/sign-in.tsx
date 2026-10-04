import { ExternalLink } from "lucide-react";
import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { keys, queryClient, refreshLogins, useJob, useJobs } from "@/api/queries";
import { verb } from "@/api/request";
import { jobStartError } from "@/api/run-job";
import type { AgentPackage, Job, LoginMethodStatus, LoginStatus } from "@/api/types";
import { BrandIcon } from "@/components/brand-icon";
import { BusyButton } from "@/components/busy-button";
import { DeviceBox } from "@/components/device-box";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { StatusBadge } from "@/components/status-badge";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { cn } from "@/lib/utils";
import { deviceHost } from "./accounts-logic";
import { cleanCode, dialogStep, methodLines, runningLogin } from "./login-logic";

type AgentLogins = { name: string; methods: LoginMethodStatus[] | null };

/** SignInCell lists an agent's sign-in methods, each with its actions. */
export function SignInCell({
  pkg,
  status,
  loading,
  failed,
  onSignIn,
  onSignOut,
}: {
  pkg: AgentPackage;
  status: AgentLogins | undefined;
  loading: boolean;
  failed: boolean;
  onSignIn: (m: LoginMethodStatus) => void;
  onSignOut: (m: LoginMethodStatus) => void;
}) {
  if (!pkg.enabled) return <span className="text-xs text-fg-muted">—</span>;
  if (loading) return <Skeleton className="h-3.5 w-24" />;
  if (failed) return <span className="text-xs text-fg-muted">Unavailable</span>;
  const methods = status?.methods ?? [];
  if (!methods.length) return <span className="text-xs text-fg-muted">—</span>;
  return (
    <div className="flex flex-col gap-1.5">
      {methodLines(methods).map((l, i) => {
        const m = methods[i];
        return (
          <div key={l.id} className="flex flex-wrap items-center gap-2">
            {l.label ? <span className="text-xs">{l.label}</span> : null}
            <StatusBadge
              small
              state={l.state === "unknown" ? "probe_failed" : l.state}
              detail={l.account || null}
            />
            {l.state === "signed_in" ? (
              <>
                <Button
                  variant="ghost"
                  size="xs"
                  aria-label={`Sign in ${pkg.name} with ${m.label} again`}
                  onClick={() => onSignIn(m)}
                >
                  Sign in again
                </Button>
                <Button
                  variant="outline"
                  size="xs"
                  aria-label={`Sign ${pkg.name} out of ${m.label}`}
                  onClick={() => onSignOut(m)}
                >
                  Sign out
                </Button>
              </>
            ) : (
              <Button
                variant="outline"
                size="xs"
                aria-label={`Sign in ${pkg.name} with ${m.label}`}
                onClick={() => onSignIn(m)}
              >
                Sign in
              </Button>
            )}
            {l.error ? <span className="w-full text-xs text-fg-muted">{l.error}</span> : null}
          </div>
        );
      })}
    </div>
  );
}

export interface SignInTarget {
  agent: string;
  agentName: string;
  icon?: string;
  method: string;
  label: string;
}

/** useSignIn owns the sign-in dialog: start opens one, and a sign-in
 * already running (another tab, a reload) reopens it. */
export function useSignIn(
  logins: LoginStatus | undefined,
  pkgs: AgentPackage[] | undefined,
): { dialog: ReactNode; start: (t: SignInTarget) => void } {
  const jobs = useJobs();
  const [open, setOpen] = useState<{ target: SignInTarget; jobId: number | null } | null>(null);
  const [dismissed, setDismissed] = useState<number | null>(null);
  // While a start is in flight the jobs list can show its job before the
  // broker's answer does; reopening then would undo a Cancel.
  const [starting, setStarting] = useState(0);
  // Bumped by every start and close: a start whose dialog was closed before
  // the broker answered cancels its own job instead of reopening.
  const attempt = useRef(0);
  const running = runningLogin(jobs.data?.jobs);
  if (!open && !starting && running && running.id !== dismissed) {
    const a = logins?.agents?.[running.agent];
    const m = a?.methods?.find((x) => x.id === running.method);
    if (a && m) {
      const icon = pkgs?.find((p) => p.package === running.agent)?.icon;
      setOpen({
        target: { agent: running.agent, agentName: a.name, icon, method: m.id, label: m.label },
        jobId: running.id,
      });
    }
  }
  const start = async (target: SignInTarget) => {
    const mine = ++attempt.current;
    setOpen({ target, jobId: null });
    setStarting((n) => n + 1);
    try {
      const r = await verb<{ job: Job }>("login.start", {
        agent: target.agent,
        method: target.method,
      });
      if (attempt.current !== mine) {
        setDismissed(r.job.id);
        void verb("login.cancel", { job: r.job.id }).catch(() => undefined);
        return;
      }
      setOpen({ target, jobId: r.job.id });
      void queryClient.invalidateQueries({ queryKey: keys.jobs });
    } catch (err) {
      if (attempt.current === mine) {
        setOpen(null);
        notify(jobStartError(err), "bad");
      }
    } finally {
      setStarting((n) => n - 1);
    }
  };
  const dialog = open ? (
    <SignInDialog
      key={open.jobId ?? "starting"}
      target={open.target}
      jobId={open.jobId}
      onRestart={() => void start(open.target)}
      onClose={() => {
        attempt.current++;
        setDismissed(open.jobId);
        setOpen(null);
      }}
    />
  ) : null;
  return { dialog, start: (t) => void start(t) };
}

function SignInDialog({
  target,
  jobId,
  onRestart,
  onClose,
}: {
  target: SignInTarget;
  jobId: number | null;
  onRestart: () => void;
  onClose: () => void;
}) {
  const job = useJob(jobId).data?.job;
  const step = dialogStep(jobId === null ? undefined : job);
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const codeId = useId();
  const last = useRef<string | undefined>(undefined);

  // Acts only when the step's kind changes, so a new onClose identity on
  // each render is harmless.
  useEffect(() => {
    if (step.kind === last.current) return;
    if (step.kind === "done") {
      notify(`${target.agentName}: signed in${step.account ? ` as ${step.account}` : ""}.`, "ok");
      void refreshLogins();
      onClose();
    }
    if (step.kind === "failed") void refreshLogins();
    last.current = step.kind;
  }, [step, target.agentName, onClose]);

  // A new prompt means the CLI is waiting again: the code can be retyped.
  const waiting = step.kind === "paste" && step.waiting;
  const [wasWaiting, setWasWaiting] = useState(waiting);
  if (waiting !== wasWaiting) {
    setWasWaiting(waiting);
    if (waiting) setSent(false);
  }

  const close = () => {
    if (jobId !== null && job?.state === "running") {
      void verb("login.cancel", { job: jobId }).catch(() => undefined);
    }
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const clean = cleanCode(code);
    if (!clean) {
      setError("Paste the code the sign-in page shows, on one line.");
      return;
    }
    setBusy(true);
    try {
      await verb("login.input", { job: jobId, code: clean });
      setError(undefined);
      setCode("");
      setSent(true);
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };

  return (
    <Dialog open onOpenChange={(o) => (o ? undefined : close())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <BrandIcon slug={target.icon} name={target.agentName} id={target.agent} size={20} />
            Sign in to {target.agentName} · {target.label}
          </DialogTitle>
        </DialogHeader>

        {step.kind === "starting" ? (
          <div className="flex items-center gap-2">
            <Spinner className="size-3" />
            <span>Starting the sign-in…</span>
          </div>
        ) : null}

        {step.kind === "device" ? (
          <DeviceBox
            device={{ code: step.code, url: step.url }}
            hint="Enter this code on the sign-in page:"
          />
        ) : null}

        {step.kind === "paste" ? (
          <form className="flex flex-col gap-3" onSubmit={(e) => void submit(e)}>
            <p className="text-fg-secondary">
              {step.retry
                ? "That code didn't work. Open the sign-in page again; it has changed."
                : "Sign in, then copy the code the page shows."}
            </p>
            <a
              href={step.url}
              target="_blank"
              rel="noopener noreferrer"
              className={cn(buttonVariants({ variant: "outline", size: "sm" }), "w-fit")}
            >
              <ExternalLink aria-hidden />
              Open {deviceHost(step.url)}
            </a>
            <FormField id={codeId} label="Code" error={error}>
              <Input
                {...fieldProps(codeId, error)}
                value={code}
                autoComplete="off"
                spellCheck={false}
                disabled={!step.waiting}
                onChange={(e) => setCode(e.target.value)}
              />
            </FormField>
            {!step.waiting ? (
              <div className="flex items-center gap-2">
                <Spinner className="size-3" />
                <span>{sent ? "Checking the code…" : "Waiting for the sign-in page…"}</span>
              </div>
            ) : null}
            <BusyButton
              type="submit"
              className="w-fit"
              busy={busy}
              busyLabel="Sending"
              disabled={!step.waiting}
            >
              Submit code
            </BusyButton>
          </form>
        ) : null}

        {step.kind === "failed" && !step.cancelled ? (
          <InlineAlert tone="bad">
            {step.badCode ? "That code didn't work." : step.message}
          </InlineAlert>
        ) : null}

        <DialogFooter>
          {step.kind === "failed" ? (
            <>
              <Button variant="outline" onClick={onClose}>
                Close
              </Button>
              <Button onClick={onRestart}>Start again</Button>
            </>
          ) : (
            <Button variant="outline" onClick={close}>
              Cancel
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
