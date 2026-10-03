import { Link } from "@tanstack/react-router";
import {
  Activity,
  Bot,
  CircleUser,
  CircleX,
  Code,
  LayoutGrid,
  Menu,
  SlidersHorizontal,
  SquareTerminal,
  Wrench,
} from "lucide-react";
import { post } from "@/api/request";
import type { Status } from "@/api/types";
import { LogoMark } from "@/components/logo";
import { Button } from "@/components/ui/button";
import { replaceLocation } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { failedCount } from "./banners";
import { JobIndicator } from "./job-indicator";
import { ThemePicker } from "./theme-picker";

const NAV = [
  { to: "/", label: "Overview", icon: LayoutGrid },
  { to: "/terminal", label: "Terminal", icon: SquareTerminal },
  { to: "/ide", label: "IDE", icon: Code },
  { to: "/agents", label: "Agents", icon: Bot },
  { to: "/tools", label: "Tools", icon: Wrench },
  { to: "/accounts", label: "Accounts", icon: CircleUser },
  { to: "/settings", label: "Settings", icon: SlidersHorizontal },
  { to: "/activity", label: "Activity", icon: Activity },
] as const;

function Logo({ version, className }: { version?: string; className?: string }) {
  return (
    <Link
      to="/"
      aria-label="Berth overview"
      className={cn(
        "flex items-center gap-2 px-1.5 font-semibold text-fg no-underline hover:text-fg",
        className,
      )}
    >
      <LogoMark />
      Berth
      {version ? (
        <span className="ml-auto font-mono text-2xs font-medium text-fg-muted">{version}</span>
      ) : null}
    </Link>
  );
}

async function signOut() {
  await post("/api/logout").catch(() => undefined);
  replaceLocation("/login");
}

/** TopBar is the mobile header below 900 px (DESIGN.md §5). */
export function TopBar({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <header className="sticky top-0 z-20 hidden h-(--topbar-h) items-center gap-2 border-b bg-surface px-4 max-[900px]:flex">
      <Button
        variant="ghost"
        size="icon"
        aria-label="Menu"
        aria-expanded={open}
        aria-controls="nav"
        onClick={onToggle}
        className="size-9 text-fg"
      >
        <Menu aria-hidden className="size-4.5" strokeWidth={1.6} />
      </Button>
      <Logo />
    </header>
  );
}

export function Nav({
  open,
  onNavigate,
  status,
  user,
}: {
  open: boolean;
  onNavigate: () => void;
  status: Status | undefined;
  user: string;
}) {
  const failed = failedCount(status);
  return (
    <nav
      id="nav"
      aria-label="Main"
      className={cn(
        "sticky top-0 flex h-dvh flex-col gap-0.5 overflow-y-auto border-r bg-surface px-2.5 py-4",
        "max-[900px]:fixed max-[900px]:inset-x-0 max-[900px]:top-(--topbar-h) max-[900px]:z-20 max-[900px]:h-auto max-[900px]:max-h-[calc(100dvh-var(--topbar-h))] max-[900px]:border-r-0 max-[900px]:border-b",
        !open && "max-[900px]:hidden",
      )}
    >
      <Logo version={status?.agents?.build?.version} className="pb-3.5 max-[900px]:hidden" />
      {NAV.map((item) => (
        <Link
          key={item.to}
          to={item.to}
          activeOptions={{ exact: item.to === "/" }}
          activeProps={{ "aria-current": "page" }}
          onClick={onNavigate}
          className="flex h-(--nav-item-h) items-center gap-2.5 rounded-md px-2.5 font-medium text-fg-muted no-underline hover:bg-surface-sunken hover:text-fg aria-[current=page]:bg-accent-soft aria-[current=page]:font-semibold aria-[current=page]:text-accent-text max-[900px]:h-10"
        >
          <item.icon aria-hidden className="size-4" strokeWidth={1.5} />
          <span>{item.label}</span>
          {item.to === "/" && failed ? (
            <span className="ml-auto inline-flex items-center gap-0.75 text-2xs font-semibold text-bad">
              <CircleX aria-hidden className="size-2.75" strokeWidth={2} />
              {failed}
            </span>
          ) : null}
        </Link>
      ))}
      <div className="mt-auto flex flex-col gap-3 pt-3 max-[900px]:order-first max-[900px]:mt-0 max-[900px]:pt-0 max-[900px]:pb-3">
        <JobIndicator />
        <ThemePicker />
        <div className="flex items-center gap-2 px-1.5 text-fg-secondary">
          <CircleUser aria-hidden className="size-3.5" />
          <span className="truncate">{user}</span>
          <span className="flex-1" />
          <Button variant="ghost" size="sm" onClick={() => void signOut()}>
            Sign out
          </Button>
        </div>
      </div>
    </nav>
  );
}
