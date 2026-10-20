import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { availableParallelism } from 'node:os';

// The worker pool is sized by the host's CPU count. CI runs these tests in a
// container with no CPU quota on a large shared runner, where that count is
// over a hundred: every fork then loads its modules through the one vitest
// main process at once, and tests that import per tab time out. Four workers
// matches the 4-vCPU runner the tests were tuned on.
const MAX_WORKERS = Math.min(4, availableParallelism());

export default defineConfig({
  plugins: [svelte({ hot: !process.env['VITEST'] })],
  test: {
    // jsdom with browser-mode Svelte compilation (prevents server-side mount errors).
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./test/setup.ts'],
    include: ['test/**/*.test.ts'],
    maxWorkers: MAX_WORKERS,
    minWorkers: 1,
    alias: {
      $lib: '/src/lib',
      $stores: '/src/stores',
      $components: '/src/components',
    },
    // Ensure the svelte package resolves to the browser build, not the SSR build.
    deps: {
      inline: [/svelte/, /@testing-library\/svelte/],
    },
  },
  resolve: {
    conditions: ['browser', 'import', 'module', 'default'],
    alias: {
      $lib: '/src/lib',
      $stores: '/src/stores',
      $components: '/src/components',
    },
  },
});
