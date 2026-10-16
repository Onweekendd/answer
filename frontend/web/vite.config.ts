import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The Go server runs on :8081 (site_url); the proxy keeps requests same-origin in dev.
const backend = 'http://localhost:8081'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, 'src') },
  },
  server: {
    proxy: {
      '/answer': backend,
      '/uploads': backend,
    },
  },
})
