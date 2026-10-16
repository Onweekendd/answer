import path from "node:path";

import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The Go server runs on :8081 (site_url); the proxy keeps requests same-origin in dev.
// 127.0.0.1 and not "localhost": Node resolves localhost to ::1 first, and on some Windows
// setups the IPv6 loopback refuses connections ("Bad access"), which leaves the dev server
// or the proxy unreachable.
const backend = "http://127.0.0.1:8081";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "src") }
  },
  server: {
    host: "127.0.0.1",
    proxy: {
      "/answer": backend,
      "/uploads": backend
    }
  }
});
