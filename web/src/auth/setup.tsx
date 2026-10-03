import { useQuery } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { keys } from "@/api/queries";
import { ApiError, get, post, verb } from "@/api/request";
import type { Session, Status } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { Facts } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { PanelCard } from "@/components/panel-card";
import { SettingRow } from "@/components/setting-row";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Stepper,
  StepperIndicator,
  StepperItem,
  StepperNav,
  StepperSeparator,
  StepperTitle,
} from "@/components/ui/stepper";
import { errorText, fieldErrors } from "@/lib/errors";
import { replaceLocation } from "@/lib/nav";
import { AuthLayout } from "./auth-layout";
import { strength, USER } from "./auth-logic";

const STEPS = ["Login", "Frontends", "Extras", "Done"];

interface WizardData {
  user: string;
  password: string;
  terminal: boolean;
  ide: boolean;
  keys: number;
  git: string;
}

// The first-run setup wizard. The token was taken from the URL fragment
// before this rendered (setup-main.tsx) and only ever travels in a POST body.
export function SetupPage({ token }: { token: string | null }) {
  const session = useQuery({ queryKey: keys.session, queryFn: () => get<Session>("/api/session") });
  const [step, setStep] = useState(0);
  const [data, setData] = useState<WizardData>({
    user: "",
    password: "",
    terminal: true,
    ide: false,
    keys: 0,
    git: "",
  });
  const [linkError, setLinkError] = useState<string | null>(null);
  const [loginError, setLoginError] = useState("");
  const update = (patch: Partial<WizardData>) => setData((d) => ({ ...d, ...patch }));

  if (session.isPending) return null;
  if (session.data?.mode && session.data.mode !== "setup") {
    return (
      <LinkError
        message="Berth is already set up."
        action={
          <a href="/login" className={buttonVariants()}>
            Sign in
          </a>
        }
      />
    );
  }
  if (!token) return <LinkError message="This page needs the setup link from the container log." />;
  if (linkError) return <LinkError message={linkError} />;

  return (
    <AuthLayout wide title="Set up Berth">
      <Stepper value={step + 1}>
        <StepperNav>
          {STEPS.map((label, i) => (
            <StepperItem
              key={label}
              step={i + 1}
              completed={i < step}
              aria-current={i === step ? "step" : undefined}
            >
              <StepperIndicator>
                {i < step ? <Check aria-hidden className="size-3" strokeWidth={2} /> : i + 1}
              </StepperIndicator>
              <StepperTitle>{label}</StepperTitle>
              {i < STEPS.length - 1 ? <StepperSeparator /> : null}
            </StepperItem>
          ))}
        </StepperNav>
      </Stepper>
      {step === 0 ? (
        <StepLogin
          data={data}
          error={loginError}
          onNext={(user, password) => {
            update({ user, password });
            setLoginError("");
            setStep(1);
          }}
        />
      ) : null}
      {step === 1 ? (
        <StepFrontends
          token={token}
          data={data}
          update={update}
          onBack={() => setStep(0)}
          onCreated={() => {
            update({ password: "" });
            setStep(2);
          }}
          onLinkError={setLinkError}
          onInvalidLogin={(message) => {
            setLoginError(message);
            setStep(0);
          }}
        />
      ) : null}
      {step === 2 ? (
        <StepExtras
          onSkip={() => setStep(3)}
          onDone={(saved, git) => {
            update({ keys: saved, git });
            setStep(3);
          }}
        />
      ) : null}
      {step === 3 ? <StepDone data={data} /> : null}
    </AuthLayout>
  );
}

function WizardCard({
  title,
  step,
  onSubmit,
  footer,
  children,
}: {
  title: string;
  step: number;
  onSubmit: () => void;
  footer: ReactNode;
  children: ReactNode;
}) {
  return (
    <form
      id="setup-form"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      <Card className="gap-0 py-0">
        <CardHeader className="border-b px-4 py-3">
          <div className="flex items-baseline gap-2">
            <h2 className="text-lg">{title}</h2>
            <span className="text-fg-muted">
              Step {step + 1} of {STEPS.length}
            </span>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 p-4">{children}</CardContent>
        <CardFooter className="flex flex-wrap gap-2 border-t px-4 py-3">{footer}</CardFooter>
      </Card>
    </form>
  );
}

function StepLogin({
  data,
  error,
  onNext,
}: {
  data: WizardData;
  error: string;
  onNext: (user: string, password: string) => void;
}) {
  const [user, setUser] = useState(data.user);
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [errs, setErrs] = useState<{ user?: string; password?: string; again?: string }>({});
  const ids = { user: useId(), password: useId(), again: useId() };
  const submit = () => {
    const u = user.trim();
    const next = {
      user: USER.test(u) ? undefined : "Use 1 to 64 letters, digits or . _ @ -",
      password: password.length >= 10 ? undefined : "Use at least 10 characters.",
      again: again === password ? undefined : "The passwords differ.",
    };
    setErrs(next);
    if (next.user || next.password || next.again) return;
    onNext(u, password);
  };
  return (
    <WizardCard
      title="Create the panel login"
      step={0}
      onSubmit={submit}
      footer={
        <>
          <span className="flex-1" />
          <Button type="submit">Next</Button>
        </>
      }
    >
      {error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}
      <FormField id={ids.user} label="User" hint="Letters, digits and . _ @ -" error={errs.user}>
        <Input
          {...fieldProps(ids.user, errs.user)}
          autoComplete="username"
          autoFocus
          value={user}
          onChange={(e) => setUser(e.target.value)}
        />
      </FormField>
      <FormField
        id={ids.password}
        label="Password"
        hint={password ? strength(password) : "At least 10 characters."}
        error={errs.password}
      >
        <Input
          {...fieldProps(ids.password, errs.password)}
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </FormField>
      <FormField id={ids.again} label="Repeat password" error={errs.again}>
        <Input
          {...fieldProps(ids.again, errs.again)}
          type="password"
          autoComplete="new-password"
          value={again}
          onChange={(e) => setAgain(e.target.value)}
        />
      </FormField>
    </WizardCard>
  );
}

function StepFrontends({
  token,
  data,
  update,
  onBack,
  onCreated,
  onLinkError,
  onInvalidLogin,
}: {
  token: string;
  data: WizardData;
  update: (patch: Partial<WizardData>) => void;
  onBack: () => void;
  onCreated: () => void;
  onLinkError: (message: string) => void;
  onInvalidLogin: (message: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submit = async () => {
    setBusy(true);
    try {
      await post("/api/setup", {
        token,
        user: data.user,
        password: data.password,
        patch: { frontends: { terminal: { enabled: data.terminal }, ide: { enabled: data.ide } } },
      });
      onCreated();
      return;
    } catch (err) {
      const code = err instanceof ApiError ? err.code : "";
      if (code === "invalid_token" || code === "not_in_setup")
        onLinkError("This setup link is not valid or was already used.");
      else if (code === "invalid_login") onInvalidLogin(errorText(err));
      else setError(errorText(err));
    }
    setBusy(false);
  };
  return (
    <WizardCard
      title="Choose the frontends"
      step={1}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button variant="outline" onClick={onBack}>
            Back
          </Button>
          <span className="flex-1" />
          <BusyButton type="submit" busy={busy} busyLabel="Creating…">
            Create login
          </BusyButton>
        </>
      }
    >
      <SettingRow
        title="Terminal"
        text="A browser terminal with tmux; sessions survive closing the tab."
        checked={data.terminal}
        onChange={(v) => update({ terminal: v })}
      />
      <SettingRow
        title="IDE"
        text="VS Code in the browser (code-server)."
        checked={data.ide}
        onChange={(v) => update({ ide: v })}
      />
      <p className="text-xs text-fg-muted">You can change both later in Settings.</p>
      <div aria-live="polite">{error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}</div>
    </WizardCard>
  );
}

function StepExtras({
  onSkip,
  onDone,
}: {
  onSkip: () => void;
  onDone: (saved: number, git: string) => void;
}) {
  const status = useQuery({ queryKey: keys.status, queryFn: () => get<Status>("/api/status") });
  const [secrets, setSecrets] = useState<Record<string, string>>({});
  const [keyErrors, setKeyErrors] = useState<Record<string, string>>({});
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const keyId = useId();
  const nameId = useId();
  const emailId = useId();

  if (status.isPending) {
    return (
      <WizardCard title="Optional extras" step={2} onSubmit={() => undefined} footer={null}>
        <p className="text-fg-muted">Loading…</p>
      </WizardCard>
    );
  }
  const owners = (status.data?.packages ?? []).flatMap((p) =>
    (p.keys ?? []).map((k): [string, string] => [k.name, p.name]),
  );

  const submit = async () => {
    setError("");
    setBusy(true);
    const filled = Object.fromEntries(Object.entries(secrets).filter(([, v]) => v));
    let ok = true;
    let saved = 0;
    let git = "";
    if (Object.keys(filled).length) {
      try {
        await verb("config.apply", { secrets: filled });
        setKeyErrors({});
        saved = Object.keys(filled).length;
      } catch (err) {
        ok = false;
        const f = fieldErrors(err);
        setKeyErrors(Object.fromEntries(owners.map(([n]) => [n, f[`secrets.${n}`] ?? ""])));
        if (!Object.keys(f).length) setError(errorText(err));
      }
    }
    if (ok && (name.trim() || email.trim())) {
      try {
        await verb("git.identity", { name: name.trim(), email: email.trim() });
        git = `${name.trim()} <${email.trim()}>`;
      } catch (err) {
        ok = false;
        setError(errorText(err));
      }
    }
    setBusy(false);
    if (ok) onDone(saved, git);
  };

  return (
    <WizardCard
      title="Optional extras"
      step={2}
      onSubmit={() => void submit()}
      footer={
        <>
          <span className="flex-1" />
          <Button variant="outline" onClick={onSkip}>
            Skip
          </Button>
          <BusyButton type="submit" busy={busy} busyLabel="Saving…">
            Save and continue
          </BusyButton>
        </>
      }
    >
      <h3 className="text-md font-semibold">API keys</h3>
      {owners.length ? (
        owners.map(([n, owner]) => (
          <FormField
            key={n}
            id={`${keyId}-${n}`}
            label={n}
            hint={`Used by ${owner}.`}
            error={keyErrors[n]}
          >
            <Input
              {...fieldProps(`${keyId}-${n}`, keyErrors[n])}
              type="password"
              autoComplete="off"
              spellCheck={false}
              className="font-mono text-xs"
              value={secrets[n] ?? ""}
              onChange={(e) => setSecrets((s) => ({ ...s, [n]: e.target.value }))}
            />
          </FormField>
        ))
      ) : (
        <p className="text-fg-muted">No installed agent needs a key yet.</p>
      )}
      <h3 className="text-md font-semibold">Git identity</h3>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
        <FormField id={nameId} label="Git name">
          <Input
            {...fieldProps(nameId)}
            autoComplete="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </FormField>
        <FormField id={emailId} label="Git email">
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
    </WizardCard>
  );
}

function StepDone({ data }: { data: WizardData }) {
  return (
    <WizardCard
      title="Berth is ready"
      step={3}
      onSubmit={() => replaceLocation("/")}
      footer={
        <>
          <span className="flex-1" />
          <Button type="submit">Open Berth</Button>
        </>
      }
    >
      <Facts
        rows={[
          ["Panel login", data.user],
          ["Terminal", data.terminal ? "On" : "Off"],
          ["IDE", data.ide ? "On" : "Off"],
          ["API keys", data.keys ? `${data.keys} saved` : "None yet"],
          ["Git identity", data.git || "Not set"],
        ]}
      />
      <p className="text-fg-muted">
        The first start installs the agent CLIs, which can take a few minutes. The overview shows
        when they are ready.
      </p>
    </WizardCard>
  );
}

function LinkError({ message, action }: { message: string; action?: ReactNode }) {
  return (
    <AuthLayout wide title="Set up Berth">
      <PanelCard footer={action}>
        <InlineAlert tone="bad">{message}</InlineAlert>
        <p>
          Find a fresh link with <code>docker compose logs berth | grep setup</code>. Restarting the
          container in setup mode prints a new one.
        </p>
      </PanelCard>
    </AuthLayout>
  );
}
