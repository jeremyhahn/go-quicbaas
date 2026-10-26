<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  TenantBarrierView manages the per-tenant cryptographic barrier for tenants
  that select the "barrier" isolation mode. It is the per-tenant counterpart to
  BarrierView (which drives the process-wide system barrier).

  The operator first selects a tenant, then the view renders a strategy-aware
  UI driven by the broker's GET /api/v1/tenants/{id}/barrier/status:

    none        → banner "no per-tenant barrier"
    passphrase  → single passphrase unseal field (when sealed)
    shamir      → Shamir unseal flow with quorum progress (when sealed)

  Before initialization the BarrierStrategyPicker lets the admin provision any
  of the three strategies. A shamir init returns no share material: the shares
  are sealed to their holders, who each claim their own. The view lists the
  certificates issued to an inline roster's holders and the sealed share count.
  A claimed share envelope is delivered once, so the view keeps it until the
  holder downloads it and holds the init result open until every one is saved.

  All barrier traffic flows through the typed @quicbaas/sdk TenantBarrierClient
  (RestTenantBarrierClient under the hood); the view never builds raw URLs.
-->
<script lang="ts">
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { downloadText } from '../../lib/download.ts';
  import { Lock } from 'lucide-svelte';
  import { barrierCredentialBytes, toInitRequest } from '../../lib/barrierCredential.ts';
  import { APIError, RestTenantBarrierClient } from '@quicbaas/sdk';
  import type {
    BarrierQuorumOp,
    TenantBarrierClient,
    TenantBarrierInitResponse,
    TenantBarrierStatusResponse,
    IAMTenant,
  } from '@quicbaas/sdk';
  import {
    BarrierStatusBadge,
    BarrierStrategyPicker,
    CeremonyClaimChallenge,
    ShamirShareInput,
    BarrierUnsealForm,
    tenantClaimLabel,
  } from '@quicbaas/svelte-security';
  import type {
    BarrierClient as StructuralBarrierClient,
    BarrierStatus,
    BarrierStrategy,
    ShareClaimClient,
    TenantBarrierInitParams,
  } from '@quicbaas/svelte-security';

  // ---------------------------------------------------------------------------
  // Tenant-barrier client
  //
  // The per-tenant barrier is a REST-only surface, so the view constructs a
  // RestTenantBarrierClient against the current origin rather than going
  // through the auto-detected SDK singleton (which may be a Wails client whose
  // tenantBarrier accessor is intentionally null). The IAM tenant list still
  // comes from the shared singleton.
  // ---------------------------------------------------------------------------

  const baseURL = typeof window !== 'undefined' ? window.location.origin : '';
  const tb: TenantBarrierClient = new RestTenantBarrierClient(baseURL);

  // ---------------------------------------------------------------------------
  // Strategy classification — the broker returns the canonical strategy name;
  // map it onto the BarrierStrategy union the svelte-security components paint.
  // ---------------------------------------------------------------------------

  function classifyStrategy(mode: string | undefined): BarrierStrategy {
    const lower = (mode ?? '').toLowerCase();
    if (lower === 'none' || lower === 'disabled' || lower === 'off') return 'disabled';
    if (lower === 'passphrase' || lower === 'password' || lower === 'software') return 'software';
    if (lower === 'shamir') return 'shamir';
    // Hardware/cloud strategies are not selectable per-tenant here, but the
    // badge still renders them correctly if the broker reports one.
    if (lower === 'tpm2' || lower === 'tpm') return 'tpm2';
    if (lower === 'pkcs11') return 'pkcs11';
    if (lower === 'aws-kms') return 'aws-kms';
    if (lower === 'gcp-kms') return 'gcp-kms';
    if (lower === 'azure-kms') return 'azure-kms';
    if (lower === 'vault-kms') return 'vault-kms';
    return 'disabled';
  }

  // adaptStatus folds the broker's TenantBarrierStatusResponse into the
  // strategy-aware BarrierStatus shape the shared ShamirShareInput consumes.
  function adaptStatus(raw: TenantBarrierStatusResponse): BarrierStatus {
    return {
      strategy: classifyStrategy(raw.strategy),
      sealed: Boolean(raw.sealed),
      initialized: Boolean(raw.initialized),
      threshold: Number(raw.shamir_threshold ?? 0),
      total_shares: Number(raw.shamir_total ?? 0),
      shares_provided: 0,
      hardware_backed: Boolean(raw.hardware_backed),
      auto_unseal: false,
      version: 0,
    };
  }

  // structuralClient adapts the tenant-scoped SDK calls into the structural
  // BarrierClient shape ShamirShareInput expects (quorumBegin / quorumSubmit
  // of the tenant's quorum for op). It is rebuilt whenever the selected tenant
  // changes so every call is bound to the active tenant.
  function makeStructuralClient(
    tenantId: string,
    op: BarrierQuorumOp = 'unseal',
  ): StructuralBarrierClient {
    return {
      async status(): Promise<BarrierStatus> {
        return adaptStatus(await tb.tenantBarrierStatus(tenantId));
      },
      // init is unused by ShamirShareInput (we drive init via the picker), but
      // the structural contract requires it. Route it through the shamir init.
      async init(req) {
        const resp = await tb.tenantBarrierInit(tenantId, {
          strategy: 'shamir',
          threshold: req.threshold,
          shares: req.total_shares,
        });
        return {
          threshold: resp?.threshold ?? 0,
          total_shares: resp?.total_shares ?? 0,
          share_count: resp?.share_count ?? 0,
        };
      },
      async unseal(req) {
        await tb.tenantBarrierUnseal(tenantId, barrierCredentialBytes(req.passphrase));
        const next = await tb.tenantBarrierStatus(tenantId);
        return {
          sealed: next.sealed,
          shares_provided: 0,
          threshold: next.shamir_threshold,
        };
      },
      async quorumBegin(req) {
        return tb.tenantBarrierQuorumBegin(tenantId, { op, ...req });
      },
      async quorumSubmit(req) {
        return tb.tenantBarrierQuorumSubmit(tenantId, { op, ...req });
      },
    };
  }

  // makeClaimClient adapts the tenant share-claim verbs into the structural
  // ShareClaimClient CeremonyClaimChallenge drives, bound to one tenant.
  function makeClaimClient(tenantId: string): ShareClaimClient {
    return {
      async beginClaim(username) {
        return tb.tenantBarrierShareClaimBegin(tenantId, username);
      },
      async claimShare(req) {
        const share = await tb.tenantBarrierShareClaimComplete(tenantId, {
          username: req.username,
          nonce_hex: req.nonce_hex,
          signature: req.signature_b64,
        });
        claimedEnvelopes.push({
          tenantId,
          username: req.username,
          share_index: share.share_index,
          envelope: share.envelope,
          saved: false,
        });
        return share;
      },
    };
  }

  // ---------------------------------------------------------------------------
  // Reactive state
  // ---------------------------------------------------------------------------

  let tenants: IAMTenant[] = $state([]);
  let selectedTenantID = $state('');
  let tenantsLoading = $state(false);
  let tenantsError: string | null = $state(null);

  let status: TenantBarrierStatusResponse | null = $state(null);
  let barrierStatus: BarrierStatus | null = $state(null);
  let structural: StructuralBarrierClient | null = $state(null);
  let loading = $state(false);
  let statusError: string | null = $state(null);

  // Init state — a Shamir init's share-free result is held here so the
  // operator sees the holder certificates before the view moves on to status.
  let initing = $state(false);
  let initResult: TenantBarrierInitResponse | null = $state(null);
  let sealing = $state(false);
  // sealQuorum is the tenant's seal-quorum client while a Shamir seal is in
  // progress: qrdb refuses a direct seal of a Shamir barrier, so its key
  // holders seal it the way they unseal it.
  let sealQuorum: StructuralBarrierClient | null = $state(null);

  // A claimed share envelope is delivered exactly once: qrdb destroys its
  // copy during the claim. The view keeps every envelope until its holder
  // downloads it, and the init result cannot be dismissed before then.
  interface ClaimedEnvelope {
    tenantId: string;
    username: string;
    share_index: number;
    envelope: string;
    saved: boolean;
  }
  let claimedEnvelopes: ClaimedEnvelope[] = $state([]);
  const tenantEnvelopes = $derived(claimedEnvelopes.filter((e) => e.tenantId === selectedTenantID));
  const unsavedEnvelopes = $derived(tenantEnvelopes.filter((e) => !e.saved).length);

  function saveEnvelope(e: ClaimedEnvelope): void {
    downloadText(`${e.tenantId}-${e.username}-share-${e.share_index}.envelope`, e.envelope, 'application/octet-stream');
    e.saved = true;
  }

  async function loadTenants(): Promise<void> {
    tenantsLoading = true;
    tenantsError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listTenants();
      const seen = new Set<string>();
      tenants = resp.tenants.filter((t) => {
        if (seen.has(t.id)) return false;
        seen.add(t.id);
        return true;
      });
      if (selectedTenantID === '' && tenants.length > 0) {
        selectedTenantID = tenants[0]?.id ?? '';
      }
    } catch (e) {
      tenantsError = e instanceof Error ? e.message : 'Failed to load tenants';
    } finally {
      tenantsLoading = false;
    }
  }

  async function loadStatus(): Promise<void> {
    if (selectedTenantID === '') {
      status = null;
      barrierStatus = null;
      return;
    }
    loading = true;
    statusError = null;
    try {
      structural = makeStructuralClient(selectedTenantID);
      const raw = await tb.tenantBarrierStatus(selectedTenantID);
      status = raw;
      barrierStatus = adaptStatus(raw);
    } catch (e) {
      if (e instanceof APIError) {
        statusError = `[${e.status}] ${e.message}`;
      } else {
        statusError = e instanceof Error ? e.message : 'Failed to load barrier status';
      }
      status = null;
      barrierStatus = null;
    } finally {
      loading = false;
    }
  }

  // Reload status whenever the operator picks a different tenant.
  $effect(() => {
    void selectedTenantID;
    // Reset transient init and seal state when switching tenants.
    initResult = null;
    sealQuorum = null;
    void loadStatus();
  });

  $effect(() => {
    void loadTenants();
  });

  async function handleInit(params: TenantBarrierInitParams): Promise<void> {
    if (selectedTenantID === '') return;
    initing = true;
    try {
      // The SDK takes the passphrase as bytes and base64-encodes it once;
      // init and unseal both hand it the same UTF-8 bytes, so a barrier
      // initialized through this view unseals through it (go-quicbaas#988).
      const resp = await tb.tenantBarrierInit(selectedTenantID, toInitRequest(params));
      if (params.strategy === 'shamir' && resp !== undefined) {
        // Shamir returns share-free metadata: the shares are sealed to their
        // holders, who each claim their own.
        initResult = resp;
        showToast({
          message: 'Tenant barrier initialized — each holder now claims their share',
          type: 'success',
        });
      } else {
        showToast({ message: 'Tenant barrier initialized', type: 'success' });
        await loadStatus();
      }
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Initialize failed',
        type: 'error',
      });
    } finally {
      initing = false;
    }
  }

  async function dismissInitResult(): Promise<void> {
    if (unsavedEnvelopes > 0) return;
    initResult = null;
    await loadStatus();
  }

  async function handleSeal(): Promise<void> {
    if (selectedTenantID === '') return;
    if (barrierStatus?.strategy === 'shamir') {
      sealQuorum = makeStructuralClient(selectedTenantID, 'seal');
      return;
    }
    sealing = true;
    try {
      await tb.tenantBarrierSeal(selectedTenantID);
      showToast({ message: 'Tenant barrier sealed', type: 'success' });
      await loadStatus();
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Seal failed',
        type: 'error',
      });
    } finally {
      sealing = false;
    }
  }
</script>

{#snippet holderClaim(tenantId: string)}
  <div class="border-t border-base-300 pt-4" data-testid="tenant-barrier-claim">
    <CeremonyClaimChallenge
      client={makeClaimClient(tenantId)}
      claimLabel={tenantClaimLabel(tenantId)}
      onclaimed={(share) => { showToast({ message: `Share ${share.share_index} claimed`, type: 'success' }); }}
      onerror={(err) => { showToast({ message: err.message, type: 'error' }); }}
    />
    {#if tenantEnvelopes.length > 0}
      <div class="alert alert-warning text-sm mt-3" role="alert">
        <span>
          Each claimed envelope is delivered once: the server destroyed its copy during the
          claim. Download every envelope now; it cannot be fetched again.
        </span>
      </div>
      {#each tenantEnvelopes as e (`${e.username}-${e.share_index}`)}
        <div class="flex items-center gap-2 mt-2 text-sm" data-testid="tenant-barrier-envelope">
          <span class="font-semibold">{e.username}</span>
          <span>share {e.share_index}</span>
          <code class="font-mono text-xs break-all flex-1">{e.envelope}</code>
          <button class="btn btn-xs btn-outline" type="button" onclick={() => { saveEnvelope(e); }}>
            Download envelope
          </button>
          {#if e.saved}<span class="badge badge-success badge-sm">saved</span>{/if}
        </div>
      {/each}
    {/if}
  </div>
{/snippet}

<div class="p-6 space-y-6">
  <PageHeader
    title="Tenant Barrier"
    subtitle="Per-tenant encryption-at-rest barrier and unseal ceremony"
    icon={Lock}
  />

  <!-- Tenant selector -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-3">
      {#if tenantsError !== null}
        <div class="alert alert-error text-sm" role="alert">
          <span>{tenantsError}</span>
          <button class="btn btn-xs btn-ghost" onclick={() => { void loadTenants(); }}>Retry</button>
        </div>
      {/if}
      <div class="form-control max-w-sm">
        <label class="label label-text" for="tenant-barrier-select">Tenant</label>
        <select
          id="tenant-barrier-select"
          class="select select-bordered select-sm"
          bind:value={selectedTenantID}
          disabled={tenantsLoading || tenants.length === 0}
          data-testid="tenant-barrier-select"
        >
          {#if tenants.length === 0}
            <option value="">No tenants available</option>
          {/if}
          {#each tenants as tenant (tenant.id)}
            <option value={tenant.id}>{tenant.name || tenant.id}</option>
          {/each}
        </select>
      </div>
    </div>
  </div>

  {#if selectedTenantID !== ''}
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-6 gap-4">

        {#if statusError !== null}
          <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
            <span>{statusError}</span>
            <button class="btn btn-xs btn-ghost" onclick={() => { void loadStatus(); }}>Retry</button>
          </div>
        {/if}

        {#if loading && status === null}
          <div class="flex justify-center py-4">
            <span class="loading loading-spinner loading-md"></span>
          </div>
        {:else if status !== null && barrierStatus !== null}

          <!-- Status header -->
          <div class="flex items-center gap-4 flex-wrap">
            <div class="flex-1 min-w-[200px]">
              <h2 class="text-lg font-semibold">Tenant Encryption Barrier</h2>
              <p class="text-xs text-base-content/40 font-mono">{status.tenant_id}</p>
            </div>
            <BarrierStatusBadge
              strategy={barrierStatus.strategy}
              sealed={status.sealed}
              hardwareBacked={status.hardware_backed}
            />
          </div>

          <!-- =================================================================
               Not initialized → strategy picker
               ================================================================= -->
          {#if !status.initialized}
            {#if initResult !== null}
              <div class="space-y-3" data-testid="tenant-barrier-holders">
                <div class="alert alert-info text-sm" role="alert">
                  <span>
                    Shamir barrier initialized: any {initResult.threshold} of {initResult.total_shares}
                    shares unseal it. {initResult.share_count} shares sealed to their holders; each
                    holder claims their own share.
                  </span>
                </div>
                {#each initResult.holders ?? [] as holder (holder.username)}
                  <div class="space-y-1" data-testid="tenant-barrier-holder">
                    <p class="text-sm font-semibold">{holder.username} <span class="text-xs text-base-content/60">({holder.role})</span></p>
                    <pre class="text-xs font-mono whitespace-pre-wrap break-all bg-base-200 p-2 rounded">{holder.cert_pem}</pre>
                  </div>
                {/each}
                {@render holderClaim(selectedTenantID)}
                <button
                  class="btn btn-sm btn-primary"
                  disabled={unsavedEnvelopes > 0}
                  title={unsavedEnvelopes > 0 ? 'Download every claimed envelope first' : undefined}
                  onclick={() => { void dismissInitResult(); }}
                >Continue</button>
              </div>
            {:else}
              <div class="space-y-3" data-testid="tenant-barrier-init">
                <p class="text-sm text-base-content/70">
                  This tenant's barrier is not initialized. Choose a strategy to
                  provision it.
                </p>
                <BarrierStrategyPicker
                  submitting={initing}
                  oninit={(params) => { void handleInit(params); }}
                />
              </div>
            {/if}

          <!-- =================================================================
               Initialized → strategy-aware unseal / status
               ================================================================= -->
          {:else if barrierStatus.strategy === 'disabled'}
            <div class="alert alert-info text-sm" role="alert" data-testid="tenant-barrier-none">
              <span>This tenant has no encryption barrier (strategy: none).</span>
            </div>

          {:else if barrierStatus.strategy === 'software'}
            {#if status.sealed && structural !== null}
              <div data-testid="tenant-barrier-passphrase-unseal">
                <BarrierUnsealForm
                  client={structural}
                  label="Unseal passphrase"
                  placeholder="Enter the tenant passphrase..."
                  onunseal={(resp) => {
                    if (!resp.sealed) {
                      showToast({ message: 'Tenant barrier unsealed', type: 'success' });
                    }
                    void loadStatus();
                  }}
                  onerror={(err) => { showToast({ message: err.message, type: 'error' }); }}
                />
              </div>
            {:else}
              <div class="alert alert-success text-sm" data-testid="tenant-barrier-passphrase-unsealed">
                <span>Passphrase barrier is unsealed and active.</span>
              </div>
            {/if}

          {:else if barrierStatus.strategy === 'shamir'}
            {#if status.sealed && structural !== null}
              <div data-testid="tenant-barrier-shamir-unseal">
                <ShamirShareInput
                  client={structural}
                  status={barrierStatus}
                  onunseal={(s) => {
                    if (!s.sealed) {
                      showToast({ message: 'Tenant barrier unsealed', type: 'success' });
                    }
                    void loadStatus();
                  }}
                  onerror={(err) => { showToast({ message: err.message, type: 'error' }); }}
                />
              </div>
            {:else if sealQuorum !== null}
              <div data-testid="tenant-barrier-shamir-seal">
                <ShamirShareInput
                  client={sealQuorum}
                  status={barrierStatus}
                  label="Seal"
                  onunseal={() => {
                    sealQuorum = null;
                    showToast({ message: 'Tenant barrier sealed', type: 'success' });
                    void loadStatus();
                  }}
                  onerror={(err) => { showToast({ message: err.message, type: 'error' }); }}
                />
              </div>
            {:else}
              <div class="alert alert-success text-sm" data-testid="tenant-barrier-shamir-unsealed">
                <span>Shamir barrier is unsealed and active.</span>
              </div>
            {/if}
            <!-- Holders claim their sealed shares whether or not the barrier is
                 sealed: a fresh barrier cannot unseal until they have. -->
            {@render holderClaim(selectedTenantID)}

          {:else}
            <div class="alert alert-success text-sm" data-testid="tenant-barrier-auto">
              <span>
                The {barrierStatus.strategy} provider seals and unseals this
                tenant's barrier automatically.
              </span>
            </div>
          {/if}

          <!-- Detail grid -->
          <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
            <div>
              <div class="text-base-content/50 text-xs mb-1">Initialized</div>
              <div class="font-medium">{status.initialized ? 'Yes' : 'No'}</div>
            </div>
            <div>
              <div class="text-base-content/50 text-xs mb-1">Strategy</div>
              <div class="font-medium">{status.strategy}</div>
            </div>
            <div>
              <div class="text-base-content/50 text-xs mb-1">Shamir threshold</div>
              <div class="font-medium">{status.shamir_threshold}</div>
            </div>
            <div>
              <div class="text-base-content/50 text-xs mb-1">Shamir total</div>
              <div class="font-medium">{status.shamir_total}</div>
            </div>
          </div>
        {/if}

        <!-- Actions -->
        <div class="flex gap-2 mt-2">
          <button
            class="btn btn-ghost btn-sm"
            onclick={() => { void loadStatus(); }}
            disabled={loading}
          >
            Refresh
          </button>
          {#if status !== null && status.initialized && !status.sealed && status.strategy !== 'none'}
            <button
              class="btn btn-warning btn-sm"
              onclick={() => { void handleSeal(); }}
              disabled={sealing || sealQuorum !== null}
              data-testid="tenant-barrier-seal"
            >
              {sealing ? 'Sealing...' : 'Seal'}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</div>
