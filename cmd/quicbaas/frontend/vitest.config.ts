import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte({ hot: !process.env['VITEST'] })],
  test: {
    // jsdom with browser-mode Svelte compilation (prevents server-side mount errors).
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./test/setup.ts'],
    include: ['test/**/*.test.ts'],
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
