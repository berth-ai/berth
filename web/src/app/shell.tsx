import { Outlet, useLocation } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useSession, useStatus } from "@/api/queries";
import { LoadingRows } from "@/components/loading";
import { Page } from "@/components/page";
import { RecoveryPage } from "@/pages/recovery";
import { Banners } from "./banners";
import { Nav, TopBar } from "./nav";

/** Shell is the panel frame: navigation, banners and the current page. */
export function Shell() {
  const status = useStatus();
  const session = useSession();
  const { pathname } = useLocation();
  const [menuOpen, setMenuOpen] = useState(false);
  const main = useRef<HTMLElement>(null);
  useEffect(() => {
    main.current?.focus({ preventScroll: true });
  }, [pathname]);
  const recovery = status.data?.mode === "recovery";
  let content = <Outlet />;
  if (recovery) content = <RecoveryPage status={status.data} />;
  else if (status.isPending) {
    content = (
      <Page>
        <LoadingRows n={6} />
      </Page>
    );
  }
  return (
    <div className="grid min-h-dvh grid-cols-[var(--nav-w)_minmax(0,1fr)] max-[900px]:grid-cols-[minmax(0,1fr)]">
      <TopBar open={menuOpen} onToggle={() => setMenuOpen(!menuOpen)} />
      <Nav
        open={menuOpen}
        onNavigate={() => setMenuOpen(false)}
        status={status.data}
        user={session.data?.user ?? ""}
      />
      <div className="flex min-w-0 flex-col">
        <Banners status={status.data} pathname={pathname} />
        <main ref={main} tabIndex={-1} className="outline-none">
          {content}
        </main>
      </div>
    </div>
  );
}
