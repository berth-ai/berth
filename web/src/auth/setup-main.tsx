import { createRoot } from "react-dom/client";
import { Providers } from "@/app/providers";
import { reloadOnSetupToken, takeSetupToken } from "@/lib/setup-token";
import { applyTheme, storedTheme } from "@/lib/theme";
import { SetupPage } from "./setup";
import "@/styles/theme.css";

// First, before any request: read the token and drop it from the address bar.
const token = takeSetupToken();
reloadOnSetupToken();
applyTheme(storedTheme());

createRoot(document.getElementById("root")!).render(
  <Providers>
    <SetupPage token={token} />
  </Providers>,
);
