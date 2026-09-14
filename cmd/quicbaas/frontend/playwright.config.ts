import { defineConfig, devices } from '@playwright/test';
import { ADMIN_STORAGE_STATE } from './test/e2e/fixtures/auth.ts';
import { chromiumLaunchOptions } from './test/e2e/fixtures/browser.ts';

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
// Override the base URL via QUICBAAS_UI_URL. Both projects also run unchanged
// against an already-running node as an existing tenant user — see
// test/e2e/global-setup.ts and `make test-e2e-live`.
export default defineConfig({
  testDir: './test/e2e',
  // global-setup mints a real session against the live daemon — first-boot
  // invite → passkey activate → passkey login for a fresh daemon, or admin-mTLS
  // passkey enrolment → passkey login for an existing tenant user on a running
  // node — and persists it to ADMIN_STORAGE_STATE + the token file. Every
  // authenticated project reuses that session; specs that exercise the
  // logged-out flow opt out with
  // `test.use({ storageState: { cookies: [], origins: [] } })`.
  globalSetup: './test/e2e/global-setup.ts',
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
    // Sandbox drop in a container, plus the CA trust store when a live node was
    // configured. Shared with global-setup so the setup browser and the spec
    // browsers are launched identically — see test/e2e/fixtures/browser.ts.
    launchOptions: chromiumLaunchOptions(),
  },
  projects: [
    {
      // Default project: all specs except the ACP lifecycle suite.
      name: 'chromium-no-backend',
      use: { ...devices['Desktop Chrome'], storageState: ADMIN_STORAGE_STATE },
      testIgnore: ['**/session_lifecycle.spec.ts'],
    },
    {
      // ACP-gated project: only the lifecycle suite, requires QUICBAAS_E2E_BACKEND=acp.
      name: 'chromium-acp',
      use: { ...devices['Desktop Chrome'], storageState: ADMIN_STORAGE_STATE },
      testMatch: ['**/session_lifecycle.spec.ts'],
    },
  ],
});
