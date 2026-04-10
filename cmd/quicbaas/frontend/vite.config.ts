// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  resolve: {
    alias: {
      $lib: "/src/lib",
      $stores: "/src/stores",
      $components: "/src/components",
    },
  },
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      "/agent": { target: "https://localhost:9443", secure: false },
      "/job": { target: "https://localhost:9443", secure: false },
      "/task": { target: "https://localhost:9443", secure: false },
      "/jobs": { target: "https://localhost:9443", secure: false },
      "/tasks": { target: "https://localhost:9443", secure: false },
      "/agents": { target: "https://localhost:9443", secure: false },
      "/backends": { target: "https://localhost:9443", secure: false },
      "/health": { target: "https://localhost:9443", secure: false },
      "/v1": { target: "https://localhost:9443", secure: false },
    },
  },
});
