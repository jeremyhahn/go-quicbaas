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
    BarrierStatus,
    BarrierStrategy,
  } from '@quicbaas/svelte-security';

  // ---------------------------------------------------------------------------
  // SDK -> structural BarrierClient adapter
  //
  // The svelte-security components consume a structurally-typed BarrierClient
  // (init({threshold, total_shares}), unseal({share}), status() →
  // BarrierStatus). The SDK BarrierClient surface is richer (initializeShamir,
  // unsealWithShare, etc.) so we adapt the SDK calls into the shape the
  // components expect. Crucially, all URL routing flows through the SDK so
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
          // to derive the master key from the active strategy.
          secret: new Uint8Array(),
          threshold: req.threshold,
          total_shares: req.total_shares,
        });
        return { shares: resp.shares };
      },
      async unseal(req) {
        const resp = await sdk.unsealWithShare({ share: req.share });
        return {
          sealed: !resp.complete,
          shares_provided: resp.submitted,
          threshold: resp.required,
        };
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
  let initShares: string[] | null = $state(null);
  let initError: string | null = $state(null);

  // Operations card state (seal, rekey, recovery keys). The typed SDK
  // client is kept next to the structural adapter so these calls go
  // through the SDK's own routes.
  let sdkClient: SDKBarrierClient | null = $state(null);
  type PendingOp = 'seal' | 'rekey' | 'delete-recovery' | null;
  let pendingOp: PendingOp = $state(null);
  let opBusy = $state(false);
  let sealSOPIN = $state('');
  let rekeyThreshold: number | null = $state(null);
  let rekeyTotal: number | null = $state(null);
  let rekeyShares: string[] | null = $state(null);
  let recoveryHasKeys: boolean | null = $state(null);
  let recoveryThreshold: number | null = $state(null);
  let recoveryTotal: number | null = $state(null);
  let recoveryKeys: string[] | null = $state(null);

  async function loadStatus(): Promise<void> {
    loading = true;
    statusError = null;
    subsystemDisabled = false;
    try {
      if (client === null) {
        const { structural, sdk } = await makeClient();
        client = structural;
        sdkClient = sdk;
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
      initShares = resp.shares;
      showToast({
        message: 'Barrier initialized — store your Shamir shares securely!',
        type: 'warning',
      });
      void loadStatus();
    } catch (e) {
      initError = e instanceof Error ? e.message : 'Initialize failed';
    } finally {
      initing = false;
    }
  }

  function errorMessage(e: unknown, fallback: string): string {
    return e instanceof Error && e.message !== '' ? e.message : fallback;
  }

  // canOperate is true when the barrier is initialized and unsealed: the
  // states in which it can be sealed, rekeyed or given recovery keys.
  let canOperate = $derived.by((): boolean => {
    const s: BarrierStatus | null = status;
    if (s === null || sdkClient === null) return false;
    return s.strategy !== 'disabled' && s.initialized && !s.sealed;
  });

  async function loadRecoveryStatus(): Promise<void> {
    if (sdkClient === null) return;
    try {
      const resp = await sdkClient.recoveryStatus();
      recoveryHasKeys = Boolean(resp.has_keys);
    } catch (e) {
      recoveryHasKeys = null;
      showToast({ message: errorMessage(e, 'Failed to load recovery key status'), type: 'error' });
    }
  }

  $effect(() => {
    if (canOperate) {
      void loadRecoveryStatus();
    }
  });

  async function confirmSeal(): Promise<void> {
    if (sdkClient === null) return;
    pendingOp = null;
    opBusy = true;
    const pin = sealSOPIN;
    try {
      await sdkClient.seal(pin !== '' ? { so_pin: pin } : undefined);
      showToast({ message: 'Barrier sealed', type: 'success' });
      void loadStatus();
    } catch (e) {
      showToast({ message: errorMessage(e, 'Seal failed'), type: 'error' });
    } finally {
      sealSOPIN = '';
      opBusy = false;
    }
  }

  function requestRekey(): void {
    if (rekeyThreshold === null || rekeyTotal === null) {
      showToast({ message: 'Enter the new threshold and total shares', type: 'error' });
      return;
    }
    pendingOp = 'rekey';
  }

  async function confirmRekey(): Promise<void> {
    if (sdkClient === null || rekeyThreshold === null || rekeyTotal === null) return;
    pendingOp = null;
    opBusy = true;
    const req = { threshold: Number(rekeyThreshold), total: Number(rekeyTotal) };
    try {
      const resp = await sdkClient.rekey(req);
      rekeyShares = resp.shares ?? [];
      showToast({ message: 'Barrier rekeyed — store the new shares securely!', type: 'warning' });
      void loadStatus();
    } catch (e) {
      showToast({ message: errorMessage(e, 'Rekey failed'), type: 'error' });
    } finally {
      rekeyThreshold = null;
      rekeyTotal = null;
      opBusy = false;
    }
  }

  async function submitGenerateRecovery(): Promise<void> {
    if (sdkClient === null) return;
    if (recoveryThreshold === null || recoveryTotal === null) {
      showToast({ message: 'Enter the recovery threshold and total keys', type: 'error' });
      return;
    }
    opBusy = true;
    const req = { threshold: Number(recoveryThreshold), total: Number(recoveryTotal) };
    try {
      const resp = await sdkClient.generateRecoveryKeys(req);
      recoveryKeys = resp.keys ?? [];
      showToast({ message: 'Recovery keys generated — store them securely!', type: 'warning' });
      void loadRecoveryStatus();
    } catch (e) {
      showToast({ message: errorMessage(e, 'Generating recovery keys failed'), type: 'error' });
    } finally {
      recoveryThreshold = null;
      recoveryTotal = null;
      opBusy = false;
    }
  }

  async function confirmDeleteRecovery(): Promise<void> {
    if (sdkClient === null) return;
    pendingOp = null;
    opBusy = true;
    try {
      await sdkClient.deleteRecoveryKeys();
      recoveryKeys = null;
      showToast({ message: 'Recovery keys deleted', type: 'success' });
    } catch (e) {
      showToast({ message: errorMessage(e, 'Deleting recovery keys failed'), type: 'error' });
    } finally {
      opBusy = false;
      void loadRecoveryStatus();
    }
  }

  async function copyValues(values: string[]): Promise<void> {
    try {
      await navigator.clipboard.writeText(values.join('\n'));
      showToast({ message: 'Copied to clipboard', type: 'success' });
    } catch (e) {
      showToast({ message: errorMessage(e, 'Copy failed'), type: 'error' });
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
                the threshold required to unseal. Shares are returned exactly once.
              </p>
              {#if initError !== null}
                <div class="alert alert-error text-sm" role="alert">{initError}</div>
              {/if}
              {#if initShares !== null}
                <div class="alert alert-warning text-sm" role="alert">
                  <div>
                    <div class="font-semibold mb-2">New shares — store securely!</div>
                    <div class="space-y-1">
                      {#each initShares as share, i}
                        <div class="font-mono text-xs break-all" data-testid="shamir-share-result">
                          <span class="text-base-content/50">#{i + 1}</span> {share}
                        </div>
                      {/each}
                    </div>
                  </div>
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

  {#snippet shownOnce(title: string, values: string[], testid: string, ondismiss: () => void)}
    <div class="alert alert-warning text-sm" role="alert" data-testid={testid}>
      <div class="w-full space-y-2">
        <div class="font-semibold">{title}</div>
        <div class="text-xs">These values are shown only once and are not shown again. Store them securely now.</div>
        <div class="space-y-1">
          {#each values as value, i}
            <div class="font-mono text-xs break-all" data-testid="{testid}-value">
              <span class="text-base-content/50">#{i + 1}</span> {value}
            </div>
          {/each}
        </div>
        <div class="flex gap-2">
          <button class="btn btn-xs" onclick={() => { void copyValues(values); }}>Copy</button>
          <button class="btn btn-xs btn-ghost" onclick={ondismiss}>Dismiss</button>
        </div>
      </div>
    </div>
  {/snippet}

  {#if canOperate}
    <div class="card bg-base-200 rounded-box" data-testid="barrier-operations">
      <div class="card-body p-6 gap-6">
        <h2 class="text-lg font-semibold">Operations</h2>

        {#if pendingOp !== null}
          <div class="alert alert-error text-sm" role="alertdialog" aria-label="Confirm barrier operation" data-testid="barrier-op-confirm">
            <div class="w-full space-y-2">
              {#if pendingOp === 'seal'}
                <p>Seal the barrier? Every service that uses the barrier stops until it is unsealed again.</p>
              {:else if pendingOp === 'rekey'}
                <p>Rekey the barrier to {rekeyThreshold} of {rekeyTotal} shares? The current shares stop working.</p>
              {:else}
                <p>Delete the recovery keys? They can no longer be used to recover the barrier.</p>
              {/if}
              <div class="flex gap-2">
                <button
                  class="btn btn-sm btn-error"
                  disabled={opBusy}
                  onclick={() => {
                    if (pendingOp === 'seal') void confirmSeal();
                    else if (pendingOp === 'rekey') void confirmRekey();
                    else void confirmDeleteRecovery();
                  }}
                >Confirm</button>
                <button class="btn btn-sm btn-ghost" onclick={() => { pendingOp = null; }}>Cancel</button>
              </div>
            </div>
          </div>
        {/if}

        <!-- Seal -->
        <section class="space-y-2" data-testid="barrier-op-seal">
          <h3 class="font-semibold">Seal</h3>
          <div class="form-control max-w-sm">
            <label class="label label-text" for="barrier-seal-so-pin">Security Officer PIN (if required)</label>
            <input
              id="barrier-seal-so-pin"
              class="input input-bordered input-sm"
              type="password"
              autocomplete="off"
              bind:value={sealSOPIN}
            />
          </div>
          <button class="btn btn-warning btn-sm" disabled={opBusy} onclick={() => { pendingOp = 'seal'; }}>
            Seal barrier
          </button>
        </section>

        <!-- Rekey -->
        <section class="space-y-2" data-testid="barrier-op-rekey">
          <h3 class="font-semibold">Rekey</h3>
          {#if rekeyShares !== null}
            {@render shownOnce('New shares', rekeyShares, 'barrier-rekey-shares', () => { rekeyShares = null; })}
          {/if}
          <form
            class="space-y-2"
            data-testid="barrier-rekey-form"
            onsubmit={(e) => { e.preventDefault(); requestRekey(); }}
          >
            <div class="grid grid-cols-2 gap-3 max-w-md">
              <div class="form-control">
                <label class="label label-text" for="barrier-rekey-threshold">New threshold</label>
                <input id="barrier-rekey-threshold" class="input input-bordered input-sm" type="number" min="1" bind:value={rekeyThreshold} />
              </div>
              <div class="form-control">
                <label class="label label-text" for="barrier-rekey-total">New total shares</label>
                <input id="barrier-rekey-total" class="input input-bordered input-sm" type="number" min="1" bind:value={rekeyTotal} />
              </div>
            </div>
            <button class="btn btn-sm" type="submit" disabled={opBusy}>Rekey barrier</button>
          </form>
        </section>

        <!-- Recovery keys -->
        <section class="space-y-2" data-testid="barrier-op-recovery">
          <h3 class="font-semibold">Recovery keys</h3>
          <p class="text-sm" data-testid="barrier-recovery-status">
            {#if recoveryHasKeys === null}
              Recovery key status unknown.
            {:else if recoveryHasKeys}
              Recovery keys exist.
            {:else}
              No recovery keys have been generated.
            {/if}
          </p>
          {#if recoveryKeys !== null}
            {@render shownOnce('Recovery keys', recoveryKeys, 'barrier-recovery-keys', () => { recoveryKeys = null; })}
          {/if}
          <form
            class="space-y-2"
            data-testid="barrier-recovery-form"
            onsubmit={(e) => { e.preventDefault(); void submitGenerateRecovery(); }}
          >
            <div class="grid grid-cols-2 gap-3 max-w-md">
              <div class="form-control">
                <label class="label label-text" for="barrier-recovery-threshold">Recovery threshold</label>
                <input id="barrier-recovery-threshold" class="input input-bordered input-sm" type="number" min="1" bind:value={recoveryThreshold} />
              </div>
              <div class="form-control">
                <label class="label label-text" for="barrier-recovery-total">Recovery keys to generate</label>
                <input id="barrier-recovery-total" class="input input-bordered input-sm" type="number" min="1" bind:value={recoveryTotal} />
              </div>
            </div>
            <div class="flex gap-2">
              <button class="btn btn-sm" type="submit" disabled={opBusy}>Generate recovery keys</button>
              {#if recoveryHasKeys}
                <button class="btn btn-sm btn-error btn-outline" type="button" disabled={opBusy} onclick={() => { pendingOp = 'delete-recovery'; }}>
                  Delete recovery keys
                </button>
              {/if}
            </div>
          </form>
        </section>
      </div>
    </div>
  {/if}
</div>
