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

  The component collects structured per-mode material rather than a single
  opaque string so the emitted BootstrapRequest maps directly onto the
  backend service.BootstrapRequest without further interpretation.
-->
<script lang="ts">
  import type { BootstrapMethod, BootstrapRequest } from '../types.ts';

  interface Props {
    onselect?: (req: BootstrapRequest) => void;
  }

  const { onselect }: Props = $props();

  // Use the explicit generic so TypeScript treats method as the full union,
  // not the literal type of the initial value.
  let method = $state<BootstrapMethod>('spki-pin');
  let serverUrl = $state('');

  // Per-mode material — each field persists across method switches so the
  // operator can compare methods without re-entering material.
  let spkiPin = $state('');
  let daneHostname = $state('');
  let daneDnsServer = $state('');
  let noiseStaticKey = $state('');
  let noiseAddr = $state('');

  interface MethodMeta {
    label: string;
    description: string;
    requiresMaterial: boolean;
  }

  const methodMeta: Record<BootstrapMethod, MethodMeta> = {
    'spki-pin': {
      label: 'SPKI Pin',
      description: 'Trust on first sight, verified against an out-of-band SHA-256 SPKI hash.',
      requiresMaterial: true,
    },
    'dane': {
      label: 'DANE',
      description: 'Trust derived from a DNSSEC-signed TLSA record.',
      requiresMaterial: true,
    },
    'noise': {
      label: 'Noise',
      description: "Trust the daemon's long-term Noise static public key.",
      requiresMaterial: true,
    },
    'direct': {
      label: 'Direct',
      description: 'Transport already enforces trust (localhost, mTLS, etc.). No pin required.',
      requiresMaterial: false,
    },
  };

  const currentMeta = $derived(methodMeta[method]);

  // computeReady is extracted as a named function so TypeScript evaluates each
  // case in isolation rather than applying aggressive control-flow narrowing
  // across the entire boolean expression inside $derived.
  function computeReady(): boolean {
    if (!serverUrl.trim()) return false;
    switch (method) {
      case 'direct': return true;
      case 'spki-pin': return spkiPin.trim() !== '';
      case 'dane': return daneHostname.trim() !== '';
      case 'noise': return noiseStaticKey.trim() !== '';
      default: return false;
    }
  }

  // ready is true when the minimum required fields for the current method are
  // non-empty. Drives the disabled state of the Bootstrap button reactively.
  const ready = $derived(computeReady());

  function emit(): void {
    if (onselect === undefined || !ready) return;
    const req: BootstrapRequest = { method, server_url: serverUrl.trim() };
    if (method === 'spki-pin') {
      req.spki_pin = spkiPin.trim();
    } else if (method === 'dane') {
      req.hostname = daneHostname.trim();
      if (daneDnsServer.trim()) req.dns_server = daneDnsServer.trim();
    } else if (method === 'noise') {
      req.noise_static_key = noiseStaticKey.trim();
      if (noiseAddr.trim()) req.noise_addr = noiseAddr.trim();
    }
    onselect(req);
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
      aria-label="Server URL"
      bind:value={serverUrl}
    />
  </div>

  {#if method === 'spki-pin'}
    <div class="form-control">
      <label class="label label-text" for="bootstrap-spki-pin">SPKI Pin value</label>
      <input
        id="bootstrap-spki-pin"
        class="input input-bordered input-sm font-mono text-xs"
        type="text"
        placeholder="abcdef0123...  (64 hex chars, SHA-256 of server SPKI)"
        aria-label="SPKI Pin value"
        bind:value={spkiPin}
      />
    </div>
  {:else if method === 'dane'}
    <div class="form-control">
      <label class="label label-text" for="bootstrap-dane-hostname">DANE Hostname</label>
      <input
        id="bootstrap-dane-hostname"
        class="input input-bordered input-sm font-mono text-xs"
        type="text"
        placeholder="kms.example.com"
        aria-label="DANE Hostname"
        bind:value={daneHostname}
      />
    </div>
    <div class="form-control">
      <label class="label label-text" for="bootstrap-dns-server">
        DNS Server <span class="text-base-content/50">(optional)</span>
      </label>
      <input
        id="bootstrap-dns-server"
        class="input input-bordered input-sm font-mono text-xs"
        type="text"
        placeholder="8.8.8.8:53"
        aria-label="DNS Server (optional)"
        bind:value={daneDnsServer}
      />
    </div>
  {:else if method === 'noise'}
    <div class="form-control">
      <label class="label label-text" for="bootstrap-noise-static-key">Noise Static Key</label>
      <input
        id="bootstrap-noise-static-key"
        class="input input-bordered input-sm font-mono text-xs"
        type="text"
        placeholder="abcd1234...  (64 hex chars, 32-byte Curve25519 public key)"
        aria-label="Noise Static Key"
        bind:value={noiseStaticKey}
      />
    </div>
    <div class="form-control">
      <label class="label label-text" for="bootstrap-noise-addr">
        Bootstrap Address <span class="text-base-content/50">(optional)</span>
      </label>
      <input
        id="bootstrap-noise-addr"
        class="input input-bordered input-sm font-mono text-xs"
        type="text"
        placeholder="daemon.example.com:9444"
        aria-label="Bootstrap Address (optional)"
        bind:value={noiseAddr}
      />
    </div>
  {:else}
    <p class="text-xs text-base-content/50">
      No additional material required for {currentMeta.label}.
    </p>
  {/if}

  <button
    class="btn btn-primary btn-sm"
    onclick={emit}
    disabled={!ready}
  >
    Bootstrap
  </button>
</div>
