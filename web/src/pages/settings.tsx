import { ChevronRight } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { conflictConfig } from "@/api/config";
import { keys, queryClient, useConfig, useSession, useStatus } from "@/api/queries";
import { ApiError, verb } from "@/api/request";
import type { ApplyResult, Config, ConfigView, Session, Status } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { Facts, Mono } from "@/components/facts";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { LoadingRows } from "@/components/loading";
import { Page, PageHeader } from "@/components/page";
import { PanelCard } from "@/components/panel-card";
import { SaveBar } from "@/components/save-bar";
import { SettingRow } from "@/components/setting-row";
import {
  Autocomplete,
  AutocompleteContent,
  AutocompleteInput,
  AutocompleteItem,
  AutocompleteList,
} from "@/components/ui/autocomplete";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import {
  NumberField,
  NumberFieldDecrement,
  NumberFieldGroup,
  NumberFieldIncrement,
  NumberFieldInput,
} from "@/components/ui/number-field";
import { diff, rebase, setPath } from "@/lib/draft";
import { errorText, fieldErrors } from "@/lib/errors";
import { describeChanges } from "@/lib/format";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";
import { BackupCard } from "./settings-backup";
import { PATHS, predict } from "./settings-logic";

const ZONES: string[] =
  typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];

const ADVANCED = [
  ["keep", "Versions to keep", "Old agent sets kept besides current and previous.", 0, 50],
  [
    "boot_timeout",
    "Agent boot timeout (s)",
    "Time allowed for installing agents at start.",
    30,
    3600,
  ],
  ["mcp_timeout", "MCP setup timeout (s)", "Time allowed for registering MCP servers.", 10, 1800],
] as const;

type Edit = (path: string, value: unknown) => void;

// Settings: login, frontends, runtime, advanced, host values, backup.
export function SettingsPage() {
  useTitle("Settings");
  const config = useConfig();
  let body = <LoadingRows n={6} />;
  if (config.isError) body = <InlineAlert tone="bad">{errorText(config.error)}</InlineAlert>;
  else if (config.data)
    body = <SettingsForm key={config.data.config.revision} initial={config.data.config} />;
  return (
    <Page narrow>
      <PageHeader title="Settings" description="Changes apply without restarting the container." />
      {body}
    </Page>
  );
}

function reload() {
  void queryClient.invalidateQueries({ queryKey: keys.config });
  void queryClient.invalidateQueries({ queryKey: keys.status });
}

function SettingsForm({ initial }: { initial: Config }) {
  const status = useStatus();
  const session = useSession();
  const [base, setBase] = useState(initial);
  const [draft, setDraft] = useState(() => structuredClone(initial));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [conflict, setConflict] = useState(false);
  const [saving, setSaving] = useState(false);
  const { changed } = diff(base, draft, PATHS);

  const edit: Edit = (path, value) =>
    setDraft((d) => {
      const next = structuredClone(d);
      setPath(next, path, value);
      return next;
    });

  const save = async () => {
    const { patch, changed: paths } = diff(base, draft, PATHS);
    if (!paths.length) return;
    setSaving(true);
    try {
      const r = await verb<ApplyResult>("config.apply", { base_revision: base.revision, patch });
      setErrors({});
      setConflict(false);
      notify(describeChanges(r.changes));
      void queryClient.invalidateQueries({ queryKey: keys.status });
      // The new revision remounts this form with a clean draft.
      queryClient.setQueryData<ConfigView>(keys.config, (v) =>
        v ? { ...v, config: r.config } : v,
      );
    } catch (err) {
      const server = conflictConfig(err);
      if (server) {
        setDraft(rebase(base, draft, server, PATHS));
        setBase(server);
        setErrors({});
        setConflict(true);
      } else {
        const errs = fieldErrors(err);
        setErrors(errs);
        if (!Object.keys(errs).some((p) => (PATHS as readonly string[]).includes(p)))
          notify(errorText(err), "bad");
      }
    }
    setSaving(false);
  };

  return (
    <div className="flex flex-col gap-3">
      <LoginCard key={session.data?.user ?? ""} user={session.data?.user ?? ""} />
      <PanelCard title="Frontends">
        <SettingRow
          title="Terminal"
          text="Browser terminal with tmux."
          checked={draft.frontends.terminal.enabled}
          onChange={(v) => edit("frontends.terminal.enabled", v)}
        />
        <SettingRow
          title="IDE"
          text="VS Code in the browser."
          checked={draft.frontends.ide.enabled}
          onChange={(v) => edit("frontends.ide.enabled", v)}
        />
      </PanelCard>
      <PanelCard title="Runtime">
        <TimeZoneField
          value={draft.runtime.tz}
          error={errors["runtime.tz"]}
          onChange={(v) => edit("runtime.tz", v.trim())}
        />
      </PanelCard>
      <AdvancedCard draft={draft} errors={errors} edit={edit} />
      <HostCard status={status.data} />
      <BackupCard base={base} onReload={reload} />
      <div aria-live="polite">
        {conflict ? (
          <InlineAlert tone="warn">
            Settings changed elsewhere. Your edits are kept on top of the new values; review and
            save again.
          </InlineAlert>
        ) : null}
      </div>
      {changed.length ? (
        <SaveBar
          summary={predict(base, draft)}
          busy={saving}
          onSave={() => void save()}
          onDiscard={() => setDraft(structuredClone(base))}
        />
      ) : null}
    </div>
  );
}

function TimeZoneField({
  value,
  error,
  onChange,
}: {
  value: string;
  error?: string;
  onChange: (value: string) => void;
}) {
  const id = useId();
  return (
    <FormField id={id} label="Time zone" hint="New terminal and IDE sessions use it." error={error}>
      <Autocomplete items={ZONES} value={value} onValueChange={(v: string) => onChange(v)}>
        <AutocompleteInput
          {...fieldProps(id, error)}
          className="font-mono text-xs"
          spellCheck={false}
          autoComplete="off"
        />
        <AutocompleteContent>
          <AutocompleteList>
            {(zone: string) => (
              <AutocompleteItem key={zone} value={zone} className="font-mono text-xs">
                {zone}
              </AutocompleteItem>
            )}
          </AutocompleteList>
        </AutocompleteContent>
      </Autocomplete>
    </FormField>
  );
}

function AdvancedCard({
  draft,
  errors,
  edit,
}: {
  draft: Config;
  errors: Record<string, string>;
  edit: Edit;
}) {
  const [open, setOpen] = useState(false);
  // A field error inside must never be hidden behind the closed section.
  const forced = ADVANCED.some(([key]) => errors[`agents.${key}`]);
  return (
    <PanelCard>
      <Collapsible open={open || forced} onOpenChange={setOpen}>
        <CollapsibleTrigger className="group flex cursor-pointer items-center gap-1 font-medium">
          <ChevronRight
            aria-hidden
            className="size-3.5 transition-transform group-data-panel-open:rotate-90"
          />
          Advanced
        </CollapsibleTrigger>
        <CollapsibleContent className="pt-3">
          <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
            {ADVANCED.map(([key, label, hint, min, max]) => (
              <NumberSetting
                key={key}
                label={label}
                hint={hint}
                min={min}
                max={max}
                value={draft.agents[key]}
                error={errors[`agents.${key}`]}
                onChange={(v) => edit(`agents.${key}`, v)}
              />
            ))}
          </div>
        </CollapsibleContent>
      </Collapsible>
    </PanelCard>
  );
}

function NumberSetting({
  label,
  hint,
  min,
  max,
  value,
  error,
  onChange,
}: {
  label: string;
  hint: string;
  min: number;
  max: number;
  value: number | null;
  error?: string;
  onChange: (value: number | null) => void;
}) {
  const id = useId();
  return (
    <FormField id={id} label={label} hint={hint} error={error}>
      <NumberField
        value={value}
        onValueChange={(v: number | null) => onChange(v)}
        min={min}
        max={max}
        step={1}
      >
        <NumberFieldGroup>
          <NumberFieldDecrement />
          <NumberFieldInput {...fieldProps(id, error)} inputMode="numeric" />
          <NumberFieldIncrement />
        </NumberFieldGroup>
      </NumberField>
    </FormField>
  );
}

function HostCard({ status }: { status: Status | undefined }) {
  const host = status?.host;
  return (
    <PanelCard title="Host settings">
      <InlineAlert tone="info">
        Set in .env on the host, then apply with docker compose up -d.
      </InlineAlert>
      <Facts
        rows={[
          ["Bind address", <Mono key="bind">{host?.bind_address || "—"}</Mono>],
          ["Panel port", <Mono key="port">{host?.panel_port || "—"}</Mono>],
          [
            "User and group IDs",
            <Mono key="ids">{`${host?.puid || "?"}:${host?.pgid || "?"}`}</Mono>,
          ],
          ["Workspaces path", <Mono key="ws">{host?.workspaces_dir || "—"}</Mono>],
          ["Image version", <Mono key="ver">{status?.agents?.build?.version || "—"}</Mono>],
        ]}
      />
    </PanelCard>
  );
}

function LoginCard({ user: initialUser }: { user: string }) {
  const [current, setCurrent] = useState("");
  const [user, setUser] = useState(initialUser);
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const [errs, setErrs] = useState<{
    current?: string;
    user?: string;
    password?: string;
    again?: string;
  }>({});
  const [busy, setBusy] = useState(false);
  const ids = { current: useId(), user: useId(), password: useId(), again: useId() };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (password !== again) {
      setErrs({ again: "The passwords differ." });
      return;
    }
    setBusy(true);
    try {
      const r = await verb<{ user: string }>("auth.change", {
        current_password: current,
        user: user.trim(),
        password,
      });
      setErrs({});
      setCurrent("");
      setPassword("");
      setAgain("");
      notify("Login changed; other sessions were signed out.");
      queryClient.setQueryData<Session>(keys.session, (s) => (s ? { ...s, user: r.user } : s));
    } catch (err) {
      const f = fieldErrors(err);
      const next = {
        current:
          err instanceof ApiError && err.code === "wrong_password"
            ? "The current password is wrong."
            : undefined,
        user: f.user,
        password: f.password,
      };
      setErrs(next);
      if (!next.current && !next.user && !next.password) notify(errorText(err), "bad");
    }
    setBusy(false);
  };

  return (
    <PanelCard title="Panel login">
      <form
        noValidate
        onSubmit={(e) => void submit(e)}
        className="flex max-w-(--form-max) flex-col gap-4"
      >
        <FormField id={ids.current} label="Current password" error={errs.current}>
          <Input
            {...fieldProps(ids.current, errs.current)}
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </FormField>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
          <FormField id={ids.user} label="User" error={errs.user}>
            <Input
              {...fieldProps(ids.user, errs.user)}
              autoComplete="username"
              value={user}
              onChange={(e) => setUser(e.target.value)}
            />
          </FormField>
        </div>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
          <FormField
            id={ids.password}
            label="New password"
            hint="At least 10 characters."
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
          <FormField id={ids.again} label="Repeat new password" error={errs.again}>
            <Input
              {...fieldProps(ids.again, errs.again)}
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
            />
          </FormField>
        </div>
        <div>
          <BusyButton type="submit" variant="outline" busy={busy} busyLabel="Changing…">
            Change login
          </BusyButton>
        </div>
      </form>
    </PanelCard>
  );
}
