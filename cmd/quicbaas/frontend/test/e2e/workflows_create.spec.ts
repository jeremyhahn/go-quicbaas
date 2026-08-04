/**
 * workflows_create.spec.ts
 *
 * Operational round-trip tests for the Workflows view (#/workflows).
 *
 * The existing workflows.spec.ts stops at the wizard opening.  This spec
 * drives the six-step WorkflowWizard to completion, asserts the POST returns
 * 201, and verifies the new workflow row appears in the list.  It then
 * exercises the Delete path to prove clean-up also round-trips successfully.
 *
 * WIZARD STEP SUMMARY (verified against WorkflowWizard.svelte):
 *   Step 1 — Basics:   name (#wz-name), mode (input[name="wz-mode"][value="..."])
 *   Step 2 — Agents:   "Add Lead Agent" button, role field (#wz-agent-role-0)
 *   Step 3 — Topology: radio cards (input[name="wz-topology"], input[name="wz-isolation"])
 *   Step 4 — Limits:   #wz-maxiter, #wz-poll, #wz-timeout-item, #wz-timeout-total
 *   Step 5 — Advanced: #wz-verbosity (all defaults are valid)
 *   Step 6 — Review:   "Create Workflow" button (btn-success btn-sm)
 *
 * Navigation between steps uses the "Continue" button (btn-primary btn-sm) at
 * the bottom of each step content area.  The button is disabled if the current
 * step is invalid (stepValid() returns false).
 *
 * MODE CHOICE — AUTONOMOUS:
 *   canned / constrained modes require a "lead" role agent (step2Valid() asserts
 *   agentForms.some(af => af.role === 'lead')).  Autonomous only requires
 *   agentForms.length > 0, making it the simplest path through step 2.
 *
 * GATING: workflow create (POST /api/v1/orchestrator/workflows) is a
 * control-plane operation that works WITHOUT an ACP backend.  The Run action
 * (StartJobDialog) would need an ACP backend to spawn a session; that path is
 * out of scope for the chromium-no-backend project.
 *
 * Selectors verified against:
 *   src/views/orchestrator/WorkflowsView.svelte  (page, buttons, table row)
 *   src/components/orchestrator/WorkflowWizard.svelte (inputs, step tabs, nav)
 *
 * REST endpoints verified against:
 *   pkg/api/rest/handlers_orchestrator.go:
 *     POST   /api/v1/orchestrator/workflows       (line 1615, HandleOrchestratorCreateWorkflow)
 *     GET    /api/v1/orchestrator/workflows        (line 1483, HandleOrchestratorListWorkflows)
 *     DELETE /api/v1/orchestrator/workflows/{name} (line 1585, HandleOrchestratorDeleteWorkflow)
 *   sdk/typescript/src/rest.ts:
 *     createWorkflow (line 548), listWorkflows (line 555), deleteWorkflow (line 585)
 *
 * Run:
 *   cd frontend && QUICBAAS_UI_URL=https://localhost:18080/ui/ \
 *     NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
 *     npx playwright test test/e2e/workflows_create.spec.ts
 */

import { test, expect } from '@playwright/test';
import { deleteSeededWorkflow } from './fixtures/workflows_seed.ts';

test.use({ ignoreHTTPSErrors: true });

test.describe('Workflows — create workflow via wizard round-trip', () => {
  // Serial execution: tests build on shared page state (wizard navigation).
  test.describe.configure({ mode: 'serial' });

  // Unique workflow name generated once for the entire suite run.
  let wfName: string;

  test.beforeAll(async () => {
    wfName = `e2e-wf-${Date.now()}`;
  });

  test.afterAll(async () => {
    // Delete the workflow created during the suite so repeated runs start clean.
    // The deleteWorkflowViaUI test (8b) may have already deleted it;
    // deleteSeededWorkflow swallows 404.
    await deleteSeededWorkflow(wfName);
  });

  // -------------------------------------------------------------------------
  // Navigation helper
  // -------------------------------------------------------------------------

  /**
   * Navigate to the Workflows view via the sidebar and wait for the page
   * heading to confirm the view rendered.
   */
  async function goToWorkflows(page: Parameters<Parameters<typeof test>[1]>[0]): Promise<void> {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Workflows' }).first().click();
    await expect(page).toHaveURL(/#\/workflows/);
    // PageHeader renders an <h1> — verified in WorkflowsView.svelte line 462.
    await expect(page.getByRole('heading', { name: 'Workflows' })).toBeVisible({ timeout: 10_000 });
  }

  // -------------------------------------------------------------------------
  // Tests
  // -------------------------------------------------------------------------

  test('1. Workflows view renders "New Workflow" / "Create workflow" button', async ({ page }) => {
    await goToWorkflows(page);

    // The primary create button — verified WorkflowsView.svelte line 465-469:
    //   aria-label="Create workflow", visible text "New Workflow"
    await expect(
      page.locator('button[aria-label="Create workflow"]'),
    ).toBeVisible({ timeout: 5_000 });

    // The Advanced button (raw JSON editor entry point) must also be present.
    // Verified: WorkflowsView.svelte line 475–479 aria-label="Advanced JSON editor".
    await expect(
      page.locator('button[aria-label="Advanced JSON editor"]'),
    ).toBeVisible();
  });

  test('2. Clicking "New Workflow" opens the wizard modal with step tabs', async ({ page }) => {
    await goToWorkflows(page);

    // Click the "New Workflow" / "Create workflow" button.
    await page.locator('button[aria-label="Create workflow"]').click();

    // Wizard modal — verified WorkflowsView.svelte line 309-312:
    //   role="dialog", aria-label="Workflow wizard"
    const wizard = page.locator('div[role="dialog"][aria-label="Workflow wizard"]');
    await expect(wizard).toBeVisible({ timeout: 5_000 });

    // Six step tabs rendered as role="tab" buttons — verified WorkflowWizard.svelte
    // lines 665-716 (ol[role="tablist"] with button[role="tab"]).
    // Step labels are: Basics, Agents, Topology, Limits, Advanced, Review.
    const tabs = wizard.locator('button[role="tab"]');
    await expect(tabs).toHaveCount(6, { timeout: 5_000 });

    // Step 1 (Basics) must be the active step on open.
    // Verified: WorkflowWizard.svelte line 675 aria-current="step" when active.
    await expect(
      wizard.locator('button[role="tab"][aria-current="step"]', { hasText: 'Basics' }),
    ).toBeVisible();

    // Close via Escape to leave a clean state for subsequent tests.
    await page.keyboard.press('Escape');
    await expect(wizard).not.toBeVisible({ timeout: 5_000 });
  });

  test('3. Step 1 (Basics): enter unique name, select Autonomous mode, Continue is enabled', async ({ page }) => {
    await goToWorkflows(page);
    await page.locator('button[aria-label="Create workflow"]').click();

    const wizard = page.locator('div[role="dialog"][aria-label="Workflow wizard"]');
    await expect(wizard).toBeVisible({ timeout: 5_000 });

    // Step 1 heading — verified WorkflowWizard.svelte line 739.
    await expect(wizard.locator('h2', { hasText: 'Basics' })).toBeVisible();

    // Name input — id="wz-name" (WorkflowWizard.svelte line 752).
    const nameInput = wizard.locator('#wz-name');
    await expect(nameInput).toBeVisible();
    await nameInput.fill(wfName);

    // Select "Autonomous" mode by clicking its <label> card. The radio itself is
    // class="hidden" (display:none) inside the label, so it has no layout box and
    // cannot be clicked directly (even with force) — click the visible label.
    await wizard.locator('h3', { hasText: 'Autonomous' }).click();

    // The "Continue" button must become enabled once name is non-empty.
    // Verified: WorkflowWizard.svelte lines 1833–1841 — btn-primary btn-sm, disabled={!canAdvance()}.
    // step1Valid() = name.trim() !== '' && !existingNames.includes(name)
    const continueBtn = wizard.locator('button.btn-primary.btn-sm', { hasText: 'Continue' });
    await expect(continueBtn).toBeEnabled({ timeout: 5_000 });

    // Close — next test opens a fresh wizard.
    await page.keyboard.press('Escape');
  });

  test('4. Step 2 (Agents): add agent, Continue is enabled; step 3 reachable', async ({ page }) => {
    await goToWorkflows(page);
    await page.locator('button[aria-label="Create workflow"]').click();
    const wizard = page.locator('div[role="dialog"][aria-label="Workflow wizard"]');
    await expect(wizard).toBeVisible({ timeout: 5_000 });

    // Fill Step 1 (required to unlock Continue).
    await wizard.locator('#wz-name').fill(wfName);
    await wizard.locator('h3', { hasText: 'Autonomous' }).click();
    await wizard.locator('button.btn-primary.btn-sm', { hasText: 'Continue' }).click();

    // Step 2 heading — verified WorkflowWizard.svelte line 815.
    await expect(wizard.locator('h2', { hasText: 'Agents' })).toBeVisible({ timeout: 5_000 });
    // Active step tab.
    await expect(
      wizard.locator('button[role="tab"][aria-current="step"]', { hasText: 'Agents' }),
    ).toBeVisible();

    // Empty state: "Add Lead Agent" button — verified WorkflowWizard.svelte line 829.
    // In autonomous mode this adds a 'worker' role agent, which satisfies step2Valid().
    const addBtn = wizard.locator('button.btn-primary.btn-sm', { hasText: 'Add Lead Agent' });
    await expect(addBtn).toBeVisible({ timeout: 5_000 });
    await addBtn.click();

    // Agent form card must appear with the role field — id="wz-agent-role-0"
    // (WorkflowWizard.svelte line 869).
    const roleInput = wizard.locator('#wz-agent-role-0');
    await expect(roleInput).toBeVisible({ timeout: 5_000 });

    // With autonomous mode and at least one agent, Continue must be enabled.
    // step2Valid() = agentForms.length > 0 (lines 422–427 — autonomous branch).
    const continueBtn = wizard.locator('button.btn-primary.btn-sm', { hasText: 'Continue' });
    await expect(continueBtn).toBeEnabled();

    // Advance to Step 3 (Topology).
    await continueBtn.click();
    await expect(wizard.locator('h2', { hasText: 'Topology' })).toBeVisible({ timeout: 5_000 });

    await page.keyboard.press('Escape');
  });

  test('5. Steps 3–5 (Topology, Limits, Advanced): defaults are valid, all advance cleanly', async ({ page }) => {
    await goToWorkflows(page);
    await page.locator('button[aria-label="Create workflow"]').click();
    const wizard = page.locator('div[role="dialog"][aria-label="Workflow wizard"]');
    await expect(wizard).toBeVisible({ timeout: 5_000 });

    const continueBtn = wizard.locator('button.btn-primary.btn-sm', { hasText: 'Continue' });

    // Step 1: fill name + mode.
    await wizard.locator('#wz-name').fill(wfName);
    await wizard.locator('h3', { hasText: 'Autonomous' }).click();
    await continueBtn.click();

    // Step 2: add agent.
    await expect(wizard.locator('h2', { hasText: 'Agents' })).toBeVisible({ timeout: 5_000 });
    await wizard.locator('button.btn-primary.btn-sm', { hasText: 'Add Lead Agent' }).click();
    await continueBtn.click();

    // Step 3 (Topology): defaults are hub_and_spoke / shared — always valid.
    // Verified: WorkflowWizard.svelte lines 1271–1373; stepValid(2) returns true.
    await expect(wizard.locator('h2', { hasText: 'Topology' })).toBeVisible({ timeout: 5_000 });
    // The default hub_and_spoke radio card border should indicate selection.
    await expect(
      wizard.locator('input[type="radio"][name="wz-topology"][value="hub_and_spoke"]'),
    ).toBeChecked({ timeout: 5_000 });
    await continueBtn.click();

    // Step 4 (Limits & Budgets): defaults 30s / 5m / 1h — step4Valid() = true.
    // Verified: WorkflowWizard.svelte lines 1375–1501; DURATION_RE = /^\d+(ms|s|m|h)$/.
    await expect(wizard.locator('h2', { hasText: 'Limits' })).toBeVisible({ timeout: 5_000 });
    // Default poll_interval must be pre-filled with "30s".
    await expect(wizard.locator('#wz-poll')).toHaveValue('30s');
    await continueBtn.click();

    // Step 5 (Advanced): stepValid(4) always returns true.
    // Verified: WorkflowWizard.svelte line 445.
    await expect(wizard.locator('h2', { hasText: 'Advanced' })).toBeVisible({ timeout: 5_000 });
    await continueBtn.click();

    // Step 6 (Review) is now active.
    await expect(wizard.locator('h2', { hasText: 'Review' })).toBeVisible({ timeout: 5_000 });
    await expect(
      wizard.locator('button[role="tab"][aria-current="step"]', { hasText: 'Review' }),
    ).toBeVisible();

    await page.keyboard.press('Escape');
  });

  test('6. Step 6 (Review): summary card shows correct name and mode', async ({ page }) => {
    await goToWorkflows(page);
    await page.locator('button[aria-label="Create workflow"]').click();
    const wizard = page.locator('div[role="dialog"][aria-label="Workflow wizard"]');
    await expect(wizard).toBeVisible({ timeout: 5_000 });

    const continueBtn = wizard.locator('button.btn-primary.btn-sm', { hasText: 'Continue' });

    // Drive through all 5 preceding steps with minimal input.
    await wizard.locator('#wz-name').fill(wfName);
    await wizard.locator('h3', { hasText: 'Autonomous' }).click();
    await continueBtn.click();
    await wizard.locator('button.btn-primary.btn-sm', { hasText: 'Add Lead Agent' }).click();
    await continueBtn.click(); // → step 3
    await continueBtn.click(); // → step 4
    await continueBtn.click(); // → step 5
    await continueBtn.click(); // → step 6

    await expect(wizard.locator('h2', { hasText: 'Review' })).toBeVisible({ timeout: 5_000 });

    // Review summary card — verified WorkflowWizard.svelte lines 1697–1740.
    // The assembled.name and assembled.mode are rendered in text rows.
    await expect(wizard.locator('text=' + wfName).first()).toBeVisible({ timeout: 5_000 });
    // Mode badge — verified line 1712: badge badge-sm badge-primary with mode text.
    await expect(wizard.locator('.badge-primary', { hasText: 'autonomous' })).toBeVisible();

    // Generated YAML section — aria-label="generated-yaml" (line 1792).
    await expect(wizard.locator('pre[aria-label="generated-yaml"]')).toBeVisible();
    const yaml = await wizard.locator('pre[aria-label="generated-yaml"]').textContent();
    expect(yaml).toContain(`name: ${wfName}`);
    expect(yaml).toContain('mode: autonomous');

    // "Create Workflow" button must be present (not Continue).
    // Verified: WorkflowWizard.svelte lines 1843-1854 — btn-success btn-sm.
    await expect(wizard.locator('button.btn-success.btn-sm', { hasText: 'Create Workflow' })).toBeVisible();

    await page.keyboard.press('Escape');
  });

  test('7. Create Workflow: POST /orchestrator/workflows returns 201 and row appears in list', async ({ page }) => {
    await goToWorkflows(page);
    await page.locator('button[aria-label="Create workflow"]').click();
    const wizard = page.locator('div[role="dialog"][aria-label="Workflow wizard"]');
    await expect(wizard).toBeVisible({ timeout: 5_000 });

    const continueBtn = wizard.locator('button.btn-primary.btn-sm', { hasText: 'Continue' });

    // Step 1.
    await wizard.locator('#wz-name').fill(wfName);
    await wizard.locator('h3', { hasText: 'Autonomous' }).click();
    await continueBtn.click();
    // Step 2.
    await wizard.locator('button.btn-primary.btn-sm', { hasText: 'Add Lead Agent' }).click();
    await continueBtn.click();
    // Steps 3, 4, 5 — all defaults valid.
    await continueBtn.click();
    await continueBtn.click();
    await continueBtn.click();

    // We are now on Step 6 (Review).
    await expect(wizard.locator('h2', { hasText: 'Review' })).toBeVisible({ timeout: 5_000 });

    // Capture the create POST response BEFORE clicking "Create Workflow".
    // SDK sends: POST /api/v1/orchestrator/workflows
    //   body: { tenant_id, name, definition }
    // Server responds 201 { workflow: { ... } }
    // Verified: handlers_orchestrator.go line 1660 RespondJSON(w, r, http.StatusCreated, ...)
    const createResponsePromise = page.waitForResponse(
      (resp) =>
        resp.url().includes('/orchestrator/workflows') &&
        resp.request().method() === 'POST',
      { timeout: 15_000 },
    );

    // Click "Create Workflow" — the final wizard action button.
    // Verified: WorkflowWizard.svelte line 1844-1853 — btn-success btn-sm gap-1.
    await wizard.locator('button.btn-success.btn-sm', { hasText: 'Create Workflow' }).click();

    const createResp = await createResponsePromise;

    // Assert 201 Created.
    expect(
      createResp.status(),
      `Create workflow POST returned ${createResp.status()} — expected 201`,
    ).toBe(201);

    // Verify response body contains the workflow definition.
    const respBody = (await createResp.json()) as { workflow?: { name?: string } };
    expect(respBody.workflow?.name).toBe(wfName);

    // Wizard closes after success — WorkflowsView.svelte handleWizardComplete: wizardOpen = false.
    await expect(wizard).not.toBeVisible({ timeout: 5_000 });

    // After wizardOpen = false, fetchWorkflows() reruns:
    //   GET /api/v1/orchestrator/workflows?tenant_id=system
    // The new row must appear in the table.
    // Verified: WorkflowsView.svelte line 524 — <td class="font-mono text-xs font-semibold">{wf.name}</td>
    await expect(
      page.locator('table.table td.font-mono', { hasText: wfName }),
    ).toBeVisible({ timeout: 15_000 });
  });

  test('8. Delete Workflow: inline Delete button opens confirm modal, DELETE returns 2xx, row disappears', async ({ page }) => {
    // This test verifies the delete round-trip.  Prerequisite: the workflow
    // was created in test 7.  If test 7 did not succeed, this test will fail
    // because the workflow row won't be present.

    await goToWorkflows(page);

    // Wait for the workflow list to load and show our workflow.
    // Verified: WorkflowsView.svelte lines 505–578 (table with rows).
    await expect(
      page.locator('table.table td.font-mono', { hasText: wfName }),
    ).toBeVisible({ timeout: 15_000 });

    // The delete button for our workflow row has aria-label="Delete workflow {name}".
    // Verified: WorkflowsView.svelte line 560:
    //   aria-label="Delete workflow {wf.name}"
    const deleteBtn = page.locator(`button[aria-label="Delete workflow ${wfName}"]`);
    await expect(deleteBtn).toBeVisible({ timeout: 5_000 });
    await deleteBtn.click();

    // Delete confirm modal appears — aria-label="Confirm delete workflow".
    // Verified: WorkflowsView.svelte line 429.
    const deleteModal = page.locator('div[role="dialog"][aria-label="Confirm delete workflow"]');
    await expect(deleteModal).toBeVisible({ timeout: 5_000 });

    // The modal must name the workflow being deleted.
    // Verified: WorkflowsView.svelte line 434 — font-mono font-semibold span.
    await expect(deleteModal.locator('span.font-mono', { hasText: wfName })).toBeVisible();

    // Capture the delete response.
    // SDK sends: DELETE /api/v1/orchestrator/workflows/{name}?tenant_id=system
    // Verified: sdk/typescript/src/rest.ts line 586-589.
    const deleteResponsePromise = page.waitForResponse(
      (resp) =>
        resp.url().includes(`/orchestrator/workflows/${encodeURIComponent(wfName)}`) &&
        resp.request().method() === 'DELETE',
      { timeout: 10_000 },
    );

    // Click "Delete" in the confirm modal.
    // Verified: WorkflowsView.svelte line 446 — btn-error, text "Delete".
    await deleteModal.locator('button.btn-error', { hasText: 'Delete' }).click();

    const deleteResp = await deleteResponsePromise;
    expect(
      deleteResp.status(),
      `Delete workflow returned ${deleteResp.status()} — expected 2xx`,
    ).toBeLessThan(300);

    // Modal must close.
    await expect(deleteModal).not.toBeVisible({ timeout: 5_000 });

    // After fetchWorkflows() completes, the row must be gone.
    await expect(
      page.locator('table.table td.font-mono', { hasText: wfName }),
    ).toHaveCount(0, { timeout: 15_000 });
  });
});
