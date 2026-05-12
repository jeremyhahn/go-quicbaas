<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  BootstrapMethodPicker is the entry point of the auto-bootstrap wizard.
  Operators choose how the client should establish trust in the daemon at
  first contact: SPKI pin, DANE TLSA record, Noise static key, or direct
  (transport-trusted, e.g. localhost / pre-shared TLS).
-->
<script lang="ts">
  import type { BootstrapMethod, BootstrapRequest } from '../types.ts';

  interface Props {
    onselect?: (req: BootstrapRequest) => void;
  }

  const { onselect }: Props = $props();

  let method: BootstrapMethod = $state('spki-pin');
  let serverUrl = $state('');
  let material = $state('');

  // Map each method to a human-readable label and the placeholder we should
  // show in the trust-material textarea.
  interface MethodMeta {
    label: string;
    description: string;
    placeholder: string;
    requiresMaterial: boolean;
  }

  const methodMeta: Record<BootstrapMethod, MethodMeta> = {
    'spki-pin': {
      label: 'SPKI Pin',
      description: 'Trust on first sight, verified against an out-of-band SHA-256 SPKI hash.',
      placeholder: 'sha256/abcd1234... (hex)',
      requiresMaterial: true,
    },
    'dane': {
      label: 'DANE',
      description: 'Trust derived from a DNSSEC-signed TLSA record.',
      placeholder: '3 1 1 abcdef0123... (presentation form)',
      requiresMaterial: true,
    },
    'noise': {
      label: 'Noise',
      description: 'Trust the daemon\'s long-term Noise static public key.',
      placeholder: 'abcd1234... (hex, 32 bytes)',
      requiresMaterial: true,
    },
    'direct': {
      label: 'Direct',
      description: 'Transport already enforces trust (localhost, mTLS, etc.). No pin required.',
      placeholder: '',
      requiresMaterial: false,
    },
  };

  const currentMeta = $derived(methodMeta[method]);

  function emit(): void {
    if (onselect === undefined) return;
    if (!serverUrl.trim()) return;
    if (currentMeta.requiresMaterial && !material.trim()) return;
    onselect({
      method,
      server_url: serverUrl.trim(),
      material: material.trim(),
    });
  }
</script>

<div class="space-y-4" data-testid="bootstrap-method-picker">
  <fieldset class="space-y-2">
    <legend class="text-sm font-semibold">Bootstrap method</legend>
    {#each Object.entries(methodMeta) as [key, meta]}
      {@const m = key as BootstrapMethod}
      <label class="flex items-start gap-3 cursor-pointer">
        <input
          type="radio"
          class="radio radio-primary radio-sm mt-0.5"
          name="bootstrap-method"
          value={m}
          checked={method === m}
          onchange={() => { method = m; }}
          data-testid="bootstrap-method-radio-{m}"
        />
        <span class="flex flex-col">
          <span class="font-medium text-sm">{meta.label}</span>
          <span class="text-xs text-base-content/60">{meta.description}</span>
        </span>
      </label>
    {/each}
  </fieldset>

  <div class="form-control">
    <label class="label label-text" for="bootstrap-server-url">Server URL</label>
    <input
      id="bootstrap-server-url"
      class="input input-bordered input-sm font-mono"
      type="text"
      placeholder="https://daemon.example.com:9443"
      bind:value={serverUrl}
    />
  </div>

  {#if currentMeta.requiresMaterial}
    <div class="form-control">
      <label class="label label-text" for="bootstrap-material">
        {currentMeta.label} value
      </label>
      <textarea
        id="bootstrap-material"
        class="textarea textarea-bordered font-mono text-xs"
        rows="2"
        placeholder={currentMeta.placeholder}
        bind:value={material}
      ></textarea>
    </div>
  {:else}
    <p class="text-xs text-base-content/50">
      No additional material required for {currentMeta.label}.
    </p>
  {/if}

  <button
    class="btn btn-primary btn-sm"
    onclick={emit}
    disabled={!serverUrl.trim() || (currentMeta.requiresMaterial && !material.trim())}
  >
    Bootstrap
  </button>
</div>
