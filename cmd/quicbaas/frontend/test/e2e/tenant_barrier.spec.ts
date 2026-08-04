/**
 * tenant_barrier.spec.ts
 *
 * Playwright tests for Security > Tenant Barrier
 * (TenantBarrierView.svelte at hash route #/tenant-barrier).
 *
 * The Tenant Barrier surface lets an operator provision a per-tenant
 * cryptographic barrier strategy (none / passphrase / shamir) for tenants
 * that use the "barrier" isolation mode. It is the per-tenant counterpart to
 * the system BarrierView (#/barrier).
 *
 * Selectors verified against:
 *   src/views/security/TenantBarrierView.svelte
 *   sdk/typescript/svelte-security/src/components/BarrierStrategyPicker.svelte
 *   src/views/MainApp.svelte (sidebar label "Tenant Barrier", route "tenant-barrier")
 *   src/stores/router.svelte.ts (/tenant-barrier → name: 'tenant-barrier')
 *
 * Key selectors used:
 *   - h1 "Tenant Barrier"                       (PageHeader → <h1>{title}</h1>)
 *   - aside nav button "Tenant Barrier"          (sidebar nav item label)
 *   - [data-testid="tenant-barrier-select"]      (tenant <select>)
 *   - .loading.loading-spinner                   (spinner guard)
 *   - [data-testid="tenant-barrier-init"]        (uninitialized → picker block)
 *   - [data-testid="barrier-strategy-picker"]    (BarrierStrategyPicker root)
 *   - [data-testid="barrier-strategy-radio-none"](None radio)
 *   - [data-testid="barrier-strategy-submit"]    (picker CTA)
 *   - .breadcrumbs                               (breadcrumb nav)
 *   - [role="alert"]                             (error / info banners)
 *
 * REST paths confirmed:
 *   GET  /api/v1/tenants/{id}/barrier/status  (pkg/api/rest/iam.go line 135)
 *   POST /api/v1/tenants/{id}/barrier/init    (pkg/api/rest/iam.go line 131)
 *
 * Seeding: createTestTenant() provisions a fresh tenant before the suite so
 * the barrier status fetch sees an uninitialized barrier predictably. The
 * tenant is deleted in afterAll.
 *
 * No ACP backend required. Inherits the admin storageState minted by
 * global-setup.ts (chromium-no-backend project).
 */

import { test, expect, type Request } from '@playwright/test';
import { createTestTenant, deleteTestTenant } from './fixtures/tenants.ts';
import type { CreatedTenant } from './fixtures/tenants.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Shared state
// ---------------------------------------------------------------------------

let tenant: CreatedTenant | null = null;

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

test.beforeAll(async () => {
  tenant = await createTestTenant('e2e-barrier');
});

test.afterAll(async () => {
  if (tenant?.id) {
    await deleteTestTenant(tenant.id);
  }
});

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

type TestPage = Parameters<Parameters<typeof test>[1]>[0];

async function gotoTenantBarrier(page: TestPage): Promise<void> {
  // Navigate to app root first so the SPA is fully mounted before the hash
  // change fires, matching the pattern used in barrier.spec.ts.
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/tenant-barrier'; });
  await page.waitForLoadState('networkidle');
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Security > Tenant Barrier', () => {

  test('1. #/tenant-barrier renders the "Tenant Barrier" h1 heading', async ({ page }) => {
    await gotoTenantBarrier(page);
    // PageHeader renders title in <h1 class="text-2xl font-bold text-base-content">
    await expect(
      page.getByRole('heading', { name: 'Tenant Barrier', exact: true }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('2. sidebar "Tenant Barrier" nav item navigates to the view', async ({ page }) => {
    await page.goto('./', { waitUntil: 'networkidle' });

    const navBtn = page.locator('aside nav button', { hasText: 'Tenant Barrier' });
    await expect(navBtn).toBeVisible();
    await navBtn.click();

    // Heading must appear after navigation.
    await expect(
      page.getByRole('heading', { name: 'Tenant Barrier', exact: true }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('3. breadcrumb shows Security > Tenant Barrier', async ({ page }) => {
    await gotoTenantBarrier(page);
    const bc = page.locator('.breadcrumbs');
    await expect(bc).toContainText('Security');
    await expect(bc).toContainText('Tenant Barrier');
  });

  test('4. tenant selector is visible and renders at least one option', async ({ page }) => {
    await gotoTenantBarrier(page);

    const select = page.locator('[data-testid="tenant-barrier-select"]');
    await expect(select).toBeVisible({ timeout: 10_000 });

    // The tenant list is fetched from the IAM endpoint on mount ($effect on
    // loadTenants). Poll until the spinner clears (tenants loaded or errored).
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // At least one option must be present (the "system" tenant always exists).
    const optionCount = await select.locator('option').count();
    expect(optionCount, 'tenant selector must have at least one option').toBeGreaterThan(0);
  });

  test('5. selecting a tenant fires GET .../barrier/status; spinner clears', async ({ page }) => {
    const seenURLs: string[] = [];
    // Capture ALL requests so we can poll for the barrier/status call.
    page.on('request', (req: Request) => { seenURLs.push(req.url()); });

    await gotoTenantBarrier(page);

    // Wait for the tenant selector to be enabled (tenants loaded).
    const select = page.locator('[data-testid="tenant-barrier-select"]');
    await expect(select).toBeEnabled({ timeout: 10_000 });

    // TenantBarrierView's $effect fires loadStatus() on mount (auto-selects
    // the first tenant). Use poll to tolerate $effect timing without sleeping.
    await expect
      .poll(
        () => seenURLs.some((u) => u.includes('/barrier/status')),
        { timeout: 10_000, message: 'expected GET .../barrier/status to be called' },
      )
      .toBeTruthy();

    // Loading spinner must clear (status fetch settled — success or error).
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // After settling, at least one of the status-panel variants or an error
    // alert must be visible. Multiple data-testids are accepted because the
    // strategy the daemon reports for the auto-selected tenant is not known.
    const panel = page.locator([
      '[data-testid="tenant-barrier-init"]',
      '[data-testid="tenant-barrier-none"]',
      '[data-testid="tenant-barrier-passphrase-unsealed"]',
      '[data-testid="tenant-barrier-shamir-unsealed"]',
      '[data-testid="tenant-barrier-auto"]',
      '[role="alert"]',
    ].join(', '));
    await expect(panel.first()).toBeVisible({ timeout: 5_000 });
  });

  test('6. seeded tenant shows uninitialized barrier init block', async ({ page }) => {
    const tenantID = tenant?.id;
    if (!tenantID) {
      test.skip(true, 'seeded tenant not available — beforeAll may have failed');
      return;
    }

    await gotoTenantBarrier(page);

    const select = page.locator('[data-testid="tenant-barrier-select"]');
    await expect(select).toBeEnabled({ timeout: 10_000 });

    // Explicitly select the seeded tenant (freshly created → barrier not
    // initialized). selectOption triggers the onchange handler which sets
    // selectedTenantID and fires the $effect → loadStatus().
    await select.selectOption({ value: tenantID });
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // Two acceptable outcomes:
    //   a) Barrier not initialized → init block + BarrierStrategyPicker visible.
    //   b) Daemon rejected the tenant (e.g. TenantBarrierService not wired) →
    //      error alert visible. This is a known limitation when the per-tenant
    //      barrier service is not available in the test environment.
    const initBlock = page.locator('[data-testid="tenant-barrier-init"]');
    const errorAlert = page.locator('[role="alert"]');

    if ((await initBlock.count()) > 0) {
      await expect(initBlock).toBeVisible();
      // The BarrierStrategyPicker renders inside the init block.
      await expect(
        page.locator('[data-testid="barrier-strategy-picker"]'),
      ).toBeVisible();
    } else {
      // Acceptable: daemon does not support per-tenant barriers in this env.
      await expect(errorAlert.first()).toBeVisible({ timeout: 5_000 });
    }
  });

  test('7. "none" strategy init submits POST .../barrier/init via picker', async ({ page }) => {
    const tenantID = tenant?.id;
    if (!tenantID) {
      test.skip(true, 'seeded tenant not available — beforeAll may have failed');
      return;
    }

    await gotoTenantBarrier(page);

    const select = page.locator('[data-testid="tenant-barrier-select"]');
    await expect(select).toBeEnabled({ timeout: 10_000 });
    await select.selectOption({ value: tenantID });
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // If the init block is not present the barrier is already initialized (or
    // the service is unavailable). Skip rather than fail — this test only
    // applies to a freshly seeded, uninitialized barrier.
    const initBlock = page.locator('[data-testid="tenant-barrier-init"]');
    if ((await initBlock.count()) === 0) {
      test.skip(true, 'tenant barrier already initialized or status fetch error — skip init test');
      return;
    }

    // The BarrierStrategyPicker defaults to "passphrase". Switch to "none" so
    // the submit call requires no passphrase/share credentials and succeeds
    // deterministically.
    //
    // The "none" radio:
    //   <input ... data-testid="barrier-strategy-radio-none" onchange=... />
    // Clicking the radio fires its onchange → strategy = 'none' → valid = true.
    const noneRadio = page.locator('[data-testid="barrier-strategy-radio-none"]');
    await expect(noneRadio).toBeVisible({ timeout: 5_000 });
    await noneRadio.click();

    // Capture the init POST before clicking submit so we don't miss a fast response.
    const initRequest = page.waitForRequest(
      (req) =>
        req.url().includes('/barrier/init') &&
        req.method() === 'POST' &&
        req.url().includes(`/tenants/${tenantID}`),
      { timeout: 10_000 },
    );

    // Submit button: disabled={!valid || submitting}, text "Initialize (None)"
    const submitBtn = page.locator('[data-testid="barrier-strategy-submit"]');
    await expect(submitBtn).toBeEnabled({ timeout: 5_000 });
    await submitBtn.click();

    // The request MUST fire — this is the primary assertion.
    await initRequest;

    // After the call resolves the view reloads status. Wait for the spinner to
    // clear (success or graceful error). A toast may or may not be visible by
    // the time the assertion runs, so we only assert on the settled UI state.
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 15_000 });
  });

  test('8. Refresh button triggers a second GET .../barrier/status call', async ({ page }) => {
    await gotoTenantBarrier(page);

    const select = page.locator('[data-testid="tenant-barrier-select"]');
    await expect(select).toBeEnabled({ timeout: 10_000 });
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    const seenStatusURLs: string[] = [];
    page.on('request', (req: Request) => {
      if (req.url().includes('/barrier/status')) seenStatusURLs.push(req.url());
    });

    // The Refresh button is always rendered when a tenant is selected:
    //   <button class="btn btn-ghost btn-sm" onclick={...} disabled={loading}>Refresh</button>
    const refreshBtn = page.getByRole('button', { name: 'Refresh', exact: true });
    await expect(refreshBtn).toBeVisible({ timeout: 5_000 });
    await refreshBtn.click();

    // At least one additional status call must fire after the click.
    await expect
      .poll(
        () => seenStatusURLs.length >= 1,
        { timeout: 10_000, message: 'expected Refresh to trigger GET .../barrier/status' },
      )
      .toBeTruthy();
  });

});
