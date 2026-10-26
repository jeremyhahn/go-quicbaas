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
  BarrierView is the strategy-aware replacement for the legacy SealView. It
  reads the barrier configuration from the daemon and renders a UI that is
  appropriate for the configured strategy:

    disabled  → banner "encryption disabled"
    software  → single password unseal field
    shamir    → Shamir init / unseal flow with progress meter
    tpm2      → "auto-init at boot" + "auto-unseal active"
    pkcs11    → same as tpm2
    *-kms     → same as tpm2

  Strategy-specific affordances live in @quicbaas/svelte-security so the
  same components can be reused by other Svelte apps.

  All barrier traffic flows through the typed @quicbaas/sdk BarrierClient
  (RestBarrierClient under the hood). The view never hits /api/v1/seal/*;
  the canonical surface is /api/v1/barrier/* per the QuicBaaS REST plan.
-->
<script lang="ts">
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { ShieldOff } from 'lucide-svelte';
  import { APIError, createBarrierClient, isWailsRuntime } from '@quicbaas/sdk';
  import type {
    BarrierClient as SDKBarrierClient,
    BarrierStatusResponse,
  } from '@quicbaas/sdk';
  import {
    BarrierStatusBadge,
    BarrierUnsealForm,
    ShamirShareInput,
  } from '@quicbaas/svelte-security';
  import type {
    BarrierClient,
    BarrierInitResponse,
    BarrierStatus,
    BarrierStrategy,
  } from '@quicbaas/svelte-security';

  // ---------------------------------------------------------------------------
  // SDK -> structural BarrierClient adapter
  //
  // The svelte-security components consume a structurally-typed BarrierClient
  // (init({threshold, total_shares}), unseal({passphrase}), quorumBegin,
  // quorumSubmit, status() → BarrierStatus). The SDK BarrierClient surface is
  // richer (initializeShamir, the quorum domains and ops, etc.) so we adapt the
  // SDK calls into the shape the components expect. Crucially, all URL routing flows through the SDK so
  // future server-side path changes propagate automatically.
  // ---------------------------------------------------------------------------

  function classifyStrategy(mode: string | undefined): BarrierStrategy {
    if (mode === undefined || mode === '' || mode === 'manual') return 'shamir';
    const lower = mode.toLowerCase();
    if (lower === 'disabled' || lower === 'off') return 'disabled';
    if (lower === 'software' || lower === 'password' || lower === 'passphrase') return 'software';
    if (lower === 'shamir') return 'shamir';
    if (lower === 'tpm2' || lower === 'tpm') return 'tpm2';
    if (lower === 'pkcs11') return 'pkcs11';
    if (lower === 'aws-kms' || lower === 'awskms') return 'aws-kms';
    if (lower === 'gcp-kms' || lower === 'gcpkms') return 'gcp-kms';
    if (lower === 'azure-kms' || lower === 'azurekms') return 'azure-kms';
    if (lower === 'vault-kms' || lower === 'vaultkms') return 'vault-kms';
    return 'shamir';
  }

  function isHardwareBacked(strategy: BarrierStrategy): boolean {
    return (
      strategy === 'tpm2' ||
      strategy === 'pkcs11' ||
      strategy === 'aws-kms' ||
      strategy === 'gcp-kms' ||
      strategy === 'azure-kms' ||
      strategy === 'vault-kms'
    );
  }

  function isAutoUnseal(strategy: BarrierStrategy): boolean {
    return isHardwareBacked(strategy);
  }

  // resolveTransport prefers the desktop Wails transport when running inside
  // the wails runtime; otherwise it uses REST against the current origin.
  function resolveTransport(): 'wails' | 'rest' {
    return isWailsRuntime() ? 'wails' : 'rest';
  }

  // makeClient builds the structural BarrierClient that the svelte-security
  // components consume by adapting the typed SDK BarrierClient. Every HTTP
  // path is owned by the SDK — see RestBarrierClient in @quicbaas/sdk.
  async function makeClient(): Promise<{ structural: BarrierClient; sdk: SDKBarrierClient }> {
    const baseURL = typeof window !== 'undefined' ? window.location.origin : '';
    const sdk = await createBarrierClient(resolveTransport(), { baseURL });

    // adaptStatus folds the SDK BarrierStatusResponse into the
    // strategy-aware BarrierStatus shape the
    // components consume. Optional fields (initialized, threshold, etc.) are
    // best-effort: the SDK BarrierStatusResponse is intentionally minimal
    // because the daemon's response shape varies per strategy.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const adaptStatus = async (raw: BarrierStatusResponse | any): Promise<BarrierStatus> => {
      const strategy = classifyStrategy(raw.strategy ?? raw.mode);
      const threshold = Number(raw.threshold ?? 0);
      const totalShares = Number(raw.total_shares ?? 0);
      const sharesProvided = Number(raw.shares_provided ?? 0);
      return {
        strategy,
        sealed: Boolean(raw.sealed),
        initialized: Boolean(raw.initialized ?? raw.initialized_at !== undefined),
        threshold,
        total_shares: totalShares,
        shares_provided: sharesProvided,
        hardware_backed: Boolean(raw.hardware_backed) || isHardwareBacked(strategy),
        auto_unseal: isAutoUnseal(strategy),
        version: Number(raw.version ?? 0),
        cluster_name: raw.cluster_name as string | undefined,
      };
    };

    const structural: BarrierClient = {
      async status(): Promise<BarrierStatus> {
        const raw = await sdk.status();
        return adaptStatus(raw);
      },
      async init(req) {
        const resp = await sdk.initializeShamir({
          // The SDK initializeShamir requires a master-key secret; for the
          // strategy-aware UI the daemon's barrier strategy already carries
          // the protector, so we send an empty secret and rely on the daemon
          // to derive the master key from the active strategy. The secret is
          // bytes; the SDK encodes it.
          secret: new Uint8Array(0),
          threshold: req.threshold,
          total_shares: req.total_shares,
        });
        return {
          threshold: resp.threshold,
          total_shares: resp.total_shares,
          share_count: resp.share_count,
        };
      },
      async unseal(req) {
        await sdk.unseal({ secret: new TextEncoder().encode(req.passphrase) });
        const after = await adaptStatus(await sdk.status());
        return {
          sealed: after.sealed,
          shares_provided: after.shares_provided,
          threshold: after.threshold,
        };
      },
      // A Shamir barrier unseals through a quorum round in the unseal domain.
      async quorumBegin(req) {
        return sdk.quorumBegin({ domain: 'unseal', op: 'unseal', ...req });
      },
      async quorumSubmit(req) {
        return sdk.quorumSubmit({ domain: 'unseal', op: 'unseal', ...req });
      },
    };

    return { structural, sdk };
  }

  // ---------------------------------------------------------------------------
  // Reactive state
  // ---------------------------------------------------------------------------

  let client: BarrierClient | null = $state(null);
  let status: BarrierStatus | null = $state(null);
  let loading = $state(false);
  let statusError: string | null = $state(null);
  let subsystemDisabled = $state(false);

  // Shamir init form state.
  let initThreshold = $state(2);
  let initTotal = $state(3);
  let initing = $state(false);
  let initResult: BarrierInitResponse | null = $state(null);
  let initError: string | null = $state(null);

  async function loadStatus(): Promise<void> {
    loading = true;
    statusError = null;
    subsystemDisabled = false;
    try {
      if (client === null) {
        const { structural } = await makeClient();
        client = structural;
      }
      status = await client.status();
    } catch (e) {
      if (
        e instanceof APIError &&
        (e.status === 500 || e.status === 503) &&
        e.message.toLowerCase().includes('not available')
      ) {
        subsystemDisabled = true;
      } else if (e instanceof APIError) {
        statusError = `[${e.status}] ${e.message}`;
      } else {
        statusError = e instanceof Error ? e.message : 'Failed to load barrier status';
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void loadStatus();
  });

  async function submitShamirInit(): Promise<void> {
    if (client === null) return;
    initError = null;
    initing = true;
    try {
      const resp = await client.init({
        threshold: initThreshold,
        total_shares: initTotal,
      });
      // Share-free: the shares are sealed to their holders, who each claim
      // their own.
      initResult = resp;
      showToast({
        message: 'Barrier initialized — each holder now claims their share',
        type: 'success',
      });
      void loadStatus();
    } catch (e) {
      initError = e instanceof Error ? e.message : 'Initialize failed';
    } finally {
      initing = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader
    title="Barrier"
    subtitle="Encryption-at-rest barrier and key management"
    icon={ShieldOff}
  />

  {#if subsystemDisabled}
    <div class="alert alert-info text-sm" role="alert" data-testid="barrier-disabled-banner">
      <span>
        Barrier subsystem is not enabled. Set
        <code class="font-mono">barrier.enabled: true</code>
        in the daemon config or set
        <code class="font-mono">QUICBAAS_BARRIER_ENABLED=true</code>
        to activate encryption.
      </span>
    </div>
  {:else if statusError !== null}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{statusError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadStatus(); }}>Retry</button>
    </div>
  {/if}

  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">

      {#if loading && status === null}
        <div class="flex justify-center py-4">
          <span class="loading loading-spinner loading-md"></span>
        </div>
      {:else if status !== null}

        <!-- Strategy-aware header -->
        <div class="flex items-center gap-4 flex-wrap">
          <div class="flex-1 min-w-[200px]">
            <h2 class="text-lg font-semibold">Encryption Barrier</h2>
            {#if status.cluster_name}
              <p class="text-xs text-base-content/40 font-mono">{status.cluster_name}</p>
            {/if}
          </div>
          <BarrierStatusBadge
            strategy={status.strategy}
            sealed={status.sealed}
            hardwareBacked={status.hardware_backed}
          />
        </div>

        <!-- ===================================================================
             Strategy: disabled
             =================================================================== -->
        {#if status.strategy === 'disabled'}
          <div class="alert alert-warning text-sm" role="alert" data-testid="barrier-strategy-disabled">
            <span>
              Encryption is disabled. Set
              <code class="font-mono">barrier.enabled: true</code>
              to activate the barrier.
            </span>
          </div>

        <!-- ===================================================================
             Strategy: software (single password)
             =================================================================== -->
        {:else if status.strategy === 'software'}
          {#if status.sealed && client !== null}
            <div data-testid="barrier-strategy-software">
              <BarrierUnsealForm
                client={client}
                label="Unseal password"
                placeholder="Enter the operator password..."
                onunseal={(resp) => {
                  if (!resp.sealed) {
                    showToast({ message: 'Barrier unsealed', type: 'success' });
                  }
                  void loadStatus();
                }}
                onerror={(err) => {
                  showToast({ message: err.message, type: 'error' });
                }}
              />
            </div>
          {:else}
            <div class="alert alert-success text-sm" data-testid="barrier-strategy-software-unsealed">
              <span>Software barrier is unsealed and active.</span>
            </div>
          {/if}

        <!-- ===================================================================
             Strategy: shamir
             =================================================================== -->
        {:else if status.strategy === 'shamir'}
          {#if !status.initialized}
            <div class="space-y-3" data-testid="barrier-strategy-shamir-init">
              <p class="text-sm text-base-content/70">
                Initialize the Shamir barrier. Choose how many shares to mint and
                the threshold required to unseal. Each share is sealed to its holder,
                who claims it individually.
              </p>
              {#if initError !== null}
                <div class="alert alert-error text-sm" role="alert">{initError}</div>
              {/if}
              {#if initResult !== null}
                <div class="alert alert-success text-sm" role="alert" data-testid="barrier-shamir-init-result">
                  <span>
                    Barrier initialized: any {initResult.threshold} of {initResult.total_shares}
                    shares unseal it. {initResult.share_count} shares sealed to their holders;
                    each holder claims their own share.
                  </span>
                </div>
              {:else}
                <div class="grid grid-cols-2 gap-3">
                  <div class="form-control">
                    <label class="label label-text" for="shamir-init-threshold">Threshold</label>
                    <input
                      id="shamir-init-threshold"
                      class="input input-bordered input-sm"
                      type="number"
                      min="1"
                      bind:value={initThreshold}
                    />
                  </div>
                  <div class="form-control">
                    <label class="label label-text" for="shamir-init-total">Total shares</label>
                    <input
                      id="shamir-init-total"
                      class="input input-bordered input-sm"
                      type="number"
                      min="1"
                      bind:value={initTotal}
                    />
                  </div>
                </div>
                <button
                  class="btn btn-accent btn-sm"
                  disabled={initing}
                  onclick={() => { void submitShamirInit(); }}
                >
                  {initing ? 'Initializing...' : 'Initialize Shamir barrier'}
                </button>
              {/if}
            </div>
          {:else if status.sealed && client !== null}
            <div data-testid="barrier-strategy-shamir-unseal">
              <ShamirShareInput
                client={client}
                status={status}
                onunseal={(s) => {
                  if (!s.sealed) {
                    showToast({ message: 'Barrier unsealed', type: 'success' });
                  }
                  void loadStatus();
                }}
                onerror={(err) => {
                  showToast({ message: err.message, type: 'error' });
                }}
              />
            </div>
          {:else}
            <div
              class="alert alert-success text-sm"
              data-testid="barrier-strategy-shamir-unsealed"
            >
              <span>Shamir barrier is unsealed and active.</span>
            </div>
          {/if}

        <!-- ===================================================================
             Strategy: tpm2 / pkcs11 / *-kms
             =================================================================== -->
        {:else}
          <div
            class="alert alert-success text-sm space-y-1"
            data-testid="barrier-strategy-auto"
          >
            <div>
              <div class="font-semibold">Auto-init at boot</div>
              <div class="text-xs text-base-content/60">
                The {status.strategy} provider seals and unseals the barrier
                automatically. No operator action is required.
              </div>
              {#if !status.sealed}
                <div class="text-xs text-success mt-1" data-testid="barrier-auto-unseal-active">
                  Auto-unseal active
                </div>
              {/if}
            </div>
          </div>
        {/if}

        <!-- Detail grid (always visible when status loaded) -->
        <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div>
            <div class="text-base-content/50 text-xs mb-1">Initialized</div>
            <div class="font-medium">{status.initialized ? 'Yes' : 'No'}</div>
          </div>
          <div>
            <div class="text-base-content/50 text-xs mb-1">Threshold</div>
            <div class="font-medium">{status.threshold}</div>
          </div>
          <div>
            <div class="text-base-content/50 text-xs mb-1">Total shares</div>
            <div class="font-medium">{status.total_shares}</div>
          </div>
          <div>
            <div class="text-base-content/50 text-xs mb-1">Version</div>
            <div class="font-mono text-xs">{status.version}</div>
          </div>
        </div>
      {/if}

      <div class="flex gap-2 mt-2">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { void loadStatus(); }}
          disabled={loading}
        >
          Refresh
        </button>
      </div>
    </div>
  </div>
</div>
