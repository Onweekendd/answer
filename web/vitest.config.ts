import path from 'node:path'
import { defineConfig } from 'vitest/config'

// core/ is plain TS and runs in node; hook and component tests opt in to jsdom
// with a `// @vitest-environment jsdom` comment.
export default defineConfig({
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, 'src') },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['src/test/setup.ts'],
  },
})
