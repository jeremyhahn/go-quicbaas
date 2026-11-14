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
  of the three strategies. A shamir init reveals the minted shares behind a
  save-acknowledgement gate (ShamirSharesReveal) — the shares are the only copy
  of the unseal material and are returned exactly once.

  All barrier traffic flows through the typed @quicbaas/sdk TenantBarrierClient
  (RestTenantBarrierClient under the hood); the view never builds raw URLs.
-->
<script lang="ts">
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { Lock } from 'lucide-svelte';
  import {
    encodeBarrierCredential,
    encodeInitParams,
  } from '../../lib/barrierCredential.ts';
  import { APIError, RestTenantBarrierClient } from '@quicbaas/sdk';
  import type {
    TenantBarrierClient,
    TenantBarrierStatusResponse,
    IAMTenant,
    BarrierEnableEncryptionRequest,
  } from '@quicbaas/sdk';
  import {
    BarrierStatusBadge,
    BarrierStrategyPicker,
    ShamirSharesReveal,
    ShamirShareInput,
    BarrierUnsealForm,
  } from '@quicbaas/svelte-security';
  import type {
    BarrierClient as StructuralBarrierClient,
    BarrierStatus,
    BarrierStrategy,
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
  // BarrierClient shape ShamirShareInput expects (unseal({share}) →
  // {sealed, shares_provided, threshold}). It is rebuilt whenever the selected
  // tenant changes so every call is bound to the active tenant.
  function makeStructuralClient(tenantId: string): StructuralBarrierClient {
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
        return { shares: resp?.shares ?? [] };
      },
      async unseal(req) {
        const resp = await tb.tenantBarrierUnsealShare(tenantId, req.share);
        return {
          sealed: resp.sealed,
          shares_provided: resp.submitted,
          threshold: resp.required,
        };
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

  // Init state — the freshly minted shares are held here until the operator
  // acknowledges saving them via ShamirSharesReveal.
  let initing = $state(false);
  let revealedShares: string[] | null = $state(null);
  let revealedThreshold = $state(0);
  let sealing = $state(false);

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
    // Reset transient init state when switching tenants.
    revealedShares = null;
    void loadStatus();
  });

  $effect(() => {
    void loadTenants();
  });

  async function handleInit(params: TenantBarrierInitParams): Promise<void> {
    if (selectedTenantID === '') return;
    initing = true;
    try {
      // Encode here, not in the picker: a barrier credential is BYTES, which
      // the SDK base64-encodes once onto the wire, and unseal below goes
      // through the same encoder. Forwarding the picker's plaintext meant a
      // barrier initialized through this view could not be unsealed through
      // it (go-quicbaas#988); handing the SDK an already-base64 string sent
      // all-zero bytes (go-quicbaas#2372). Both paths now send the typed
      // text's UTF-8 bytes through the one encoder.
      const resp = await tb.tenantBarrierInit(
        selectedTenantID,
        encodeInitParams(params),
      );
      if (params.strategy === 'shamir' && resp !== undefined) {
        // Shamir returns the shares exactly once — reveal them behind the
        // save-acknowledgement gate before refreshing status.
        revealedShares = resp.shares;
        revealedThreshold = resp.threshold;
        showToast({
          message: 'Tenant barrier initialized — store the Shamir shares securely!',
          type: 'warning',
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

  // ---------------------------------------------------------------------------
  // Enable encryption — migrates a tenant's unencrypted (strategy none)
  // barrier to a keyed one (POST /api/v1/tenants/{id}/barrier/enable-encryption).
  // ---------------------------------------------------------------------------

  let enableStrategy: 'passphrase' | 'shamir' = $state('passphrase');
  let enableMechanism: 'swap' | 'inplace' = $state('swap');
  let enableAvailability: 'online' | 'quiesced' = $state('online');
  let enablePassphrase = $state('');
  let enablePassphraseConfirm = $state('');
  let enableThreshold = $state(2);
  let enableTotal = $state(3);
  let enableConfirming = $state(false);
  let enabling = $state(false);

  const enableValid = $derived(
    enableStrategy === 'passphrase'
      ? enablePassphrase !== '' && enablePassphrase === enablePassphraseConfirm
      : Number.isInteger(enableThreshold) &&
          Number.isInteger(enableTotal) &&
          enableThreshold >= 1 &&
          enableThreshold <= enableTotal,
  );

  const selectedTenantName = $derived(
    tenants.find((t) => t.id === selectedTenantID)?.name ?? selectedTenantID,
  );

  async function handleEnableEncryption(): Promise<void> {
    if (selectedTenantID === '' || !enableValid) return;
    enabling = true;
    const req: BarrierEnableEncryptionRequest = {
      tenant_id: selectedTenantID,
      strategy: enableStrategy,
      mechanism: enableMechanism,
      availability: enableAvailability,
    };
    if (enableStrategy === 'passphrase') {
      // A barrier credential is BYTES: send the typed text's UTF-8 bytes, the
      // way encodeInitParams encodes init's credential.
      req.passphrase = new TextEncoder().encode(enablePassphrase);
    } else {
      req.shamir_threshold = enableThreshold;
      req.shamir_total = enableTotal;
    }
    try {
      const client = await getSDKClient();
      await client.barrier.enableEncryption(req);
      showToast({
        message: `Encryption migration started for tenant ${selectedTenantName}`,
        type: 'success',
      });
      await loadStatus();
    } catch (e) {
      showToast({
        message: e instanceof Error ? e.message : 'Enable encryption failed',
        type: 'error',
      });
    } finally {
      enablePassphrase = '';
      enablePassphraseConfirm = '';
      enableConfirming = false;
      enabling = false;
    }
  }

  async function dismissShares(): Promise<void> {
    revealedShares = null;
    await loadStatus();
  }

  async function handlePassphraseUnseal(passphrase: string): Promise<void> {
    if (selectedTenantID === '') return;
    await tb.tenantBarrierUnseal(selectedTenantID, encodeBarrierCredential(passphrase));
  }

  async function handleSeal(): Promise<void> {
    if (selectedTenantID === '') return;
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
            {#if revealedShares !== null}
              <div data-testid="tenant-barrier-shares-reveal">
                <ShamirSharesReveal
                  shares={revealedShares}
                  threshold={revealedThreshold}
                  ondismiss={() => { void dismissShares(); }}
                />
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
            <form
              class="space-y-3"
              aria-label="Enable encryption"
              onsubmit={(e) => { e.preventDefault(); }}
            >
              <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
                <label class="form-control">
                  <span class="label-text text-xs">Strategy</span>
                  <select class="select select-bordered select-sm" bind:value={enableStrategy}>
                    <option value="passphrase">passphrase</option>
                    <option value="shamir">shamir</option>
                  </select>
                </label>
                <label class="form-control">
                  <span class="label-text text-xs">Mechanism</span>
                  <select class="select select-bordered select-sm" bind:value={enableMechanism}>
                    <option value="swap">swap</option>
                    <option value="inplace">inplace</option>
                  </select>
                </label>
                <label class="form-control">
                  <span class="label-text text-xs">Availability</span>
                  <select class="select select-bordered select-sm" bind:value={enableAvailability}>
                    <option value="online">online</option>
                    <option value="quiesced">quiesced</option>
                  </select>
                </label>
              </div>
              {#if enableStrategy === 'passphrase'}
                <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <label class="form-control">
                    <span class="label-text text-xs">Passphrase</span>
                    <input
                      type="password"
                      class="input input-bordered input-sm"
                      autocomplete="new-password"
                      bind:value={enablePassphrase}
                    />
                  </label>
                  <label class="form-control">
                    <span class="label-text text-xs">Confirm passphrase</span>
                    <input
                      type="password"
                      class="input input-bordered input-sm"
                      autocomplete="new-password"
                      bind:value={enablePassphraseConfirm}
                    />
                  </label>
                </div>
              {:else}
                <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <label class="form-control">
                    <span class="label-text text-xs">Threshold</span>
                    <input
                      type="number"
                      min="1"
                      class="input input-bordered input-sm"
                      bind:value={enableThreshold}
                    />
                  </label>
                  <label class="form-control">
                    <span class="label-text text-xs">Total shares</span>
                    <input
                      type="number"
                      min="1"
                      class="input input-bordered input-sm"
                      bind:value={enableTotal}
                    />
                  </label>
                </div>
              {/if}
              {#if enableConfirming}
                <div class="alert alert-warning text-sm" data-testid="tenant-barrier-enable-confirm">
                  <span>
                    Enable encryption for tenant {selectedTenantName}? The tenant's
                    barrier is re-encrypted with the new {enableStrategy} key.
                  </span>
                  <div class="flex gap-2">
                    <button
                      type="button"
                      class="btn btn-ghost btn-sm"
                      onclick={() => { enableConfirming = false; }}
                      disabled={enabling}
                    >
                      Cancel
                    </button>
                    <button
                      type="button"
                      class="btn btn-warning btn-sm"
                      onclick={() => { void handleEnableEncryption(); }}
                      disabled={enabling || !enableValid}
                    >
                      {enabling ? 'Starting...' : 'Confirm'}
                    </button>
                  </div>
                </div>
              {:else}
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  onclick={() => { enableConfirming = true; }}
                  disabled={!enableValid}
                >
                  Enable
                </button>
              {/if}
            </form>

          {:else if barrierStatus.strategy === 'software'}
            {#if status.sealed}
              <div data-testid="tenant-barrier-passphrase-unseal">
                <BarrierUnsealForm
                  client={{
                    status: async () => barrierStatus!,
                    init: async () => ({ shares: [] }),
                    unseal: async (req) => {
                      await handlePassphraseUnseal(req.share);
                      const next = await tb.tenantBarrierStatus(selectedTenantID);
                      return {
                        sealed: next.sealed,
                        shares_provided: 0,
                        threshold: next.shamir_threshold,
                      };
                    },
                  }}
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
            {:else}
              <div class="alert alert-success text-sm" data-testid="tenant-barrier-shamir-unsealed">
                <span>Shamir barrier is unsealed and active.</span>
              </div>
            {/if}

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
              disabled={sealing}
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
