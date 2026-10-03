import { useQuery } from "@tanstack/react-query";
import { useEffect, useState, type FormEvent } from "react";
import { keys } from "@/api/queries";
import { ApiError, get, post } from "@/api/request";
import type { Session } from "@/api/types";
import { BusyButton } from "@/components/busy-button";
import { FormField, fieldProps } from "@/components/form-field";
import { InlineAlert } from "@/components/inline-alert";
import { PanelCard } from "@/components/panel-card";
import { Input } from "@/components/ui/input";
import { errorText } from "@/lib/errors";
import { replaceLocation } from "@/lib/nav";
import { AuthLayout, LostLogin } from "./auth-layout";
import { lockSeconds } from "./auth-logic";

export function LoginPage() {
  const session = useQuery({ queryKey: keys.session, queryFn: () => get<Session>("/api/session") });
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [errs, setErrs] = useState<{ user?: string; password?: string }>({});
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const [lockLeft, setLockLeft] = useState(0);
  const signedIn = Boolean(session.data?.user);

  useEffect(() => {
    if (signedIn) replaceLocation("/");
  }, [signedIn]);
  useEffect(() => {
    if (lockLeft <= 0) return;
    const t = setTimeout(() => setLockLeft((n) => n - 1), 1000);
    return () => clearTimeout(t);
  }, [lockLeft]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const u = user.trim();
    setErrs({
      user: u ? undefined : "Enter your user name.",
      password: password ? undefined : "Enter your password.",
    });
    if (!u || !password) return;
    setFailure("");
    setBusy(true);
    try {
      await post("/api/login", { user: u, password });
      replaceLocation("/");
      return;
    } catch (err) {
      setPassword("");
      const lock = lockSeconds(err);
      if (lock) setLockLeft(lock);
      else
        setFailure(
          err instanceof ApiError && err.status === 401
            ? "Wrong user name or password."
            : errorText(err),
        );
    }
    setBusy(false);
  };

  if (session.isPending || signedIn) return null;
  let notice = null;
  if (lockLeft > 0)
    notice = <InlineAlert tone="warn">Too many attempts. Try again in {lockLeft} s.</InlineAlert>;
  else if (failure) notice = <InlineAlert tone="bad">{failure}</InlineAlert>;
  return (
    <AuthLayout title="Sign in">
      <PanelCard>
        <form
          id="login-form"
          noValidate
          onSubmit={(e) => void submit(e)}
          className="flex flex-col gap-4"
        >
          <FormField id="user" label="User" error={errs.user}>
            <Input
              {...fieldProps("user", errs.user)}
              name="user"
              autoComplete="username"
              autoFocus
              value={user}
              onChange={(e) => setUser(e.target.value)}
            />
          </FormField>
          <FormField id="password" label="Password" error={errs.password}>
            <Input
              {...fieldProps("password", errs.password)}
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </FormField>
          <div aria-live="polite">{notice}</div>
          <BusyButton type="submit" busy={busy} busyLabel="Signing in…" disabled={lockLeft > 0}>
            Sign in
          </BusyButton>
        </form>
      </PanelCard>
      <LostLogin />
    </AuthLayout>
  );
}
