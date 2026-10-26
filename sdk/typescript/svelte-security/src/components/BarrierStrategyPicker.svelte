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
  BarrierStrategyPicker is the initialization entry point for a (per-tenant or
  system) barrier. The operator chooses how the barrier protects its master
  key — no encryption, a single passphrase, or an M-of-N Shamir quorum — and
  supplies the strategy-specific parameters. On submit it emits a fully formed
  init request; the parent owns the SDK call so this component stays purely
  presentational and reusable across the qrdb and quicbaas frontends.
-->
<script lang="ts">
  import type {
    TenantBarrierStrategy,
    TenantBarrierInitParams,
  } from '../types.ts';

  interface Props {
    /** Disables the form while an init request is in flight. */
    submitting?: boolean;
    /** Emitted when the operator submits a valid strategy + parameters. */
    oninit?: (params: TenantBarrierInitParams) => void;
  }

  const { submitting = false, oninit }: Props = $props();

  let strategy: TenantBarrierStrategy = $state('passphrase');
  let passphrase = $state('');
  let confirmPassphrase = $state('');
  let threshold = $state(2);
  let total = $state(3);

  interface StrategyMeta {
    label: string;
    description: string;
  }

  const strategyMeta: Record<TenantBarrierStrategy, StrategyMeta> = {
    none: {
      label: 'None',
      description:
        'No encryption barrier. Data is stored unprotected — use only when an outer barrier already protects this tenant.',
    },
    passphrase: {
      label: 'Passphrase',
      description:
        'A single operator passphrase derives the master key. The barrier must be unsealed with the same passphrase after a restart.',
    },
    shamir: {
      label: 'Shamir (M-of-N)',
      description:
        'The master key is split into N shares; any M of them reconstruct it. Each share is sealed to a separate holder, who claims it individually.',
    },
  };

  const currentMeta = $derived(strategyMeta[strategy]);

  // valid reflects whether the strategy-specific inputs are complete enough to
  // submit. The parent's submit button binds to this so the form cannot emit a
  // malformed init request.
  const valid = $derived.by((): boolean => {
    if (strategy === 'none') return true;
    if (strategy === 'passphrase') {
      return passphrase.trim().length > 0 && passphrase === confirmPassphrase;
    }
    // shamir
    return (
      Number.isInteger(threshold) &&
      Number.isInteger(total) &&
      threshold >= 1 &&
      total >= threshold
    );
  });

  function emit(): void {
    if (oninit === undefined || !valid) return;
    if (strategy === 'none') {
      oninit({ strategy: 'none' });
      return;
    }
    if (strategy === 'passphrase') {
      oninit({ strategy: 'passphrase', passphrase: passphrase.trim() });
      return;
    }
    oninit({ strategy: 'shamir', threshold, shares: total });
  }
</script>

<div class="space-y-4" data-testid="barrier-strategy-picker">
  <fieldset class="space-y-2">
    <legend class="text-sm font-semibold">Barrier strategy</legend>
    {#each Object.entries(strategyMeta) as [key, meta] (key)}
      {@const s = key as TenantBarrierStrategy}
      <label class="flex items-start gap-3 cursor-pointer">
        <input
          type="radio"
          class="radio radio-primary radio-sm mt-0.5"
          name="barrier-strategy"
          value={s}
          checked={strategy === s}
          onchange={() => { strategy = s; }}
          data-testid="barrier-strategy-radio-{s}"
        />
        <span class="flex flex-col">
          <span class="font-medium text-sm">{meta.label}</span>
          <span class="text-xs text-base-content/60">{meta.description}</span>
        </span>
      </label>
    {/each}
  </fieldset>

  {#if strategy === 'passphrase'}
    <div class="space-y-2" data-testid="barrier-strategy-passphrase-fields">
      <div class="form-control">
        <label class="label label-text" for="barrier-init-passphrase">Passphrase</label>
        <input
          id="barrier-init-passphrase"
          class="input input-bordered input-sm font-mono"
          type="password"
          autocomplete="new-password"
          placeholder="Enter a strong passphrase"
          bind:value={passphrase}
        />
      </div>
      <div class="form-control">
        <label class="label label-text" for="barrier-init-passphrase-confirm">
          Confirm passphrase
        </label>
        <input
          id="barrier-init-passphrase-confirm"
          class="input input-bordered input-sm font-mono"
          type="password"
          autocomplete="new-password"
          placeholder="Re-enter the passphrase"
          bind:value={confirmPassphrase}
        />
      </div>
      {#if confirmPassphrase.length > 0 && passphrase !== confirmPassphrase}
        <p class="text-xs text-error">Passphrases do not match.</p>
      {/if}
    </div>
  {:else if strategy === 'shamir'}
    <div class="grid grid-cols-2 gap-3" data-testid="barrier-strategy-shamir-fields">
      <div class="form-control">
        <label class="label label-text" for="barrier-init-threshold">Threshold (M)</label>
        <input
          id="barrier-init-threshold"
          class="input input-bordered input-sm"
          type="number"
          min="1"
          bind:value={threshold}
        />
      </div>
      <div class="form-control">
        <label class="label label-text" for="barrier-init-total">Total shares (N)</label>
        <input
          id="barrier-init-total"
          class="input input-bordered input-sm"
          type="number"
          min="1"
          bind:value={total}
        />
      </div>
      {#if total < threshold}
        <p class="col-span-2 text-xs text-error">
          Total shares must be greater than or equal to the threshold.
        </p>
      {/if}
    </div>
  {/if}

  <button
    class="btn btn-primary btn-sm"
    onclick={emit}
    disabled={!valid || submitting}
    data-testid="barrier-strategy-submit"
  >
    {submitting ? 'Initializing...' : `Initialize (${currentMeta.label})`}
  </button>
</div>
