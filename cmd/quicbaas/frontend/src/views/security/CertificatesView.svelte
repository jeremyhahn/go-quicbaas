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
  CertificatesView renders an operator console for the Certificate Authority.
  Equivalent to `quicbaas ca info|bundle|list|issue|revoke|export-bundle` plus
  sign-csr, verify, and CRL generation, none of which the CLI's `ca` command
  group exposes but the underlying SDK does.

  Actions:
    - CA info panel (subject, issuer, serial, validity, key type, algorithm)
    - Certificate list with common-name / revoked / expired filters
    - Issue Certificate dialog (mints a new leaf/CA cert + private key)
    - Sign CSR dialog (signs a caller-supplied CSR)
    - Revoke per row (serial-number confirmation)
    - Verify certificate / check revocation status utilities
    - Download CA bundle / generate + download CRL
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import type {
    CAInfoResponse,
    CertificateSummary,
    IssueCertificateRequest,
    IssueCertificateResponse,
    SignCSRResponse,
    CertificateUsage,
    CertificateKeyAlgorithm,
  } from '@quicbaas/sdk';
  import {
    FileBadge,
    Plus,
    Upload,
    Ban,
    ScanLine,
    Download,
    RefreshCw,
    Copy,
    Search,
  } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Shared helpers
  // ---------------------------------------------------------------------------

  function downloadText(filename: string, content: string, mime = 'application/x-pem-file'): void {
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  async function copyToClipboard(text: string, label: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', `${label} copied to clipboard`);
    } catch {
      showToast('error', `Failed to copy ${label}`);
    }
  }

  function isExpired(notAfter: string): boolean {
    const t = Date.parse(notAfter);
    return Number.isFinite(t) && t < Date.now();
  }

  // ---------------------------------------------------------------------------
  // CA info panel
  // ---------------------------------------------------------------------------

  let caInfo = $state<CAInfoResponse | null>(null);
  let caInfoError = $state('');
  let caInfoLoading = $state(false);

  async function loadCAInfo(): Promise<void> {
    caInfoLoading = true;
    caInfoError = '';
    try {
      const client = await getSDKClient();
      caInfo = await client.ca.caInfo();
    } catch (err: unknown) {
      caInfoError = err instanceof Error ? err.message : String(err);
    } finally {
      caInfoLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Certificate list
  // ---------------------------------------------------------------------------

  let certificates = $state<CertificateSummary[]>([]);
  let totalCount = $state(0);
  let listLoading = $state(false);
  let listError = $state('');
  let filterCommonName = $state('');
  let filterIncludeRevoked = $state(false);
  let filterIncludeExpired = $state(true);

  async function loadCertificates(): Promise<void> {
    listLoading = true;
    listError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.ca.listCertificates({
        common_name: filterCommonName.trim() || undefined,
        include_revoked: filterIncludeRevoked,
        include_expired: filterIncludeExpired,
        limit: 200,
      });
      certificates = resp.certificates;
      totalCount = resp.total_count;
    } catch (err: unknown) {
      listError = err instanceof Error ? err.message : String(err);
    } finally {
      listLoading = false;
    }
  }

  async function loadAll(): Promise<void> {
    await Promise.all([loadCAInfo(), loadCertificates()]);
  }

  $effect(() => { void loadAll(); });

  // ---------------------------------------------------------------------------
  // Issue certificate dialog
  // ---------------------------------------------------------------------------

  let showIssueDialog = $state(false);
  let issuing = $state(false);
  let issueError = $state('');
  let issueResult = $state<IssueCertificateResponse | null>(null);

  let issueCommonName = $state('');
  let issueOrganization = $state('');
  let issueOU = $state('');
  let issueCountry = $state('');
  let issueDNSNames = $state('');
  let issueIPAddresses = $state('');
  let issueEmailAddresses = $state('');
  let issueValidityDays = $state(365);
  let issueUsage = $state<CertificateUsage>('server');
  let issueIsCA = $state(false);
  let issueKeyAlgorithm = $state<CertificateKeyAlgorithm>('ecdsa');
  let issueKeySize = $state(0);

  function openIssueDialog(): void {
    showIssueDialog = true;
    issueError = '';
    issueCommonName = '';
    issueOrganization = '';
    issueOU = '';
    issueCountry = '';
    issueDNSNames = '';
    issueIPAddresses = '';
    issueEmailAddresses = '';
    issueValidityDays = 365;
    issueUsage = 'server';
    issueIsCA = false;
    issueKeyAlgorithm = 'ecdsa';
    issueKeySize = 0;
  }

  function splitCSV(v: string): string[] | undefined {
    const parts = v.split(',').map((s) => s.trim()).filter(Boolean);
    return parts.length > 0 ? parts : undefined;
  }

  async function submitIssue(): Promise<void> {
    if (!issueCommonName.trim()) {
      issueError = 'Common name is required.';
      return;
    }
    issuing = true;
    issueError = '';
    try {
      const client = await getSDKClient();
      const req: IssueCertificateRequest = {
        common_name: issueCommonName.trim(),
        organization: issueOrganization.trim() || undefined,
        organizational_unit: issueOU.trim() || undefined,
        country: issueCountry.trim() || undefined,
        dns_names: splitCSV(issueDNSNames),
        ip_addresses: splitCSV(issueIPAddresses),
        email_addresses: splitCSV(issueEmailAddresses),
        validity_days: issueValidityDays > 0 ? issueValidityDays : undefined,
        usage: issueUsage,
        is_ca: issueIsCA,
        key_algorithm: issueKeyAlgorithm,
        key_size: issueKeySize > 0 ? issueKeySize : undefined,
      };
      issueResult = await client.ca.issueCertificate(req);
      showIssueDialog = false;
      showToast('success', `Certificate issued for "${issueCommonName.trim()}"`);
      await loadCertificates();
    } catch (err: unknown) {
      issueError = err instanceof Error ? err.message : String(err);
    } finally {
      issuing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Sign CSR dialog
  // ---------------------------------------------------------------------------

  let showSignCSRDialog = $state(false);
  let signing = $state(false);
  let signError = $state('');
  let signResult = $state<SignCSRResponse | null>(null);
  let csrPEM = $state('');
  let signValidityDays = $state(365);
  let signUsage = $state<CertificateUsage>('server');

  function openSignCSRDialog(): void {
    showSignCSRDialog = true;
    signError = '';
    csrPEM = '';
    signValidityDays = 365;
    signUsage = 'server';
  }

  async function submitSignCSR(): Promise<void> {
    if (!csrPEM.trim()) {
      signError = 'CSR PEM is required.';
      return;
    }
    signing = true;
    signError = '';
    try {
      const client = await getSDKClient();
      signResult = await client.ca.signCSR({
        csr_pem: csrPEM.trim(),
        validity_days: signValidityDays > 0 ? signValidityDays : undefined,
        usage: signUsage,
      });
      showSignCSRDialog = false;
      showToast('success', 'CSR signed');
      await loadCertificates();
    } catch (err: unknown) {
      signError = err instanceof Error ? err.message : String(err);
    } finally {
      signing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Revoke certificate
  // ---------------------------------------------------------------------------

  let revokeTarget = $state<CertificateSummary | null>(null);
  let revoking = $state(false);

  async function confirmRevoke(): Promise<void> {
    if (!revokeTarget) return;
    revoking = true;
    try {
      const client = await getSDKClient();
      await client.ca.revokeCertificate(revokeTarget.serial_number);
      showToast('success', `Certificate ${revokeTarget.serial_number.slice(0, 12)}… revoked`);
      revokeTarget = null;
      await loadCertificates();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      revoking = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Verify certificate utility
  // ---------------------------------------------------------------------------

  let showVerifyDialog = $state(false);
  let verifyPEM = $state('');
  let verifying = $state(false);
  let verifyResult = $state<{ valid: boolean; error_message?: string } | null>(null);

  async function submitVerify(): Promise<void> {
    if (!verifyPEM.trim()) return;
    verifying = true;
    verifyResult = null;
    try {
      const client = await getSDKClient();
      verifyResult = await client.ca.verifyCertificate(verifyPEM.trim());
    } catch (err: unknown) {
      verifyResult = { valid: false, error_message: err instanceof Error ? err.message : String(err) };
    } finally {
      verifying = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Check revocation status utility
  // ---------------------------------------------------------------------------

  let showCheckRevokedDialog = $state(false);
  let checkSerial = $state('');
  let checking = $state(false);
  let checkResult = $state<{ revoked: boolean; message?: string } | null>(null);

  async function submitCheckRevoked(): Promise<void> {
    if (!checkSerial.trim()) return;
    checking = true;
    checkResult = null;
    try {
      const client = await getSDKClient();
      checkResult = await client.ca.isRevoked(checkSerial.trim());
    } catch (err: unknown) {
      checkResult = { revoked: false, message: err instanceof Error ? err.message : String(err) };
    } finally {
      checking = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Bundle download / CRL generation
  // ---------------------------------------------------------------------------

  let downloadingBundle = $state(false);
  let generatingCRL = $state(false);

  async function downloadBundle(): Promise<void> {
    downloadingBundle = true;
    try {
      const client = await getSDKClient();
      const resp = await client.ca.getCABundle();
      downloadText('ca-bundle.pem', resp.bundle_pem);
      showToast('success', 'CA bundle downloaded');
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      downloadingBundle = false;
    }
  }

  async function generateCRL(): Promise<void> {
    generatingCRL = true;
    try {
      const client = await getSDKClient();
      const resp = await client.ca.generateCRL();
      downloadText('ca.crl.pem', resp.crl_pem);
      showToast('success', 'CRL generated and downloaded');
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      generatingCRL = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Certificates" subtitle="Certificate Authority — Issue, Sign, Revoke, and Audit" icon={FileBadge}>
    {#snippet children()}
      <button class="btn btn-ghost btn-sm gap-1" onclick={() => void loadAll()} disabled={listLoading || caInfoLoading} aria-label="Refresh">
        <RefreshCw class="h-3.5 w-3.5 {(listLoading || caInfoLoading) ? 'animate-spin' : ''}" />
        Refresh
      </button>
      <button class="btn btn-ghost btn-sm gap-1" onclick={() => void downloadBundle()} disabled={downloadingBundle} aria-label="Download CA bundle">
        <Download class="h-3.5 w-3.5" />
        Bundle
      </button>
      <button class="btn btn-ghost btn-sm gap-1" onclick={() => void generateCRL()} disabled={generatingCRL} aria-label="Generate CRL">
        <Download class="h-3.5 w-3.5" />
        CRL
      </button>
      <button class="btn btn-ghost btn-sm gap-1" onclick={openSignCSRDialog} aria-label="Sign CSR">
        <Upload class="h-3.5 w-3.5" />
        Sign CSR
      </button>
      <button class="btn btn-primary btn-sm gap-1" onclick={openIssueDialog} aria-label="Issue certificate">
        <Plus class="h-3.5 w-3.5" />
        Issue Certificate
      </button>
    {/snippet}
  </PageHeader>

  <!-- CA Info panel -->
  <div class="card bg-base-200 rounded-box p-4">
    <h3 class="text-sm font-semibold mb-3">CA Info</h3>
    {#if caInfoError}
      <div class="alert alert-error text-sm" role="alert">{caInfoError}</div>
    {:else if caInfoLoading && !caInfo}
      <div class="skeleton h-16 w-full rounded-box"></div>
    {:else if caInfo}
      <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-xs">
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Common Name</div>
          <div class="font-mono">{caInfo.common_name || '—'}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Serial</div>
          <div class="font-mono truncate" title={caInfo.serial_number}>{caInfo.serial_number}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Key</div>
          <div class="font-mono">{caInfo.key_type}{caInfo.key_bits ? ` / ${caInfo.key_bits} bits` : ''}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Signature Algorithm</div>
          <div class="font-mono">{caInfo.signature_algorithm}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Subject</div>
          <div class="font-mono truncate" title={caInfo.subject}>{caInfo.subject}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Issuer</div>
          <div class="font-mono truncate" title={caInfo.issuer}>{caInfo.issuer}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Not Before</div>
          <div>{formatDateTime(caInfo.not_before)}</div>
        </div>
        <div>
          <div class="text-base-content/40 uppercase tracking-wide mb-0.5">Not After</div>
          <div>{formatDateTime(caInfo.not_after)}</div>
        </div>
      </div>
    {/if}
  </div>

  <!-- Utilities: verify certificate / check revocation -->
  <div class="flex flex-wrap gap-2">
    <button class="btn btn-ghost btn-xs gap-1" onclick={() => { showVerifyDialog = true; verifyPEM = ''; verifyResult = null; }}>
      <ScanLine class="h-3 w-3" />
      Verify Certificate
    </button>
    <button class="btn btn-ghost btn-xs gap-1" onclick={() => { showCheckRevokedDialog = true; checkSerial = ''; checkResult = null; }}>
      <Search class="h-3 w-3" />
      Check Revocation Status
    </button>
  </div>

  <!-- Filters -->
  <div class="flex flex-wrap items-end gap-3">
    <div class="form-control">
      <label class="label label-text text-xs" for="cert-filter-cn">Common Name Contains</label>
      <input
        id="cert-filter-cn"
        class="input input-bordered input-sm"
        type="text"
        placeholder="example.com"
        bind:value={filterCommonName}
        onkeydown={(e) => { if (e.key === 'Enter') void loadCertificates(); }}
      />
    </div>
    <label class="flex items-center gap-1 text-xs cursor-pointer pb-2">
      <input type="checkbox" class="checkbox checkbox-xs" bind:checked={filterIncludeRevoked} />
      Include revoked
    </label>
    <label class="flex items-center gap-1 text-xs cursor-pointer pb-2">
      <input type="checkbox" class="checkbox checkbox-xs" bind:checked={filterIncludeExpired} />
      Include expired
    </label>
    <button class="btn btn-sm btn-outline" onclick={() => void loadCertificates()}>Apply Filters</button>
  </div>

  {#if listError}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{listError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => void loadCertificates()}>Retry</button>
    </div>
  {/if}

  {#if listLoading && certificates.length === 0}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if certificates.length === 0}
    <EmptyState
      icon={FileBadge}
      title="No certificates found"
      description="Issue a certificate or adjust the filters above to see existing ones."
      actionLabel="Issue Certificate"
      onaction={openIssueDialog}
    />
  {:else}
    <div class="text-xs text-base-content/40">{totalCount} total</div>
    <div class="overflow-x-auto rounded-box border border-base-300/50">
      <table class="table table-sm table-zebra">
        <thead>
          <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
            <th>Common Name</th>
            <th>Serial</th>
            <th>Not Before</th>
            <th>Not After</th>
            <th>Type</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each certificates as cert (cert.serial_number)}
            <tr class="hover">
              <td class="font-mono text-xs">{cert.common_name}</td>
              <td class="font-mono text-xs truncate max-w-[10rem]" title={cert.serial_number}>{cert.serial_number}</td>
              <td class="text-xs text-base-content/50">{formatDateTime(cert.not_before)}</td>
              <td class="text-xs text-base-content/50">{formatDateTime(cert.not_after)}</td>
              <td>
                <span class="badge badge-xs {cert.is_ca ? 'badge-primary' : 'badge-ghost'}">{cert.is_ca ? 'CA' : 'Leaf'}</span>
                {#if isExpired(cert.not_after)}
                  <span class="badge badge-xs badge-warning ml-1">expired</span>
                {/if}
              </td>
              <td>
                <div class="flex gap-1">
                  <div class="tooltip tooltip-left" data-tip="Revoke">
                    <button
                      class="btn btn-ghost btn-xs text-error"
                      onclick={() => { revokeTarget = cert; }}
                      aria-label={`Revoke certificate ${cert.common_name}`}
                    >
                      <Ban class="h-3 w-3" />
                    </button>
                  </div>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- =========================================================================
     ISSUE CERTIFICATE DIALOG
     ========================================================================= -->
{#if showIssueDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Issue certificate dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Issue Certificate</h3>

      {#if issueError}
        <div class="alert alert-error text-sm mb-3" role="alert">{issueError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="issue-cn">Common Name <span class="text-error">*</span></label>
          <input id="issue-cn" class="input input-bordered input-sm" type="text" placeholder="app.example.com" bind:value={issueCommonName} />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="form-control">
            <label class="label label-text" for="issue-org">Organization</label>
            <input id="issue-org" class="input input-bordered input-sm" type="text" bind:value={issueOrganization} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="issue-ou">Organizational Unit</label>
            <input id="issue-ou" class="input input-bordered input-sm" type="text" bind:value={issueOU} />
          </div>
        </div>
        <div class="form-control">
          <label class="label label-text" for="issue-country">Country</label>
          <input id="issue-country" class="input input-bordered input-sm" type="text" placeholder="US" bind:value={issueCountry} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="issue-dns">DNS Names (comma-separated)</label>
          <input id="issue-dns" class="input input-bordered input-sm font-mono" type="text" placeholder="app.example.com, *.app.example.com" bind:value={issueDNSNames} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="issue-ip">IP Addresses (comma-separated)</label>
          <input id="issue-ip" class="input input-bordered input-sm font-mono" type="text" placeholder="10.0.0.1" bind:value={issueIPAddresses} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="issue-email">Email Addresses (comma-separated)</label>
          <input id="issue-email" class="input input-bordered input-sm font-mono" type="text" bind:value={issueEmailAddresses} />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="form-control">
            <label class="label label-text" for="issue-validity">Validity (days)</label>
            <input id="issue-validity" class="input input-bordered input-sm" type="number" min="1" bind:value={issueValidityDays} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="issue-usage">Usage</label>
            <select id="issue-usage" class="select select-bordered select-sm" bind:value={issueUsage}>
              <option value="server">server</option>
              <option value="client">client</option>
              <option value="both">both</option>
            </select>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="form-control">
            <label class="label label-text" for="issue-key-algo">Key Algorithm</label>
            <select id="issue-key-algo" class="select select-bordered select-sm" bind:value={issueKeyAlgorithm}>
              <option value="ecdsa">ecdsa</option>
              <option value="rsa">rsa</option>
            </select>
          </div>
          <div class="form-control">
            <label class="label label-text" for="issue-key-size">Key Size (0 = default)</label>
            <input id="issue-key-size" class="input input-bordered input-sm" type="number" min="0" bind:value={issueKeySize} />
          </div>
        </div>
        <label class="flex items-center gap-2 cursor-pointer text-sm">
          <input type="checkbox" class="checkbox checkbox-sm" bind:checked={issueIsCA} />
          Issue as a CA certificate (sub-CA)
        </label>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showIssueDialog = false; }} disabled={issuing}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={issuing} onclick={() => void submitIssue()}>
          {issuing ? 'Issuing…' : 'Issue'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showIssueDialog = false; }} onkeydown={() => { showIssueDialog = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     SIGN CSR DIALOG
     ========================================================================= -->
{#if showSignCSRDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Sign CSR dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Sign CSR</h3>

      {#if signError}
        <div class="alert alert-error text-sm mb-3" role="alert">{signError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="csr-pem">CSR (PEM)</label>
          <textarea
            id="csr-pem"
            class="textarea textarea-bordered font-mono text-xs"
            rows="8"
            placeholder="-----BEGIN CERTIFICATE REQUEST-----"
            bind:value={csrPEM}
          ></textarea>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="form-control">
            <label class="label label-text" for="sign-validity">Validity (days)</label>
            <input id="sign-validity" class="input input-bordered input-sm" type="number" min="1" bind:value={signValidityDays} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="sign-usage">Usage</label>
            <select id="sign-usage" class="select select-bordered select-sm" bind:value={signUsage}>
              <option value="server">server</option>
              <option value="client">client</option>
              <option value="both">both</option>
            </select>
          </div>
        </div>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showSignCSRDialog = false; }} disabled={signing}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={signing} onclick={() => void submitSignCSR()}>
          {signing ? 'Signing…' : 'Sign'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showSignCSRDialog = false; }} onkeydown={() => { showSignCSRDialog = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     ISSUE RESULT — ONE-TIME DISPLAY (private key never re-servable)
     ========================================================================= -->
{#if issueResult}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Issued certificate">
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-1">Certificate Issued</h3>
      <p class="text-xs text-warning mb-4">
        The private key below is shown only once. Save it now — the server does not retain it.
      </p>
      <div class="space-y-3">
        <div>
          <div class="flex items-center justify-between mb-1">
            <span class="text-xs font-semibold text-base-content/50 uppercase tracking-wide">Certificate</span>
            <button class="btn btn-ghost btn-xs gap-1" onclick={() => void copyToClipboard(issueResult?.certificate_pem ?? '', 'Certificate')}>
              <Copy class="h-3 w-3" /> Copy
            </button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto max-h-32">{issueResult.certificate_pem}</pre>
        </div>
        <div>
          <div class="flex items-center justify-between mb-1">
            <span class="text-xs font-semibold text-warning uppercase tracking-wide">Private Key (shown once)</span>
            <button class="btn btn-ghost btn-xs gap-1" onclick={() => void copyToClipboard(issueResult?.private_key_pem ?? '', 'Private key')}>
              <Copy class="h-3 w-3" /> Copy
            </button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto max-h-32">{issueResult.private_key_pem}</pre>
        </div>
        <div class="text-xs text-base-content/50">Serial: <span class="font-mono">{issueResult.serial_number}</span></div>
      </div>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-outline"
          onclick={() => downloadText(`${issueCommonName || 'certificate'}.pem`, `${issueResult?.certificate_pem}\n${issueResult?.private_key_pem}`)}
        >
          Download
        </button>
        <button class="btn btn-primary btn-sm" onclick={() => { issueResult = null; }}>Done</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { issueResult = null; }} onkeydown={() => { issueResult = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     SIGN RESULT
     ========================================================================= -->
{#if signResult}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Signed certificate">
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-4">CSR Signed</h3>
      <div class="flex items-center justify-between mb-1">
        <span class="text-xs font-semibold text-base-content/50 uppercase tracking-wide">Certificate</span>
        <button class="btn btn-ghost btn-xs gap-1" onclick={() => void copyToClipboard(signResult?.certificate_pem ?? '', 'Certificate')}>
          <Copy class="h-3 w-3" /> Copy
        </button>
      </div>
      <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto max-h-48">{signResult.certificate_pem}</pre>
      <div class="text-xs text-base-content/50 mt-2">Serial: <span class="font-mono">{signResult.serial_number}</span></div>
      <div class="modal-action">
        <button class="btn btn-sm btn-outline" onclick={() => downloadText('signed-certificate.pem', signResult?.certificate_pem ?? '')}>
          Download
        </button>
        <button class="btn btn-primary btn-sm" onclick={() => { signResult = null; }}>Done</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { signResult = null; }} onkeydown={() => { signResult = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     REVOKE CONFIRM DIALOG
     ========================================================================= -->
{#if revokeTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Revoke certificate confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Revoke Certificate?</h3>
      <p class="text-sm text-base-content/70">
        Revoke <span class="font-mono font-semibold">{revokeTarget.common_name}</span>
        (serial <span class="font-mono">{revokeTarget.serial_number}</span>)? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { revokeTarget = null; }} disabled={revoking}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={revoking} onclick={() => void confirmRevoke()}>
          {revoking ? 'Revoking…' : 'Revoke'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { revokeTarget = null; }} onkeydown={() => { revokeTarget = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     VERIFY CERTIFICATE DIALOG
     ========================================================================= -->
{#if showVerifyDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Verify certificate dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Verify Certificate</h3>
      <div class="form-control">
        <label class="label label-text" for="verify-pem">Certificate (PEM)</label>
        <textarea id="verify-pem" class="textarea textarea-bordered font-mono text-xs" rows="8" bind:value={verifyPEM}></textarea>
      </div>
      {#if verifyResult}
        <div class="alert {verifyResult.valid ? 'alert-success' : 'alert-error'} text-sm mt-3" role="alert">
          {verifyResult.valid ? 'Certificate is valid.' : (verifyResult.error_message || 'Certificate is not valid.')}
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showVerifyDialog = false; }}>Close</button>
        <button class="btn btn-primary btn-sm" disabled={verifying} onclick={() => void submitVerify()}>
          {verifying ? 'Verifying…' : 'Verify'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showVerifyDialog = false; }} onkeydown={() => { showVerifyDialog = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     CHECK REVOCATION STATUS DIALOG
     ========================================================================= -->
{#if showCheckRevokedDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Check revocation status dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Check Revocation Status</h3>
      <div class="form-control">
        <label class="label label-text" for="check-serial">Serial Number</label>
        <input id="check-serial" class="input input-bordered input-sm font-mono" type="text" bind:value={checkSerial} />
      </div>
      {#if checkResult}
        <div class="alert {checkResult.revoked ? 'alert-warning' : 'alert-success'} text-sm mt-3" role="alert">
          {checkResult.revoked ? 'Certificate is revoked.' : 'Certificate is not revoked.'}
          {#if checkResult.message}<div class="text-xs mt-1">{checkResult.message}</div>{/if}
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCheckRevokedDialog = false; }}>Close</button>
        <button class="btn btn-primary btn-sm" disabled={checking} onclick={() => void submitCheckRevoked()}>
          {checking ? 'Checking…' : 'Check'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCheckRevokedDialog = false; }} onkeydown={() => { showCheckRevokedDialog = false; }}></div>
  </div>
{/if}
