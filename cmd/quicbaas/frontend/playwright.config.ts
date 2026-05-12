import { defineConfig, devices } from '@playwright/test';

// Playwright configuration for the QuicBaaS embedded web UI.
//
// Two projects are defined:
//
//   chromium-no-backend  (default)
//     Runs every spec EXCEPT session_lifecycle.spec.ts.
//     Works against the daemon started with --embedded-agent --embedded-agent-backend ollama.
//     Expected result: 0 skipped, 0 failed.
//     Invoked by: make test-e2e
//
//   chromium-acp
//     Runs ONLY session_lifecycle.spec.ts.
//     Requires an ACP backend (claude / codex / opencode) on PATH and
//     QUICBAAS_E2E_BACKEND=acp set in the environment.
//     Invoked by: make test-e2e-acp
//
// Override the base URL via QUICBAAS_UI_URL.
export default defineConfig({
  testDir: './test/e2e',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: process.env.QUICBAAS_UI_URL ?? 'http://localhost:18080/ui/',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      // Default project: all specs except the ACP lifecycle suite.
      name: 'chromium-no-backend',
      use: { ...devices['Desktop Chrome'] },
      testIgnore: ['**/session_lifecycle.spec.ts'],
    },
    {
      // ACP-gated project: only the lifecycle suite, requires QUICBAAS_E2E_BACKEND=acp.
      name: 'chromium-acp',
      use: { ...devices['Desktop Chrome'] },
      testMatch: ['**/session_lifecycle.spec.ts'],
    },
  ],
});
