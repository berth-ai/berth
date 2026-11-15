import { Link } from "@tanstack/react-router";
import { ChevronLeft, ExternalLink } from "lucide-react";
import { useState } from "react";
import { applyConfig } from "@/api/config";
import { keys, queryClient, refreshStatus, useStatus } from "@/api/queries";
import { BusyButton } from "@/components/busy-button";
import { InlineAlert } from "@/components/inline-alert";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { buttonVariants } from "@/components/ui/button";
import { errorText } from "@/lib/errors";
import { notify } from "@/lib/notify";
import { useTitle } from "@/lib/use-title";

/** keepRightClick stops the browser menu over a same-origin frame, so a
 * right click reaches tmux; Shift+right click still opens the browser menu. */
export function keepRightClick(frame: HTMLIFrameElement) {
  frame.contentDocument?.addEventListener("contextmenu", (e) => {
    if (!e.shiftKey) e.preventDefault();
  });
}

// Terminal and IDE: full-height embeds of the proxied frontends.
export function FrontendPage({
  service,
  frontend,
  label,
  path,
  rightClick = false,
}: {
  service: string;
  frontend: Frontend;
  label: string;
  path: string;
  /** rightClick hands right clicks to the frame instead of the browser menu. */
  rightClick?: boolean;
}) {
  useTitle(label);
  const status = useStatus();
  const svc = status.data?.services?.find((s) => s.name === service);
  return (
    <div className="flex h-dvh flex-col max-[900px]:h-[calc(100dvh-var(--topbar-h))]">
      <div className="flex h-(--embed-bar-h) items-center gap-2 border-b bg-surface px-3">
        <Link to="/" className={buttonVariants({ variant: "ghost", size: "sm" })}>
          <ChevronLeft aria-hidden className="size-3.25" strokeWidth={1.6} />
          Panel
        </Link>
        <strong className="min-w-0 truncate">{label}</strong>
        {status.data ? <StatusBadge state={svc ? svc.state : "disabled"} small /> : null}
        <a
          href={path}
          target="_blank"
          rel="noopener"
          className={buttonVariants({ variant: "outline", size: "sm", className: "ml-auto" })}
        >
          <ExternalLink aria-hidden className="size-3.25" strokeWidth={1.6} />
          Open in new tab
        </a>
      </div>
      <div className="flex min-h-0 flex-1 bg-log-bg">
        {status.data && svc ? (
          <iframe
            title={label}
            src={path}
            className="size-full flex-1 border-0"
            onLoad={rightClick ? (e) => keepRightClick(e.currentTarget) : undefined}
          />
        ) : null}
        {status.data && !svc ? <FrontendOff label={label} frontend={frontend} /> : null}
      </div>
    </div>
  );
}

export type Frontend = "terminal" | "ide" | "orca";

/** FrontendOff says a frontend is switched off and turns it on in place,
 * since the user has just asked for it; Settings stays one link away. */
export function FrontendOff({ label, frontend }: { label: string; frontend: Frontend }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const turnOn = async () => {
    setBusy(true);
    setError("");
    try {
      await applyConfig({ frontends: { [frontend]: { enabled: true } } });
      notify(`The ${label} is starting.`);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.config }),
        refreshStatus(),
      ]);
    } catch (err) {
      setError(errorText(err));
    }
    setBusy(false);
  };
  return (
    <div className="flex flex-1 items-center justify-center p-4">
      <PanelCard className="max-w-105">
        <h2>The {label} is off</h2>
        <p className="text-fg-muted">
          Turn it on here or in Settings. Only the {label} starts; nothing else restarts.
        </p>
        {error ? <InlineAlert tone="bad">{error}</InlineAlert> : null}
        <div className="flex flex-wrap gap-2">
          <BusyButton busy={busy} busyLabel="Turning on…" onClick={() => void turnOn()}>
            Turn on the {label}
          </BusyButton>
          <Link to="/settings" className={buttonVariants({ variant: "outline" })}>
            Open Settings
          </Link>
        </div>
      </PanelCard>
    </div>
  );
}
