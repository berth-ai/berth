import { createRoot } from "react-dom/client";
import { Providers } from "@/app/providers";
import { applyTheme, storedTheme } from "@/lib/theme";
import { LoginPage } from "./login";
import "@/styles/theme.css";

applyTheme(storedTheme());

createRoot(document.getElementById("root")!).render(
  <Providers>
    <LoginPage />
  </Providers>,
);
