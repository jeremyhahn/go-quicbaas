<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  XKMSCAPanel is the Key Store page's certificate authority: the xkms CA,
  a service apart from the CA behind the Certificates page.

  Actions:
    - CA certificate: subject, issuer, serial, validity, is-CA and PEM, for
      an optional identity (loaded on mount)
    - CA bundle, with an optional store type and algorithm (loaded on a
      button press)
    - Issue dialog: profile, common name, organization, SANs, validity days,
      algorithm; the result shows the certificate, chain, serial and, once,
      the private key
    - Sign CSR dialog: CSR PEM, profile, validity days
    - Revoke dialog: serial and an RFC 5280 reason, after a confirmation

  Each call is bound to the session that started it: a response that lands
  after a sign-out or another login is dropped, and a session change clears
  every result, the issued private key included. Toasts name the serial
  only, never key material or PEM text.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type {
    CAClient,
    XKMSCABundle,
    XKMSCABundleOptions,
    XKMSCACertificate,
    XKMSCAIssueRequest,
    XKMSCAIssueResponse,
    XKMSCASignCSRRequest,
    XKMSCASignCSRResponse,
  } from '@quicbaas/sdk';

  const NOT_AVAILABLE = 'The certificate authority is not available on this connection.';
  const CSR_HEADER = '-----BEGIN CERTIFICATE REQUEST-----';

  /** The RFC 5280 CRLReason codes; 7 is unassigned. */
  const REVOKE_REASONS: { code: number; name: string }[] = [
    { code: 0, name: 'unspecified' },
    { code: 1, name: 'keyCompromise' },
    { code: 2, name: 'cACompromise' },
    { code: 3, name: 'affiliationChanged' },
    { code: 4, name: 'superseded' },
    { code: 5, name: 'cessationOfOperation' },
    { code: 6, name: 'certificateHold' },
    { code: 8, name: 'removeFromCRL' },
    { code: 9, name: 'privilegeWithdrawn' },
    { code: 10, name: 'aACompromise' },
  ];

  function errText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  /** Parses an optional positive whole number; '' is undefined, a bad value null. */
  function optionalDays(raw: string): number | undefined | null {
    const v = raw.trim();
    if (v === '') return undefined;
    const n = Number(v);
    return Number.isInteger(n) && n > 0 ? n : null;
  }

  let unavailable = $state(false);

  /** Resolves the CA client, or null when this connection has none. */
  async function caClient(): Promise<CAClient | null> {
    const client = await getSDKClient();
    return client.ca ?? null;
  }

  /** Records an error; an unsupported transport becomes the not-available state. */
  function failed(e: unknown, fallback: string, set: (msg: string) => void): void {
    if (e instanceof TransportNotSupportedError) unavailable = true;
    else set(errText(e, fallback));
  }

  // ---------------------------------------------------------------------------
  // CA certificate
  // ---------------------------------------------------------------------------

  let identity = $state('');
  let caCert = $state<XKMSCACertificate | null>(null);
  let caError = $state<string | null>(null);
  let caLoading = $state(false);
  let caSeq = 0;

  async function loadCACert(): Promise<void> {
    const forSession = authStore.sessionKey;
    const seq = ++caSeq;
    const id = identity.trim();
    caLoading = true;
    caError = null;
    try {
      const ca = await caClient();
      if (seq !== caSeq || forSession !== authStore.sessionKey) return;
      if (!ca) { unavailable = true; return; }
      unavailable = false;
      const res = await ca.xkmsGetCACertificate(id === '' ? undefined : id);
      if (seq !== caSeq || forSession !== authStore.sessionKey) return;
      caCert = res;
    } catch (e) {
      if (seq !== caSeq || forSession !== authStore.sessionKey) return;
      caCert = null;
      failed(e, 'Failed to load the CA certificate', (m) => { caError = m; });
    } finally {
      if (seq === caSeq && forSession === authStore.sessionKey) caLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // CA bundle
  // ---------------------------------------------------------------------------

  let storeType = $state('');
  let bundleAlgorithm = $state('');
  let bundle = $state<XKMSCABundle | null>(null);
  let bundleError = $state<string | null>(null);
  let bundleLoading = $state(false);
  let bundleSeq = 0;

  async function loadBundle(): Promise<void> {
    const forSession = authStore.sessionKey;
    const seq = ++bundleSeq;
    const opts: XKMSCABundleOptions = {};
    const st = storeType.trim();
    const alg = bundleAlgorithm.trim();
    if (st !== '') opts.store_type = st;
    if (alg !== '') opts.algorithm = alg;
    bundleLoading = true;
    bundleError = null;
    try {
      const ca = await caClient();
      if (seq !== bundleSeq || forSession !== authStore.sessionKey) return;
      if (!ca) { unavailable = true; return; }
      const res = await ca.xkmsGetCABundle(opts);
      if (seq !== bundleSeq || forSession !== authStore.sessionKey) return;
      bundle = res;
    } catch (e) {
      if (seq !== bundleSeq || forSession !== authStore.sessionKey) return;
      bundle = null;
      failed(e, 'Failed to load the CA bundle', (m) => { bundleError = m; });
    } finally {
      if (seq === bundleSeq && forSession === authStore.sessionKey) bundleLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Issue
  // ---------------------------------------------------------------------------

  let showIssue = $state(false);
  let issueProfile = $state('');
  let issueCommonName = $state('');
  let issueOrganization = $state('');
  let issueSans = $state('');
  let issueDays = $state('');
  let issueAlgorithm = $state('');
  let issueError = $state<string | null>(null);
  let issuing = $state(false);
  /** The issue result, the private key included; it lives only while its dialog is open. */
  let issued = $state<XKMSCAIssueResponse | null>(null);

  function openIssue(): void {
    issueProfile = ''; issueCommonName = ''; issueOrganization = '';
    issueSans = ''; issueDays = ''; issueAlgorithm = '';
    issueError = null;
    showIssue = true;
  }

  async function submitIssue(): Promise<void> {
    const profile = issueProfile.trim();
    const commonName = issueCommonName.trim();
    if (profile === '') { issueError = 'Profile is required.'; return; }
    if (commonName === '') { issueError = 'Common name is required.'; return; }
    const days = optionalDays(issueDays);
    if (days === null) { issueError = 'Validity days must be a positive whole number.'; return; }
    const req: XKMSCAIssueRequest = { profile, common_name: commonName };
    const org = issueOrganization.trim();
    if (org !== '') req.organization = org;
    const sans = issueSans.split(',').map((s) => s.trim()).filter((s) => s !== '');
    if (sans.length > 0) req.sans = sans;
    if (days !== undefined) req.validity_days = days;
    const alg = issueAlgorithm.trim();
    if (alg !== '') req.algorithm = alg;
    const forSession = authStore.sessionKey;
    issueError = null;
    issuing = true;
    try {
      const ca = await caClient();
      if (forSession !== authStore.sessionKey) return;
      if (!ca) { issueError = NOT_AVAILABLE; return; }
      const res = await ca.xkmsIssueCertificate(req);
      if (forSession !== authStore.sessionKey) return;
      showIssue = false;
      issued = res;
      showToast({ message: `Certificate ${res.serial_number} issued`, type: 'success' });
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      issueError = errText(e, 'Failed to issue the certificate');
    } finally {
      issuing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Sign CSR
  // ---------------------------------------------------------------------------

  let showSign = $state(false);
  let signCsr = $state('');
  let signProfile = $state('');
  let signDays = $state('');
  let signError = $state<string | null>(null);
  let signing = $state(false);
  let signed = $state<XKMSCASignCSRResponse | null>(null);

  function openSign(): void {
    signCsr = ''; signProfile = ''; signDays = '';
    signError = null;
    showSign = true;
  }

  async function submitSign(): Promise<void> {
    const csr = signCsr.trim();
    if (!csr.includes(CSR_HEADER)) { signError = `Enter a PEM certificate request (${CSR_HEADER}).`; return; }
    const days = optionalDays(signDays);
    if (days === null) { signError = 'Validity days must be a positive whole number.'; return; }
    const req: XKMSCASignCSRRequest = { csr_pem: csr };
    const profile = signProfile.trim();
    if (profile !== '') req.profile = profile;
    if (days !== undefined) req.validity_days = days;
    const forSession = authStore.sessionKey;
    signError = null;
    signing = true;
    try {
      const ca = await caClient();
      if (forSession !== authStore.sessionKey) return;
      if (!ca) { signError = NOT_AVAILABLE; return; }
      const res = await ca.xkmsSignCSR(req);
      if (forSession !== authStore.sessionKey) return;
      showSign = false;
      signCsr = '';
      signed = res;
      showToast({ message: `Certificate ${res.serial_number} signed`, type: 'success' });
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      signError = errText(e, 'Failed to sign the CSR');
    } finally {
      signing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Revoke
  // ---------------------------------------------------------------------------

  let showRevoke = $state(false);
  let revokeSerial = $state('');
  let revokeReason = $state(0);
  let revokeConfirm = $state(false);
  let revokeError = $state<string | null>(null);
  let revoking = $state(false);

  function openRevoke(): void {
    revokeSerial = ''; revokeReason = 0;
    revokeConfirm = false; revokeError = null;
    showRevoke = true;
  }

  function closeRevoke(): void {
    showRevoke = false;
    revokeConfirm = false;
    revokeError = null;
  }

  function askRevoke(): void {
    if (revokeSerial.trim() === '') { revokeError = 'Serial number is required.'; return; }
    revokeError = null;
    revokeConfirm = true;
  }

  async function runRevoke(): Promise<void> {
    const serial = revokeSerial.trim();
    const reason = Number(revokeReason);
    const forSession = authStore.sessionKey;
    revokeError = null;
    revoking = true;
    try {
      const ca = await caClient();
      if (forSession !== authStore.sessionKey) return;
      if (!ca) { revokeError = NOT_AVAILABLE; return; }
      const res = await ca.xkmsRevokeCertificate(serial, reason);
      if (forSession !== authStore.sessionKey) return;
      if (!res.success) {
        revokeConfirm = false;
        revokeError = res.message || 'The revoke was refused.';
        return;
      }
      closeRevoke();
      showToast({ message: `Certificate ${serial} revoked`, type: 'success' });
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      revokeConfirm = false;
      revokeError = errText(e, 'Failed to revoke the certificate');
    } finally {
      revoking = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Session
  // ---------------------------------------------------------------------------

  function resetAll(): void {
    caCert = null; caError = null;
    bundle = null; bundleError = null;
    showIssue = false; issueError = null; issued = null;
    showSign = false; signError = null; signCsr = ''; signed = null;
    closeRevoke();
  }

  $effect(() => { untrack(() => { void loadCACert(); }); });

  // A change of session identity drops what the previous session saw, the
  // issued private key included, and loads the CA certificate again.
  let lastSession = authStore.sessionKey;
  $effect(() => {
    const key = authStore.sessionKey;
    if (key === lastSession) return;
    lastSession = key;
    untrack(() => {
      resetAll();
      void loadCACert();
    });
  });
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h2 class="text-base font-semibold">Certificate Authority</h2>
    {#if !unavailable}
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-primary btn-sm" onclick={openIssue} aria-label="Issue certificate">Issue</button>
        <button class="btn btn-sm" onclick={openSign} aria-label="Sign CSR">Sign CSR</button>
        <button class="btn btn-error btn-outline btn-sm" onclick={openRevoke} aria-label="Revoke certificate">Revoke</button>
      </div>
    {/if}
  </div>

  {#if unavailable}
    <div class="alert alert-info text-sm" data-testid="xkms-ca-not-available">{NOT_AVAILABLE}</div>
  {:else}
    <div class="card bg-base-200 p-4 space-y-3">
      <div class="flex flex-wrap items-end gap-2">
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-identity">Identity (optional)</label>
          <input id="xkms-ca-identity" class="input input-bordered input-sm" type="text" bind:value={identity} />
        </div>
        <button class="btn btn-sm" disabled={caLoading} onclick={() => void loadCACert()} aria-label="Load CA certificate">
          {caLoading ? 'Loading…' : 'Load'}
        </button>
      </div>

      {#if caError}
        <div class="alert alert-error text-sm" role="alert" data-testid="ca-cert-error">{caError}</div>
      {:else if caCert}
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
          <dt class="font-medium">Subject</dt><dd class="font-mono">{caCert.subject}</dd>
          <dt class="font-medium">Issuer</dt><dd class="font-mono">{caCert.issuer}</dd>
          <dt class="font-medium">Serial</dt><dd class="font-mono">{caCert.serial_number}</dd>
          <dt class="font-medium">Valid</dt><dd class="font-mono">{caCert.not_before} – {caCert.not_after}</dd>
          <dt class="font-medium">CA</dt><dd data-testid="ca-is-ca">{caCert.is_ca ? 'yes' : 'no'}</dd>
        </dl>
        <pre class="bg-base-100 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="ca-cert-pem">{caCert.certificate_pem}</pre>
      {:else if caLoading}
        <span class="loading loading-spinner loading-sm"></span>
      {/if}
    </div>

    <div class="card bg-base-200 p-4 space-y-3">
      <div class="flex flex-wrap items-end gap-2">
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-store-type">Store type (optional)</label>
          <input id="xkms-ca-store-type" class="input input-bordered input-sm" type="text" bind:value={storeType} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-bundle-alg">Bundle algorithm (optional)</label>
          <input id="xkms-ca-bundle-alg" class="input input-bordered input-sm" type="text" bind:value={bundleAlgorithm} />
        </div>
        <button class="btn btn-sm" disabled={bundleLoading} onclick={() => void loadBundle()} aria-label="Get CA bundle">
          {bundleLoading ? 'Loading…' : 'Get bundle'}
        </button>
      </div>

      {#if bundleError}
        <div class="alert alert-error text-sm" role="alert" data-testid="ca-bundle-error">{bundleError}</div>
      {:else if bundle}
        <div class="text-xs text-base-content/60">
          {bundle.certificates?.length ?? 0} certificate(s){bundle.content_type ? `, ${bundle.content_type}` : ''}
        </div>
        <pre class="bg-base-100 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="ca-bundle-pem">{bundle.bundle_pem}</pre>
      {/if}
    </div>
  {/if}
</div>

{#if showIssue}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Issue certificate dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Issue Certificate</h3>

      {#if issueError}
        <div class="alert alert-error text-sm mb-3" role="alert">{issueError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-issue-profile">Profile</label>
          <input id="xkms-ca-issue-profile" class="input input-bordered input-sm" type="text" bind:value={issueProfile} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-issue-cn">Common name</label>
          <input id="xkms-ca-issue-cn" class="input input-bordered input-sm" type="text" bind:value={issueCommonName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-issue-org">Organization (optional)</label>
          <input id="xkms-ca-issue-org" class="input input-bordered input-sm" type="text" bind:value={issueOrganization} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-issue-sans">SANs (comma-separated)</label>
          <input id="xkms-ca-issue-sans" class="input input-bordered input-sm" type="text" bind:value={issueSans} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-issue-days">Validity days (optional)</label>
          <input id="xkms-ca-issue-days" class="input input-bordered input-sm" type="text" inputmode="numeric" bind:value={issueDays} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-issue-alg">Algorithm (optional)</label>
          <input id="xkms-ca-issue-alg" class="input input-bordered input-sm" type="text" bind:value={issueAlgorithm} />
        </div>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showIssue = false; issueError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={issuing} onclick={() => void submitIssue()}>
          {issuing ? 'Issuing…' : 'Issue'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showIssue = false; }} onkeydown={() => { showIssue = false; }}></div>
  </div>
{/if}

{#if issued}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Issued certificate dialog">
    <div class="modal-box max-w-2xl space-y-3">
      <h3 class="text-base font-semibold">Issued certificate <span class="font-mono">{issued.serial_number}</span></h3>
      <div>
        <div class="text-xs font-medium mb-1">Certificate</div>
        <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="issued-cert-pem">{issued.certificate_pem}</pre>
      </div>
      {#if issued.chain_pem}
        <div>
          <div class="text-xs font-medium mb-1">Chain</div>
          <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="issued-chain-pem">{issued.chain_pem}</pre>
        </div>
      {/if}
      {#if issued.private_key_pem}
        <div>
          <div class="alert alert-warning text-xs mb-1">The private key is shown once. Copy it now: it is gone when this dialog closes.</div>
          <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="issued-private-key">{issued.private_key_pem}</pre>
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={() => { issued = null; }}>Close</button>
      </div>
    </div>
  </div>
{/if}

{#if showSign}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Sign CSR dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Sign CSR</h3>

      {#if signError}
        <div class="alert alert-error text-sm mb-3" role="alert">{signError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-sign-csr">CSR PEM</label>
          <textarea
            id="xkms-ca-sign-csr"
            class="textarea textarea-bordered font-mono text-xs h-40"
            bind:value={signCsr}
            placeholder={CSR_HEADER}
          ></textarea>
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-sign-profile">Profile (optional)</label>
          <input id="xkms-ca-sign-profile" class="input input-bordered input-sm" type="text" bind:value={signProfile} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-sign-days">Validity days (optional)</label>
          <input id="xkms-ca-sign-days" class="input input-bordered input-sm" type="text" inputmode="numeric" bind:value={signDays} />
        </div>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showSign = false; signError = null; signCsr = ''; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={signing} onclick={() => void submitSign()}>
          {signing ? 'Signing…' : 'Sign'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showSign = false; }} onkeydown={() => { showSign = false; }}></div>
  </div>
{/if}

{#if signed}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Signed certificate dialog">
    <div class="modal-box max-w-2xl space-y-3">
      <h3 class="text-base font-semibold">Signed certificate <span class="font-mono">{signed.serial_number}</span></h3>
      <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="signed-cert-pem">{signed.certificate_pem}</pre>
      {#if signed.chain_pem}
        <div>
          <div class="text-xs font-medium mb-1">Chain</div>
          <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all">{signed.chain_pem}</pre>
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={() => { signed = null; }}>Close</button>
      </div>
    </div>
  </div>
{/if}

{#if showRevoke}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Revoke certificate dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Revoke Certificate</h3>

      {#if revokeError}
        <div class="alert alert-error text-sm mb-3" role="alert">{revokeError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-revoke-serial">Serial number</label>
          <input
            id="xkms-ca-revoke-serial"
            class="input input-bordered input-sm font-mono"
            type="text"
            disabled={revokeConfirm}
            bind:value={revokeSerial}
          />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-ca-revoke-reason">Reason</label>
          <select
            id="xkms-ca-revoke-reason"
            class="select select-bordered select-sm"
            disabled={revokeConfirm}
            bind:value={revokeReason}
          >
            {#each REVOKE_REASONS as r (r.code)}<option value={r.code}>{r.code} {r.name}</option>{/each}
          </select>
        </div>
        {#if revokeConfirm}
          <p class="text-sm text-error">
            Revoke certificate <span class="font-mono">{revokeSerial.trim()}</span>? A revoked certificate cannot be restored.
          </p>
        {/if}
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeRevoke}>Cancel</button>
        {#if revokeConfirm}
          <button class="btn btn-error btn-sm" disabled={revoking} onclick={() => void runRevoke()}>
            {revoking ? 'Revoking…' : 'Confirm revoke'}
          </button>
        {:else}
          <button class="btn btn-error btn-sm" onclick={askRevoke}>Revoke</button>
        {/if}
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeRevoke} onkeydown={closeRevoke}></div>
  </div>
{/if}
