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
    BarrierHolderParams,
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
  // holders is the optional inline roster registered with a Shamir init.
  let holders: BarrierHolderParams[] = $state([]);

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
    // shamir: an inline roster is optional, but when given it names one
    // complete holder per share.
    return (
      Number.isInteger(threshold) &&
      Number.isInteger(total) &&
      threshold >= 1 &&
      total >= threshold &&
      (holders.length === 0 || (holders.length === total && holders.every(holderComplete)))
    );
  });

  function holderComplete(h: BarrierHolderParams): boolean {
    return h.username.trim().length > 0 && h.csr_pem.trim().length > 0;
  }

  function addHolder(): void {
    holders.push({ username: '', csr_pem: '' });
  }

  function removeHolder(index: number): void {
    holders.splice(index, 1);
  }

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
    if (holders.length === 0) {
      oninit({ strategy: 'shamir', threshold, shares: total });
      return;
    }
    const officers = holders.map((h) => ({ username: h.username.trim(), csr_pem: h.csr_pem.trim() }));
    oninit({ strategy: 'shamir', threshold, shares: total, officers });
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
    <div class="space-y-2" data-testid="barrier-strategy-roster">
      <p class="text-xs text-base-content/60">
        Optionally name the holders now, one per share, or register them before initializing.
        {#if holders.length > 0}{holders.length} of {total} holders.{/if}
      </p>
      {#each holders as holder, i (i)}
        <div class="grid grid-cols-2 gap-2">
          <div class="form-control">
            <label class="label label-text" for="barrier-init-holder-{i}-username">Holder {i + 1} username</label>
            <input
              id="barrier-init-holder-{i}-username"
              class="input input-bordered input-sm font-mono"
              type="text"
              bind:value={holder.username}
            />
          </div>
          <div class="form-control">
            <label class="label label-text" for="barrier-init-holder-{i}-csr">Holder {i + 1} CSR (PEM)</label>
            <textarea
              id="barrier-init-holder-{i}-csr"
              class="textarea textarea-bordered font-mono text-xs"
              rows="2"
              bind:value={holder.csr_pem}
            ></textarea>
          </div>
          <button class="btn btn-ghost btn-xs col-span-2 justify-self-end" type="button" onclick={() => removeHolder(i)}>
            Remove holder {i + 1}
          </button>
        </div>
      {/each}
      <button class="btn btn-outline btn-xs" type="button" onclick={addHolder} data-testid="barrier-strategy-add-holder">
        Add holder
      </button>
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
