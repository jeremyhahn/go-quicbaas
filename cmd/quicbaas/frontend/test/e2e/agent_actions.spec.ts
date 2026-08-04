/**
 * agent_actions.spec.ts
 *
 * Operational round-trip tests for the Agent Fleet (#/fleet) and
 * AgentDetailView (#/agents/:id).
 *
 * The existing agent_disable.spec.ts only asserts that the Disable / Remove
 * buttons are VISIBLE.  This spec goes further: it seeds a real agent via
 * REST, exercises the Disable → Enable cycle through the browser UI,
 * waits for the server round-trip response, and asserts the status actually
 * flips.  It also exercises Remove on a throwaway seeded agent and asserts
 * the agent disappears from the fleet.
 *
 * GATING: agent register (POST /api/v1/orchestrator/agents) and remove
 * (DELETE /api/v1/orchestrator/agents/{id}) are control-plane ops that work
 * WITHOUT an ACP backend.  Disable and Enable are routed through the REST
 * TypeScript SDK, which sends:
 *
 *   POST /api/v1/orchestrator/agents/{id}/disable   (sdk/typescript/src/rest.ts)
 *   POST /api/v1/orchestrator/agents/{id}/enable    (sdk/typescript/src/rest.ts)
 *
 * The server registers these handlers inside the /orchestrator subrouter at
 * /api/v1/orchestrator/agents/{agentID}/disable|enable, matching the SDK path.
 * (Fixed in pkg/api/rest/handlers_orchestrator.go; old /api/v1/agents/... routes
 * were removed from server.go.)
 *
 * Selectors verified against:
 *   src/components/orchestrator/AgentFleet.svelte  (card: aria-label)
 *   src/views/orchestrator/AgentDetailView.svelte  (buttons, dialogs, badge)
 *   src/components/core/StatusBadge.svelte         (badge class mapping)
 *
 * REST endpoints verified against:
 *   pkg/api/rest/handlers_orchestrator.go — RegisterAgent (line 1138), RemoveAgent (line 1252)
 *   pkg/api/rest/handlers_agent_admin.go  — DisableAgent (line 66), EnableAgent (line 104)
 *   sdk/typescript/src/rest.ts            — disableAgent (line 641), enableAgent (line 650)
 *
 * Run:
 *   cd frontend && QUICBAAS_UI_URL=https://localhost:18080/ui/ \
 *     NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
 *     npx playwright test test/e2e/agent_actions.spec.ts
 */

import { test, expect } from '@playwright/test';
import { seedAgent, removeSeededAgent } from './fixtures/agents_seed.ts';
import type { SeededAgent } from './fixtures/agents_seed.ts';

test.use({ ignoreHTTPSErrors: true });

test.describe('Agent Fleet — operational disable / enable / remove round-trips', () => {
  // Serial execution: each test builds on the previous one.
  test.describe.configure({ mode: 'serial' });

  // Agents seeded before the suite and cleaned up after.
  let primaryAgent: SeededAgent;   // used for Disable → Enable cycle
  let disposableAgent: SeededAgent; // used for the Remove test; deleted by the test itself

  // -------------------------------------------------------------------------
  // Setup / teardown
  // -------------------------------------------------------------------------

  test.beforeAll(async () => {
    // Seed two agents via the orchestrator REST API (bearer token, no mTLS).
    // POST /api/v1/orchestrator/agents → 201 { agent: { id, name, ... } }
    primaryAgent = await seedAgent('e2e-primary');
    disposableAgent = await seedAgent('e2e-disposable');
  });

  test.afterAll(async () => {
    // Best-effort cleanup.  Both agents may have already been removed by a
    // test step; removeSeededAgent swallows 404 errors silently.
    await removeSeededAgent(primaryAgent.id);
    await removeSeededAgent(disposableAgent.id);
  });

  // -------------------------------------------------------------------------
  // Navigation helpers
  // -------------------------------------------------------------------------

  /**
   * Navigate to the fleet view and wait for the URL to settle.
   *
   * Why no waitForResponse: refreshAgents() is called from MainApp's $effect
   * on initial app mount (MainApp.svelte line 259).  By the time
   * page.goto('/ui/', { waitUntil: 'networkidle' }) resolves, the GET
   * /api/v1/orchestrator/agents response has already been received and
   * agentStore.agents is populated.  AgentFleet does NOT issue a second GET
   * on route navigation — it renders directly from the pre-populated store.
   * Registering a waitForResponse after networkidle and expecting it to fire
   * on fleet navigation therefore always times out.
   *
   * Individual tests that need the grid populated use toBeVisible({ timeout })
   * which is sufficient because the store is already hydrated.
   *
   * Endpoint: GET /api/v1/orchestrator/agents?tenant_id=system
   *   → HandleOrchestratorListAgents (handlers_orchestrator.go line 1190)
   */
  async function goToFleet(page: Parameters<Parameters<typeof test>[1]>[0]): Promise<void> {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Agent Fleet' }).first().click();
    await expect(page).toHaveURL(/#\/fleet/);
  }

  /**
   * Navigate to a specific agent's detail page via its fleet card.
   *
   * The card aria-label is "View details for agent {agent.name}" — verified
   * in AgentFleet.svelte line 281.
   */
  async function goToAgentDetail(
    page: Parameters<Parameters<typeof test>[1]>[0],
    agent: SeededAgent,
  ): Promise<void> {
    await goToFleet(page);
    // Locate the specific card for our seeded agent.
    const card = page.locator(`[role="button"][aria-label="View details for agent ${agent.name}"]`);
    await expect(card).toBeVisible({ timeout: 15_000 });
    await card.click();
    await expect(page).toHaveURL(new RegExp(`#/agents/${agent.id}`), { timeout: 5_000 });
    // Wait for AgentDetailView to resolve the agent from the store.
    await expect(page.locator('h1', { hasText: agent.name })).toBeVisible({ timeout: 10_000 });
  }

  // -------------------------------------------------------------------------
  // Tests
  // -------------------------------------------------------------------------

  test('1. Seeded agents appear as cards in the fleet grid', async ({ page }) => {
    await goToFleet(page);

    // Both seeded agents must be present.  AgentFleet.svelte renders each
    // registration as a card with role="button" and a descriptive aria-label.
    const primaryCard = page.locator(
      `[role="button"][aria-label="View details for agent ${primaryAgent.name}"]`,
    );
    const disposableCard = page.locator(
      `[role="button"][aria-label="View details for agent ${disposableAgent.name}"]`,
    );
    await expect(primaryCard).toBeVisible({ timeout: 15_000 });
    await expect(disposableCard).toBeVisible({ timeout: 5_000 });
  });

  test('2. Agent detail page renders identity and action buttons', async ({ page }) => {
    await goToAgentDetail(page, primaryAgent);

    // Identity section must show the backend badge — verified in
    // AgentDetailView.svelte line 304: <div class="badge badge-sm badge-primary ...">
    await expect(page.locator('.badge-primary').first()).toBeVisible({ timeout: 5_000 });

    // Action buttons must be present — verified in AgentDetailView.svelte lines 281–294.
    // The Disable button label toggles: "Disable agent" when active, "Enable agent" when disabled.
    await expect(
      page.locator('button[aria-label="Disable agent"]').or(
        page.locator('button[aria-label="Enable agent"]'),
      ).first(),
    ).toBeVisible({ timeout: 5_000 });
    await expect(page.locator('button[aria-label="Remove agent"]').first()).toBeVisible();
  });

  test('3. Disable: confirm dialog appears and POST returns 2xx, status flips to disabled', async ({ page }) => {

    await goToAgentDetail(page, primaryAgent);

    // The agent was freshly registered so it starts as NOT disabled.
    // The button should read "Disable agent".
    const disableBtn = page.locator('button[aria-label="Disable agent"]');
    await expect(disableBtn).toBeVisible({ timeout: 10_000 });

    // Click "Disable agent" — this opens the confirmation dialog.
    await disableBtn.click();

    // Confirm dialog appears — aria-label="Confirm disable agent"
    // (AgentDetailView.svelte line 202, derived from !isDisabled case).
    const dialog = page.locator('dialog[aria-label="Confirm disable agent"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });
    await expect(dialog.locator('h3', { hasText: 'Disable Agent?' })).toBeVisible();

    // Capture the disable HTTP response BEFORE clicking the action button.
    // SDK sends: POST /api/v1/orchestrator/agents/{id}/disable
    const disableResponsePromise = page.waitForResponse(
      (resp) =>
        resp.url().includes('/agents/') &&
        resp.url().includes('/disable') &&
        resp.request().method() === 'POST',
      { timeout: 10_000 },
    );

    // Click the "Disable" action button inside the dialog.
    // Verified: AgentDetailView.svelte line 228-235 — btn-warning class, text "Disable"
    await dialog.locator('button', { hasText: 'Disable' }).click();

    const disableResp = await disableResponsePromise;

    expect(
      disableResp.status(),
      `Disable POST returned ${disableResp.status()} — expected 2xx.`,
    ).toBeLessThan(300);

    // The dialog must close.
    await expect(dialog).not.toBeVisible({ timeout: 5_000 });

    // After refreshAgents() completes, the status badge must show "disabled".
    // StatusBadge renders badge-error for variant "error" (statusVariant("disabled") === "error").
    // Verified: AgentDetailView.svelte lines 68–74 + StatusBadge.svelte line 49.
    await expect(
      page.locator('.badge.badge-error', { hasText: 'disabled' }).first(),
    ).toBeVisible({ timeout: 10_000 });

    // The Disable button must now show "Enable agent" (isDisabled=true toggles label).
    // Verified: AgentDetailView.svelte line 283.
    await expect(page.locator('button[aria-label="Enable agent"]')).toBeVisible({
      timeout: 5_000,
    });
  });

  test('4. Enable: confirm dialog appears and POST returns 2xx, status flips back', async ({ page }) => {
    // Navigate to the same primary agent; it should be in "disabled" state
    // after test 3 succeeds.
    await goToAgentDetail(page, primaryAgent);

    // If the agent is disabled (test 3 passed), we expect the "Enable agent" button.
    // If the bug persists the agent is still active and we get "Disable agent".
    const enableBtn = page.locator('button[aria-label="Enable agent"]');
    await expect(
      enableBtn,
      'Expected "Enable agent" button — this fails if the Disable test (3) did not succeed ' +
      'due to the routing bug (app_bug_disable_enable_routing)',
    ).toBeVisible({ timeout: 10_000 });

    // Click "Enable agent" — opens the enable confirmation dialog.
    await enableBtn.click();

    // Confirm dialog appears — aria-label="Confirm enable agent"
    // (AgentDetailView.svelte line 202, derived from isDisabled=true case).
    const dialog = page.locator('dialog[aria-label="Confirm enable agent"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });
    await expect(dialog.locator('h3', { hasText: 'Re-enable Agent?' })).toBeVisible();

    // Capture the enable HTTP response.
    // SDK sends: POST /api/v1/orchestrator/agents/{id}/enable
    const enableResponsePromise = page.waitForResponse(
      (resp) =>
        resp.url().includes('/agents/') &&
        resp.url().includes('/enable') &&
        resp.request().method() === 'POST',
      { timeout: 10_000 },
    );

    // Click the "Enable" action button inside the dialog.
    // Verified: AgentDetailView.svelte line 228-235 — btn-success class, text "Enable"
    await dialog.locator('button', { hasText: 'Enable' }).click();

    const enableResp = await enableResponsePromise;

    expect(
      enableResp.status(),
      `Enable POST returned ${enableResp.status()} — expected 2xx.`,
    ).toBeLessThan(300);

    // Dialog must close.
    await expect(dialog).not.toBeVisible({ timeout: 5_000 });

    // Status badge must no longer show "disabled" — the agent is active again.
    // The badge for a running/idle/registered agent uses badge-success or badge-info.
    await expect(
      page.locator('.badge.badge-error', { hasText: 'disabled' }),
    ).toHaveCount(0, { timeout: 10_000 });

    // The button must have reverted to "Disable agent".
    await expect(page.locator('button[aria-label="Disable agent"]')).toBeVisible({
      timeout: 5_000,
    });
  });

  test('5. Remove: DELETE returns 2xx, agent disappears from fleet and afterAll cleanup is no-op', async ({ page }) => {
    // The Remove action on AgentDetailView posts to:
    //   DELETE /api/v1/orchestrator/agents/{id}?tenant_id=system
    //   → HandleOrchestratorRemoveAgent (handlers_orchestrator.go line 1252)
    // The SDK path is correct: fetchJSON('/agents/{id}?...', DELETE) → /api/v1/orchestrator/agents/{id}.

    await goToAgentDetail(page, disposableAgent);

    // Click "Remove agent" — opens the removal confirmation dialog.
    // Verified: AgentDetailView.svelte line 290.
    const removeBtn = page.locator('button[aria-label="Remove agent"]');
    await expect(removeBtn).toBeVisible({ timeout: 10_000 });
    await removeBtn.click();

    // Confirm dialog appears — aria-label="Confirm remove agent"
    // Verified: AgentDetailView.svelte line 163.
    const dialog = page.locator('dialog[aria-label="Confirm remove agent"]');
    await expect(dialog).toBeVisible({ timeout: 5_000 });
    await expect(dialog.locator('h3', { hasText: 'Remove Agent?' })).toBeVisible();

    // Capture the remove HTTP response before confirming.
    // Exact URL: DELETE /api/v1/orchestrator/agents/{disposableAgent.id}?tenant_id=system
    const removeResponsePromise = page.waitForResponse(
      (resp) =>
        resp.url().includes(`/orchestrator/agents/${disposableAgent.id}`) &&
        resp.request().method() === 'DELETE',
      { timeout: 10_000 },
    );

    // Click "Remove" in the dialog.
    // Verified: AgentDetailView.svelte line 187 — class btn-error, text "Remove"
    await dialog.locator('button.btn-error', { hasText: 'Remove' }).click();

    const removeResp = await removeResponsePromise;

    expect(
      removeResp.status(),
      `Remove DELETE returned ${removeResp.status()} — expected 2xx`,
    ).toBeLessThan(300);

    // After removal, AgentDetailView calls routeFleet() which navigates back to #/fleet.
    // Verified: AgentDetailView.svelte line 119.
    await expect(page).toHaveURL(/#\/fleet/, { timeout: 10_000 });

    // The disposable agent's card must no longer be present in the grid.
    // Allow time for refreshAgents() to complete after navigation.
    const disposableCard = page.locator(
      `[role="button"][aria-label="View details for agent ${disposableAgent.name}"]`,
    );
    await expect(disposableCard).toHaveCount(0, { timeout: 15_000 });
  });
});
