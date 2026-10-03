import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const here = (path: string) => fileURLToPath(new URL(path, import.meta.url));

// The Go panel serves every built file under /static/ and the four pages at
// /, /login, /setup and (inside the proxies) the "not running" page.
export default defineConfig({
  base: "/static/",
  plugins: [
    // The router plugin must run before the React plugin.
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react({ compiler: true }),
    tailwindcss(),
  ],
  resolve: { alias: { "@": here("./src") } },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    modulePreload: { polyfill: false },
    rolldownOptions: {
      input: {
        index: here("./index.html"),
        login: here("./login.html"),
        setup: here("./setup.html"),
        unavailable: here("./unavailable.html"),
      },
    },
  },
});
