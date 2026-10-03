import { CSPProvider } from "@base-ui/react/csp-provider";
import { QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, type ReactNode } from "react";
import { queryClient } from "@/api/queries";
import { Toaster } from "@/components/ui/toast";

/** Providers wraps every page's root. disableStyleElements keeps Base UI
 * from injecting style elements, which the panel's CSP would block. */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <StrictMode>
      <CSPProvider disableStyleElements>
        <QueryClientProvider client={queryClient}>
          <Toaster>{children}</Toaster>
        </QueryClientProvider>
      </CSPProvider>
    </StrictMode>
  );
}
