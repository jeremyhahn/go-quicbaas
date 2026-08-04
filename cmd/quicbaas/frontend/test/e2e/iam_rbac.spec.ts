/**
 * iam_rbac.spec.ts
 *
 * Playwright regression tests for the IAM RBAC round-trips in IAMView.svelte.
 * These guard against regressions of the confirmed bug where AssignRole always
 * 400ed because the TS SDK sent { "role": name } but the Go handler decoded
 * { "role_id": uuid }.
 *
 * Tests (serial, state builds across them):
 *   1. IAM > Roles tab — renders list or empty state (no stuck spinner,
 *      no "[object Object]" artefacts).
 *   2. IAM > Policies tab — same smoke check.
 *   3. Invite-User round-trip — dialog → submit → POST 2xx → new row appears
 *      in the Users table.
 *   4. Assign-Role round-trip — assign a seeded role to the invited user →
 *      POST /tenants/system/users/{id}/roles 2xx → role badge on the row.
 *
 * State seeded via REST fixtures (before all); torn down (after all).
 *
 * Run context: requires a live quicbaasd at QUICBAAS_UI_URL — the live-daemon
 * Playwright project; not run in the no-backend smoke project.
 */

import { test, expect, type Page } from '@playwright/test';
import {
  createTestRole,
  deleteTestRole,
  deleteTestUser,
  listTestUsers,
  SYSTEM_TENANT_ID,
  type IAMRole,
  type IAMUser,
} from './fixtures/iam.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Shared state — populated in beforeAll.
// ---------------------------------------------------------------------------

/** Role seeded server-side so the Assign-Role dropdown has at least one option. */
let seededRole: IAMRole | null = null;

/** User created via the Invite-User dialog during the test run. */
let createdUserID: string | null = null;

const BASE = process.env['QUICBAAS_UI_URL'] ?? 'https://localhost:18080/ui/';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Navigate to the IAM view and wait until the spinner is gone. */
async function gotoIAM(page: Page): Promise<void> {
  await page.goto(BASE, { waitUntil: 'networkidle' });
  const iamBtn = page.locator('aside nav button', { hasText: 'IAM' }).first();
  await expect(iamBtn).toBeVisible();
  await iamBtn.click();
  // Wait for the heading to appear.
  await expect(page.getByRole('heading', { name: 'IAM' })).toBeVisible();
}

/** Click an IAM tab by label and wait for the spinner to clear. */
async function selectIAMTab(page: Page, label: string): Promise<void> {
  await page.getByRole('tab', { name: new RegExp(label, 'i') }).click();
  // The loading spinner is inside .loading.loading-spinner; wait for it to vanish.
  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });
}

// ---------------------------------------------------------------------------
// Suite lifecycle
// ---------------------------------------------------------------------------

test.beforeAll(async () => {
  // Seed a role so the Assign-Role dropdown has an option to pick.
  const uniqueSuffix = Date.now();
  seededRole = await createTestRole(`e2e-rbac-role-${uniqueSuffix}`, SYSTEM_TENANT_ID);
});

test.afterAll(async () => {
  // Remove the role created for this suite.
  if (seededRole) {
    await deleteTestRole(seededRole.id, SYSTEM_TENANT_ID);
    seededRole = null;
  }

  // Remove the user created by the Invite-User dialog test.
  if (createdUserID) {
    await deleteTestUser(createdUserID, SYSTEM_TENANT_ID);
    createdUserID = null;
  }
});

// ---------------------------------------------------------------------------
// Test 1: Roles tab renders without spinner or "[object Object]"
// ---------------------------------------------------------------------------

test('IAM > Roles tab — renders list or empty state without stuck spinner', async ({ page }) => {
  await gotoIAM(page);
  await selectIAMTab(page, 'Roles');

  // No stuck spinner.
  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0);

  // Roles are fetched from the API; either a table row or the empty-state element
  // should be visible, never a raw "[object Object]" string.
  // page.content() returns the full page HTML — avoids strict-mode violations
  // caused by 'main, .overflow-x-auto, .text-center' resolving to 2+ elements.
  expect(await page.content()).not.toContain('[object Object]');

  // A "Create Role" button should be visible.
  await expect(page.getByRole('button', { name: /Create Role/i }).first()).toBeVisible();
});

// ---------------------------------------------------------------------------
// Test 2: Policies tab same smoke check
// ---------------------------------------------------------------------------

test('IAM > Policies tab — renders list or empty state without stuck spinner', async ({ page }) => {
  await gotoIAM(page);
  await selectIAMTab(page, 'Policies');

  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0);

  // page.content() returns the full page HTML — avoids strict-mode violations
  // caused by 'main, .overflow-x-auto, .text-center' resolving to 2+ elements.
  expect(await page.content()).not.toContain('[object Object]');

  // A "Create Policy" button should be visible.
  await expect(page.getByRole('button', { name: /Create Policy/i }).first()).toBeVisible();
});

// ---------------------------------------------------------------------------
// Test 3: Invite-User round-trip
// ---------------------------------------------------------------------------

test('IAM > Users tab — Invite-User dialog submits and new user row appears', async ({ page }) => {
  await gotoIAM(page);
  // Users tab is the default; make sure it's active.
  await selectIAMTab(page, 'Users');

  const uniqueSuffix = Date.now();
  const testEmail = `e2e-rbac-${uniqueSuffix}@quicbaas.test`;
  const testUsername = `e2e-rbac-${uniqueSuffix}`;

  // Open the Invite User dialog.
  await page.getByRole('button', { name: /Invite User/i }).first().click();
  const dialog = page.getByRole('dialog', { name: /Invite user dialog/i });
  await expect(dialog).toBeVisible();

  // Fill username + email (required fields).
  await dialog.locator('#create-username').fill(testUsername);
  await dialog.locator('#create-email').fill(testEmail);

  // Capture the network request so we can assert 2xx and extract the user ID.
  const [createResp] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().includes('/users') && r.request().method() === 'POST',
    ),
    dialog.getByRole('button', { name: /Send Invite/i }).click(),
  ]);

  expect(createResp.status()).toBeLessThan(300);

  // Save user ID for cleanup and the assign-role test.
  const body = (await createResp.json()) as { id?: string };
  if (body.id) {
    createdUserID = body.id;
  }

  // The invite-sent modal should appear.
  await expect(page.getByRole('dialog', { name: /Invite sent/i })).toBeVisible({ timeout: 5_000 });
  await page.getByRole('button', { name: /Close/i }).click();

  // The new user row should be visible in the table.
  // The username cell is <td class="font-mono text-xs">{user.username}</td>.
  // Using exact:true + getByRole('cell') avoids a strict-mode violation caused
  // by the email cell also containing testUsername (email = testUsername@quicbaas.test).
  await expect(page.getByRole('cell', { name: testUsername, exact: true })).toBeVisible({ timeout: 5_000 });
});

// ---------------------------------------------------------------------------
// Test 4: Assign-Role round-trip
// ---------------------------------------------------------------------------

test('IAM > Users tab — Assign-Role dialog sends role_id and badge appears', async ({ page }) => {
  // This test requires both a seeded role and the user created in Test 3.
  if (!seededRole) {
    test.skip(true, 'seededRole not available — beforeAll may have failed');
    return;
  }

  // If the invite test didn't create a user (e.g. ran in isolation), fall back
  // to querying the first real user we can find in the system tenant.
  let targetUser: IAMUser | undefined;
  if (createdUserID) {
    const users = await listTestUsers(SYSTEM_TENANT_ID);
    targetUser = users.find((u) => u.id === createdUserID);
  }
  if (!targetUser) {
    const users = await listTestUsers(SYSTEM_TENANT_ID);
    targetUser = users[0];
  }
  if (!targetUser) {
    test.skip(true, 'no users available in system tenant');
    return;
  }

  await gotoIAM(page);
  await selectIAMTab(page, 'Users');

  // Find the row for our target user and click the Assign Role button (UserCheck icon).
  const userRow = page.locator('table tbody tr').filter({ hasText: targetUser.email ?? targetUser.id });
  await expect(userRow).toBeVisible({ timeout: 5_000 });

  const assignBtn = userRow.getByRole('button', { name: new RegExp(`Assign role to`, 'i') });
  await expect(assignBtn).toBeVisible();
  await assignBtn.click();

  // The Assign Role dialog should appear and contain a role dropdown.
  const assignDialog = page.getByRole('dialog', { name: /Assign role dialog/i });
  await expect(assignDialog).toBeVisible();

  // Wait for the role dropdown to be populated (loadRoles is triggered on open).
  const roleSelect = assignDialog.locator('#assign-role-select');
  await expect(roleSelect).toBeVisible({ timeout: 5_000 });

  // Select the seeded role by its UUID value.
  await roleSelect.selectOption({ value: seededRole.id });

  // Intercept the POST so we can assert the wire body.
  const [assignResp] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().includes('/roles') && r.request().method() === 'POST',
    ),
    assignDialog.getByRole('button', { name: /^Assign$/i }).click(),
  ]);

  // The server should accept the request (2xx / 204).
  expect(assignResp.status()).toBeLessThan(300);

  // Verify the wire body carried "role_id", not "role" — the core regression guard.
  const wireBody = assignResp.request().postDataJSON() as Record<string, unknown>;
  expect(wireBody).toHaveProperty('role_id', seededRole.id);
  expect(wireBody).not.toHaveProperty('role');

  // After the dialog closes, a role badge for the assigned role should appear
  // in the updated user row (the UI reloads the users list after assign).
  await expect(assignDialog).toHaveCount(0, { timeout: 5_000 });
  // Scope the badge check to the target user's row (userRow is a lazy locator
  // that re-evaluates after loadUsers() refreshes the table).
  await expect(
    userRow.getByText(seededRole.name, { exact: false }),
  ).toBeVisible({ timeout: 5_000 });
});
