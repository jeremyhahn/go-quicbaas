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
  ConnectionView shows the transport and daemon address this UI's SDK client
  uses, and lets the user point the REST client at another daemon. The choice
  is kept in browser storage; nothing is sent to the server. Only REST works
  in a browser today, so the other transports are listed but disabled. Under
  Wails the transport is 'wails' and the view is read-only. An address on
  another origin is saved only after a probe of its readiness endpoint answers.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import type { Component } from 'svelte';
  import { Settings } from 'lucide-svelte';
  import {
    connectionStore,
    setConnected,
    isValidBaseURL,
    normalizeBaseURL,
    probeDaemon,
    saveConnectionOverride,
    clearConnectionOverride,
  } from '../../stores/connection.svelte.ts';
  import { activeConnection, resetSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';

  const UNAVAILABLE = 'not available in the browser yet';
  const BROWSER_STUBS = ['grpc-web', 'quic', 'mcp'];
  // lucide-svelte's class component type does not match Svelte 5's Component.
  const HeaderIcon = Settings as unknown as Component<{ class?: string }>;

  const initial = activeConnection();
  let active = $state(initial);
  let addressInput = $state(initial.address);
  let checking = $state(false);
  let applyError: string | null = $state(null);

  const isWails = $derived(active.transport === 'wails');
  const valid = $derived(isValidBaseURL(addressInput));
  const unchanged = $derived(valid && normalizeBaseURL(addressInput) === active.address);
  const canApply = $derived(!isWails && valid && !unchanged && !checking);

  function refresh(): void {
    active = activeConnection();
    addressInput = active.address;
  }

  async function apply(event?: Event): Promise<void> {
    event?.preventDefault();
    if (!canApply) return;
    applyError = null;
    const origin = normalizeBaseURL(addressInput);
    if (origin !== window.location.origin) {
      const ok = window.confirm(`Requests and your session token will go to ${origin}. Continue?`);
      if (!ok) return;
      checking = true;
      const reachable = await probeDaemon(origin);
      checking = false;
      if (!reachable) {
        applyError = `${origin} cannot be reached from this page. The page only connects to its own origin unless its content security policy allows another, and that daemon must list this page's origin in its CORS origins.`;
        return;
      }
    }
    try {
      saveConnectionOverride({ transport: 'rest', baseURL: origin });
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
      return;
    }
    resetSDKClient();
    setConnected(origin, 'rest');
    refresh();
    showToast('success', 'Connection updated');
  }

  function resetToDefault(): void {
    applyError = null;
    clearConnectionOverride();
    resetSDKClient();
    setConnected(window.location.origin, 'rest');
    refresh();
    showToast('success', 'Connection reset to default');
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Connection" subtitle="Transport and daemon address used by this UI" icon={HeaderIcon} />

  <div class="card bg-base-200 shadow-sm" data-testid="connection-current">
    <div class="card-body py-4 gap-2">
      <h2 class="card-title text-base">Current</h2>
      <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
        <dt class="text-base-content/60">Transport</dt>
        <dd class="font-mono" data-testid="connection-transport">{active.transport}</dd>
        <dt class="text-base-content/60">Address</dt>
        <dd class="font-mono break-all" data-testid="connection-address">{active.address || '—'}</dd>
        <dt class="text-base-content/60">Status</dt>
        <dd>
          {#if connectionStore.connected}
            <span class="badge badge-success badge-sm">connected</span>
          {:else}
            <span class="badge badge-error badge-sm">disconnected</span>
          {/if}
        </dd>
        <dt class="text-base-content/60">Source</dt>
        <dd data-testid="connection-source">{active.overridden ? 'Custom' : 'Default'}</dd>
      </dl>
    </div>
  </div>

  <form class="card bg-base-200 shadow-sm" onsubmit={apply}>
    <div class="card-body py-4 gap-4">
      <label class="form-control w-full max-w-lg">
        <span class="label-text mb-1">Transport</span>
        <select id="connection-transport" class="select select-bordered select-sm" disabled={isWails}>
          {#if isWails}
            <option value="wails" selected>wails</option>
          {:else}
            <option value="rest" selected>rest</option>
            {#each BROWSER_STUBS as t (t)}
              <option value={t} disabled>{t} ({UNAVAILABLE})</option>
            {/each}
          {/if}
        </select>
      </label>

      <label class="form-control w-full max-w-lg">
        <span class="label-text mb-1">Daemon address</span>
        <input
          id="connection-address"
          type="url"
          class="input input-bordered input-sm font-mono"
          class:input-error={!isWails && !valid}
          bind:value={addressInput}
          disabled={isWails}
          aria-invalid={!isWails && !valid}
          oninput={() => { applyError = null; }}
        />
        {#if !isWails && !valid}
          <span class="text-error text-xs mt-1" role="alert" data-testid="connection-address-error">
            Enter an http or https address, for example https://host:8443
          </span>
        {/if}
        <span class="text-xs text-base-content/60 mt-1">Sign-in and the barrier, bootstrap and ceremony views keep using this page's daemon.</span>
      </label>

      <div class="flex gap-2">
        <button type="submit" class="btn btn-primary btn-sm" disabled={!canApply}>
          {#if checking}<span class="loading loading-spinner loading-xs"></span>{/if}
          Apply
        </button>
        {#if active.overridden}
          <button type="button" class="btn btn-ghost btn-sm" onclick={resetToDefault}>Reset to default</button>
        {/if}
      </div>
      {#if applyError}
        <div class="alert alert-error text-sm" role="alert" data-testid="connection-apply-error">{applyError}</div>
      {/if}
    </div>
  </form>
</div>
