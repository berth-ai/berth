import { createHashHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { createRoot } from "react-dom/client";
import { normalizeHash } from "@/lib/hash";
import { applyTheme, storedTheme } from "@/lib/theme";
import { routeTree } from "../routeTree.gen";
import { Providers } from "./providers";
import "@/styles/theme.css";

applyTheme(storedTheme());
// M3 locations (#agents) become router paths (#/agents): on load, and on a
// same-page hash change such as the "not running" page's Settings link.
// These listeners are added before the router's, so it sees the result.
normalizeHash();
window.addEventListener("popstate", normalizeHash);
window.addEventListener("hashchange", normalizeHash);

const router = createRouter({ routeTree, history: createHashHistory() });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <Providers>
    <RouterProvider router={router} />
  </Providers>,
);
