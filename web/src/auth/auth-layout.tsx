import type { ReactNode } from "react";
import { LogoMark } from "@/components/logo";
import { cn } from "@/lib/utils";

/** AuthLayout is the centred column of the sign-in and setup screens. */
export function AuthLayout({
  wide,
  title,
  children,
}: {
  wide?: boolean;
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="flex min-h-dvh justify-center px-4 py-[clamp(20px,6vh,56px)]">
      <div className={cn("flex w-full flex-col gap-4", wide ? "max-w-140" : "max-w-105")}>
        <div className="flex items-center justify-center gap-2 text-lg font-semibold">
          <LogoMark large />
          Berth
        </div>
        <h1 className="text-2xl">{title}</h1>
        {children}
      </div>
    </div>
  );
}

export function LostLogin() {
  return (
    <p className="text-xs text-fg-muted">
      Lost the login? Run <code>docker compose exec berth berth config reset-login</code> and open
      the new setup link.
    </p>
  );
}
