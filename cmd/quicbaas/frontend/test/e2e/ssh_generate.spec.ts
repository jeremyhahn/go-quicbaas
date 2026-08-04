/**
 * ssh_generate.spec.ts
 *
 * Operational round-trip tests for Security > SSH Keys (#/ssh-keys).
 *
 * Graduates from the dialog-opens-only assertion in ssh_keys.spec.ts to a full
 * generate → verify-fingerprint → delete lifecycle. Also asserts that the
 * KnownHostsPanel component renders without error in both collapsed and expanded
 * states (a lightweight render sanity check for that sub-component).
 *
 * SSH subsystem gating: The SSH service is backed by go-xkms and may not be
 * configured in all deployments. When the view detects SSHUnavailableError it
 * renders a role="alert" banner ("SSH is not configured for this QuicBaaS
 * instance"). In that case the tests in the "generate" group skip the
 * round-trip assertions and instead assert the gated state deterministically.
 * This avoids producing a flaky test while still verifying that the UI handles
 * the unavailable state correctly. The KnownHostsPanel tests run regardless
 * because the panel silently swallows SSHUnavailableError (no error alert is
 * rendered; the panel just stays empty).
 *
 * Tests (run serially; state builds across tests):
 *  Group A — Generate Key round-trip (conditionally gated by SSH availability):
 *   1. SSH page renders — heading, scope tabs, Generate/Import buttons.
 *   2. Generate SSH key dialog — opens; name + algorithm fields present.
 *      Algorithm options: ed25519 | rsa-2048 | rsa-4096 | ecdsa-p256 | ecdsa-p384 | ecdsa-p521
 *   3. Key generation — POST /api/v1/ssh/keys returns 2xx; modal closes.
 *   4. New key row appears under "My Keys" tab with a non-empty fingerprint cell.
 *   5. Deletion — per-row Delete triggers confirm dialog; confirm removes the row.
 *
 *  Group B — KnownHostsPanel render sanity (always runs):
 *   6. KnownHostsPanel section is rendered (collapsed by default).
 *   7. Clicking the section header expands it; content area appears without error.
 *
 * Verified selectors:
 *  SSHKeysView.svelte (cmd/quicbaas/frontend/src/views/security/SSHKeysView.svelte):
 *   - Generate button:       aria-label="Generate SSH key"              (line 200)
 *   - Import button:         aria-label="Import SSH key"                (line 196)
 *   - SSH unavailable alert: role="alert" text /SSH is not configured/  (line 211)
 *   - Scope tab "My Keys":   role="tab" name /My Keys/                  (line 239)
 *   - SSH keys table:        table[aria-label="SSH keys"]               (line 289)
 *   - Key name cell:         td.font-mono.text-xs.font-medium           (line 302)
 *   - Fingerprint cell:      td.font-mono.text-xs (without font-medium) (line 303)
 *   - Delete button:         aria-label="Delete key {name}"             (line 338)
 *   - Delete dialog:         role="dialog" aria-label="Delete key confirmation" (line 393)
 *   - Confirm delete:        role="button" text="Delete" in dialog      (line 408)
 *
 *  GenerateKeyModal.svelte (cmd/quicbaas/frontend/src/views/security/ssh/GenerateKeyModal.svelte):
 *   - Generate dialog:    role="dialog" aria-label="Generate SSH key dialog"  (line 123)
 *   - Name input:         id="gen-key-name"                                   (line 139)
 *   - Algorithm select:   id="gen-key-algo"                                   (line 158)
 *   - Generate button:    aria-label="Generate key"                            (line 209)
 *   - Name validation:    slug RE /^[a-z0-9][a-z0-9-]*$/ enforced client-side (line 76)
 *
 *  KnownHostsPanel.svelte (cmd/quicbaas/frontend/src/views/security/ssh/KnownHostsPanel.svelte):
 *   - Section root:       .collapse.collapse-arrow with button.collapse-title  (line 195)
 *   - Collapse toggle:    button.collapse-title text "Known Hosts"              (line 197)
 *   - Content area:       .collapse-content (conditionally rendered when open)  (line 208)
 *
 * REST endpoints (pkg/api/rest/server.go):
 *  POST   /api/v1/ssh/keys                         → HandleGenerateSSHKey (line 529)
 *  GET    /api/v1/ssh/keys                         → HandleListSSHKeys    (line 531)
 *  DELETE /api/v1/ssh/keys/{scope}/{owner}/{name}  → HandleDeleteSSHKey   (line 533)
 *  GET    /api/v1/ssh/known-hosts                  → HandleListKnownHosts (line 538)
 */

import { test, expect, type Page } from '@playwright/test';
import { deleteSSHKey } from './fixtures/security_seed.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

async function gotoSSHKeys(page: Page): Promise<void> {
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/ssh-keys'; });
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });
}

// ---------------------------------------------------------------------------
// Shared test state
// ---------------------------------------------------------------------------

// Key name must match /^[a-z0-9][a-z0-9-]*$/ (GenerateKeyModal.svelte line 76).
// Use a timestamp-based suffix with a fixed prefix to satisfy the slug RE.
const KEY_NAME = `e2e-gen-${Date.now().toString().slice(-8)}`;
const KEY_ALGO = 'ed25519';

// Populated in test 3 from the API response; used in tests 4/5 and afterAll.
let createdKeyScope = 'user';
let createdKeyOwner = '';
let createdKeyName = '';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe.serial('Security > SSH Keys — generate/delete round-trip + KnownHostsPanel render', () => {

  test.afterAll(async () => {
    // deleteSSHKey swallows 404 and transport errors so afterAll never fails.
    if (createdKeyName) {
      await deleteSSHKey(createdKeyScope, createdKeyOwner, createdKeyName);
    }
  });

  // -------------------------------------------------------------------------
  // Test 1 — page renders correctly
  // -------------------------------------------------------------------------
  test('1. SSH Keys page renders heading, scope tabs, and action buttons', async ({ page }) => {
    await gotoSSHKeys(page);

    await expect(page.getByRole('heading', { name: 'SSH Keys' })).toBeVisible();

    // Scope tabs (SSHKeysView.svelte line 239).
    await expect(page.getByRole('tab', { name: /My Keys/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Tenant Keys/i })).toBeVisible();
    await expect(page.getByRole('tab', { name: /Service Keys/i })).toBeVisible();

    // Action buttons in the page header.
    await expect(page.getByRole('button', { name: /Generate SSH key/i })).toBeVisible();
    await expect(page.getByRole('button', { name: /Import SSH key/i })).toBeVisible();
  });

  // -------------------------------------------------------------------------
  // Test 2 — Generate SSH key dialog opens with correct fields
  // -------------------------------------------------------------------------
  test('2. Generate SSH key dialog opens with name + algorithm fields', async ({ page }) => {
    await gotoSSHKeys(page);

    // If SSH is unavailable the gated banner is shown; assert it and return.
    // The generate dialog will still open (the button is always rendered) but
    // submission will fail with SSHUnavailableError inside the modal.
    const unavailableBanner = page.locator('[role="alert"]', {
      hasText: /SSH is not configured/i,
    });
    if (await unavailableBanner.isVisible()) {
      // GATING: SSH subsystem not configured in this deployment.
      // Assert the gated state deterministically (not a timeout).
      await expect(unavailableBanner).toBeVisible();
      console.info('ssh_generate.spec.ts test 2: SSH unavailable — asserting gated banner only');
      return;
    }

    await page.getByRole('button', { name: /Generate SSH key/i }).click();

    const dialog = page.getByRole('dialog', { name: /Generate SSH key dialog/i });
    await expect(dialog).toBeVisible();

    // Name input (id="gen-key-name") and algorithm select (id="gen-key-algo").
    await expect(dialog.locator('#gen-key-name')).toBeVisible();
    await expect(dialog.locator('#gen-key-algo')).toBeVisible();

    // Algorithm options (GenerateKeyModal.svelte line 54-61).
    const algoSelect = dialog.locator('#gen-key-algo');
    await expect(algoSelect.locator('option[value="ed25519"]')).toHaveCount(1);
    await expect(algoSelect.locator('option[value="rsa-4096"]')).toHaveCount(1);
    await expect(algoSelect.locator('option[value="ecdsa-p256"]')).toHaveCount(1);

    // Close without generating.
    await dialog.getByRole('button', { name: /Cancel key generation/i }).click();
    await expect(dialog).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 3 — Generate key POST /api/v1/ssh/keys returns 2xx
  // -------------------------------------------------------------------------
  test('3. Generate SSH key POST /api/v1/ssh/keys returns 2xx and modal closes', async ({ page }) => {
    await gotoSSHKeys(page);

    const unavailableBanner = page.locator('[role="alert"]', {
      hasText: /SSH is not configured/i,
    });
    if (await unavailableBanner.isVisible()) {
      // GATING: SSH unavailable — assert the banner state and exit.
      await expect(unavailableBanner).toBeVisible();
      console.info('ssh_generate.spec.ts test 3: SSH unavailable — asserting gated banner only');
      return;
    }

    // Arm response interceptor before opening modal.
    const createResponse = page.waitForResponse(
      (resp) =>
        (resp.url().includes('/api/v1/ssh/keys') || resp.url().includes('/xkms/v1/ssh/keys')) &&
        resp.request().method() === 'POST' &&
        !resp.url().includes('/import') &&
        !resp.url().includes('/sign'),
    );

    await page.getByRole('button', { name: /Generate SSH key/i }).click();

    const dialog = page.getByRole('dialog', { name: /Generate SSH key dialog/i });
    await expect(dialog).toBeVisible();

    // Fill the name (slug RE: /^[a-z0-9][a-z0-9-]*$/).
    await dialog.locator('#gen-key-name').fill(KEY_NAME);
    await dialog.locator('#gen-key-algo').selectOption(KEY_ALGO);

    // Submit. Button has aria-label="Generate key" (GenerateKeyModal.svelte line 209),
    // which Playwright uses as the accessible name (takes priority over text content).
    await dialog.getByRole('button', { name: /Generate key/i }).click();

    const resp = await createResponse;
    expect(
      resp.status(),
      `POST /api/v1/ssh/keys expected 2xx, got ${resp.status()}`,
    ).toBeGreaterThanOrEqual(200);
    expect(resp.status()).toBeLessThan(300);

    // Parse response to capture scope/owner/name for cleanup and test 4.
    // Response shape (sdk/typescript/src/types.ts SSHKey): { name, scope?, owner?, algorithm, fingerprint, ... }
    try {
      const body = (await resp.json()) as {
        name?: string;
        scope?: string;
        owner?: string;
      };
      createdKeyName  = body.name  ?? KEY_NAME;
      createdKeyScope = body.scope ?? 'user';
      createdKeyOwner = body.owner ?? '';
    } catch {
      createdKeyName  = KEY_NAME;
      createdKeyScope = 'user';
      createdKeyOwner = '';
    }

    // Modal must close after successful creation.
    await expect(dialog).toHaveCount(0, { timeout: 5_000 });
  });

  // -------------------------------------------------------------------------
  // Test 4 — new key row appears in "My Keys" with a fingerprint
  // -------------------------------------------------------------------------
  test('4. generated key row appears in My Keys tab with a fingerprint', async ({ page }) => {
    await gotoSSHKeys(page);

    if (!createdKeyName) {
      // SSH was unavailable in test 3; no key was created; skip the list check.
      console.info('ssh_generate.spec.ts test 4: no key created (SSH unavailable) — skipping list assertion');
      return;
    }

    // Ensure "My Keys" tab is active (it is by default; click to be safe).
    await page.getByRole('tab', { name: /My Keys/i }).click();
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 8_000 });

    // SSH keys table (SSHKeysView.svelte line 289).
    const keysTable = page.locator('table[aria-label="SSH keys"]');
    await expect(keysTable).toBeVisible({ timeout: 8_000 });

    // Locate row by name cell: td.font-mono.text-xs.font-medium.
    const keyRow = keysTable.locator('tbody tr').filter({
      has: page.locator('td.font-mono.text-xs.font-medium', { hasText: createdKeyName }),
    });
    await expect(
      keyRow,
      `Expected SSH key row for "${createdKeyName}" in the My Keys tab`,
    ).toBeVisible({ timeout: 8_000 });

    // Fingerprint cell: td.font-mono.text-xs WITHOUT font-medium (line 303).
    // The cell renders truncated fingerprint text (non-empty for a real key).
    // BUG WATCH: if the fingerprint is "—" the server returned an empty fingerprint.
    const fingerprintCell = keyRow.locator('td.font-mono.text-xs:not(.font-medium)');
    const fingerprintText = (await fingerprintCell.textContent())?.trim() ?? '';
    expect(
      fingerprintText,
      `Fingerprint cell for key "${createdKeyName}" must not be empty or "—"`,
    ).not.toBe('—');
    expect(fingerprintText.length, 'Fingerprint must be non-empty').toBeGreaterThan(0);
  });

  // -------------------------------------------------------------------------
  // Test 5 — per-row Delete removes the key
  // -------------------------------------------------------------------------
  test('5. per-row Delete + confirm removes the SSH key row', async ({ page }) => {
    await gotoSSHKeys(page);

    if (!createdKeyName) {
      console.info('ssh_generate.spec.ts test 5: no key created (SSH unavailable) — skipping delete');
      return;
    }

    await page.getByRole('tab', { name: /My Keys/i }).click();
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 8_000 });

    const keysTable = page.locator('table[aria-label="SSH keys"]');
    await expect(keysTable).toBeVisible({ timeout: 8_000 });

    const keyRow = keysTable.locator('tbody tr').filter({
      has: page.locator('td.font-mono.text-xs.font-medium', { hasText: createdKeyName }),
    });
    await expect(keyRow).toBeVisible();

    // Click the per-row Delete button: aria-label="Delete key {name}".
    await keyRow.getByRole('button', { name: new RegExp(`Delete key ${createdKeyName}`) }).click();

    const confirmDialog = page.getByRole('dialog', { name: /Delete key confirmation/i });
    await expect(confirmDialog).toBeVisible();

    const deleteResponse = page.waitForResponse(
      (resp) =>
        (resp.url().includes('/api/v1/ssh/keys/') || resp.url().includes('/xkms/v1/ssh/keys/')) &&
        resp.request().method() === 'DELETE',
    );

    await confirmDialog.getByRole('button', { name: /^Delete$/ }).click();

    const delResp = await deleteResponse;
    expect(
      delResp.status(),
      `DELETE /api/v1/ssh/keys/{scope}/{owner}/{name} expected 2xx, got ${delResp.status()}`,
    ).toBeLessThan(300);

    await expect(keyRow).toHaveCount(0, { timeout: 5_000 });

    // Mark as cleaned up.
    createdKeyName = '';
  });

  // =========================================================================
  // Group B — KnownHostsPanel render sanity (always runs regardless of SSH)
  // =========================================================================

  // -------------------------------------------------------------------------
  // Test 6 — KnownHostsPanel is rendered (collapsed by default)
  // -------------------------------------------------------------------------
  test('6. KnownHostsPanel section renders in collapsed state without error', async ({ page }) => {
    await gotoSSHKeys(page);

    // The KnownHostsPanel is a daisyUI collapse section at the bottom of the page.
    // Root: div.collapse.collapse-arrow  (KnownHostsPanel.svelte line 195)
    // Toggle: button.collapse-title containing "Known Hosts"
    const collapseTrigger = page.locator('button.collapse-title', {
      hasText: /Known Hosts/i,
    });
    await expect(
      collapseTrigger,
      'KnownHostsPanel collapse trigger "Known Hosts" must be visible',
    ).toBeVisible({ timeout: 5_000 });

    // In collapsed state aria-expanded should be false (or absent).
    const expandedAttr = await collapseTrigger.getAttribute('aria-expanded');
    expect(
      expandedAttr === 'false' || expandedAttr === null,
      `Expected panel to be collapsed (aria-expanded false/absent), got "${expandedAttr}"`,
    ).toBe(true);

    // No error alert inside the panel wrapper.
    const panelRoot = page.locator('.collapse', { has: collapseTrigger });
    await expect(
      panelRoot.locator('[role="alert"]'),
    ).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Test 7 — KnownHostsPanel expands without error
  // -------------------------------------------------------------------------
  test('7. KnownHostsPanel expands and shows content without an error alert', async ({ page }) => {
    await gotoSSHKeys(page);

    const collapseTrigger = page.locator('button.collapse-title', {
      hasText: /Known Hosts/i,
    });
    await expect(collapseTrigger).toBeVisible({ timeout: 5_000 });

    // Expand the panel.
    await collapseTrigger.click();

    // After expansion aria-expanded should be true.
    await expect(collapseTrigger).toHaveAttribute('aria-expanded', 'true', { timeout: 3_000 });

    // The collapse-content div is conditionally rendered when open.
    const content = page.locator('.collapse-content');
    await expect(content).toBeVisible({ timeout: 5_000 });

    // Wait for any loading spinner inside the panel to clear.
    await expect(content.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 8_000 });

    // The panel must NOT render an error alert.
    // (SSHUnavailableError is silently swallowed; other errors DO surface here.)
    await expect(content.locator('[role="alert"]')).toHaveCount(0);

    // Content renders one of: spinner (cleared above), empty-state text, or a pins table.
    // At minimum, the "Add Pin" button must be present (it is always shown when open).
    await expect(
      content.getByRole('button', { name: /Add host pin/i }),
    ).toBeVisible({ timeout: 3_000 });
  });
});
