import { Link } from "@tanstack/react-router";
import { ChevronLeft, ExternalLink } from "lucide-react";
import { useStatus } from "@/api/queries";
import { PanelCard } from "@/components/panel-card";
import { StatusBadge } from "@/components/status-badge";
import { buttonVariants } from "@/components/ui/button";
import { useTitle } from "@/lib/use-title";

// Terminal and IDE: full-height embeds of the proxied frontends.
export function FrontendPage({
  service,
  label,
  path,
}: {
  service: string;
  label: string;
  path: string;
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
          <iframe title={label} src={path} className="size-full flex-1 border-0" />
        ) : null}
        {status.data && !svc ? (
          <div className="flex flex-1 items-center justify-center p-4">
            <PanelCard className="max-w-105">
              <h2>The {label} is off</h2>
              <p className="text-fg-muted">
                Turn it on in Settings. Only the {label} starts; nothing else restarts.
              </p>
              <div>
                <Link to="/settings" className={buttonVariants()}>
                  Open Settings
                </Link>
              </div>
            </PanelCard>
          </div>
        ) : null}
      </div>
    </div>
  );
}
