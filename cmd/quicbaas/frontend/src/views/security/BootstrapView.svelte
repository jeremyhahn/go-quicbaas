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
  BootstrapView is the auto-bootstrap wizard. The operator picks one of four
  trust mechanisms (SPKI pin, DANE, Noise, direct), supplies the server URL,
  pastes the trust material, and submits.

  All HTTP traffic flows through the typed @quicbaas/sdk BootstrapClient
  (RestBootstrapClient) which maps each method to /api/v1/bootstrap/{method}.
  This view never hand-rolls /api/v1/bootstrap URLs.
-->
<script lang="ts">
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { Compass } from 'lucide-svelte';
  import { createBootstrapClient, isWailsRuntime } from '@quicbaas/sdk';
  import type { BootstrapClient as SDKBootstrapClient, BootstrapBundleRequest } from '@quicbaas/sdk';
  import { BootstrapMethodPicker } from '@quicbaas/svelte-security';
  import type {
    BootstrapMethod,
    BootstrapRequest,
    BootstrapResponse,
  } from '@quicbaas/svelte-security';

  // ---------------------------------------------------------------------------
  // SDK -> structural BootstrapResponse adapter
  //
  // The svelte-security BootstrapMethodPicker emits a structural request:
  //
  //   {method: 'spki-pin'|'dane'|'noise'|'direct', server_url, material}
  //
  // The SDK BootstrapClient exposes typed POSTs to /api/v1/bootstrap/{method}
  // where method ∈ {auto, dane, noise, spki}. We translate the picker's
  // method enum into the SDK call. The resulting BootstrapBundleResponse
  // is folded into the structural BootstrapResponse the view renders.
  // ---------------------------------------------------------------------------

  function resolveTransport(): 'wails' | 'rest' {
    return isWailsRuntime() ? 'wails' : 'rest';
  }

  let sdkClient: SDKBootstrapClient | null = $state(null);

  async function getSdkClient(): Promise<SDKBootstrapClient> {
    if (sdkClient !== null) return sdkClient;
    const baseURL = typeof window !== 'undefined' ? window.location.origin : '';
    sdkClient = await createBootstrapClient(resolveTransport(), { baseURL });
    return sdkClient;
  }

  // hashFingerprint computes a sha256 hex digest of the bundle PEM so the
  // result panel can show a deterministic fingerprint. Browsers expose
  // crypto.subtle which we use here; tests run under jsdom which also
  // supports it via Node's webcrypto polyfill.
  async function hashFingerprint(text: string): Promise<string | undefined> {
    try {
      if (typeof crypto === 'undefined' || crypto.subtle === undefined) return undefined;
      const data = new TextEncoder().encode(text);
      const digest = await crypto.subtle.digest('SHA-256', data);
      return Array.from(new Uint8Array(digest))
        .map((b) => b.toString(16).padStart(2, '0'))
        .join('');
    } catch {
      return undefined;
    }
  }

  async function dispatch(req: BootstrapRequest): Promise<BootstrapResponse> {
    const client = await getSdkClient();

    // Build the typed request body the backend decodes. server_url is required
    // by every mode; per-mode material fields are forwarded as-is from the
    // picker so the servicer can apply the right verification strategy.
    const bundleReq: BootstrapBundleRequest = {
      server_url: req.server_url,
      hostname: req.hostname,
      dns_server: req.dns_server,
      noise_static_key: req.noise_static_key,
      noise_addr: req.noise_addr,
      spki_pin: req.spki_pin,
    };

    let bundle;
    switch (req.method) {
      case 'spki-pin':
        bundle = await client.spki(bundleReq);
        break;
      case 'dane':
        bundle = await client.dane(bundleReq);
        break;
      case 'noise':
        bundle = await client.noise(bundleReq);
        break;
      case 'direct':
        // "direct" means the transport already enforces trust (localhost,
        // mTLS, etc.). The SDK exposes this as the catch-all auto() method,
        // which iterates the daemon's configured bootstrap chain in priority
        // order and returns the first verified bundle.
        bundle = await client.auto(bundleReq);
        break;
      default: {
        const _exhaustive: never = req.method;
        throw new Error(`Unknown bootstrap method: ${String(_exhaustive)}`);
      }
    }
    const fingerprint = await hashFingerprint(bundle.bundle_pem);
    return {
      method: req.method as BootstrapMethod,
      server_url: req.server_url,
      fingerprint,
    };
  }

  // ---------------------------------------------------------------------------
  // Reactive state
  // ---------------------------------------------------------------------------

  let lastResult: BootstrapResponse | null = $state(null);
  let lastError: string | null = $state(null);
  let submitting = $state(false);

  async function onSelect(req: BootstrapRequest): Promise<void> {
    lastError = null;
    submitting = true;
    try {
      const resp = await dispatch(req);
      lastResult = resp;
      showToast({
        message: `Bootstrap via ${resp.method} succeeded`,
        type: 'success',
      });
    } catch (e) {
      lastError = e instanceof Error ? e.message : 'Bootstrap failed';
      showToast({ message: lastError, type: 'error' });
    } finally {
      submitting = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader
    title="Bootstrap"
    subtitle="First-contact trust establishment"
    icon={Compass}
  />

  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      <p class="text-sm text-base-content/70">
        Pick how the client should trust the daemon at first contact. The
        chosen mechanism becomes the trust anchor for all subsequent SDK
        traffic; rotate or revoke it from the Barrier view.
      </p>

      {#if lastError !== null}
        <div class="alert alert-error text-sm" role="alert">{lastError}</div>
      {/if}

      {#if lastResult !== null}
        <div class="alert alert-success text-sm" role="alert" data-testid="bootstrap-result">
          <div>
            <div class="font-semibold mb-1">Bootstrap successful</div>
            <div class="font-mono text-xs">
              method: {lastResult.method}
            </div>
            <div class="font-mono text-xs">
              server_url: {lastResult.server_url}
            </div>
            {#if lastResult.fingerprint}
              <div class="font-mono text-xs break-all">
                fingerprint: {lastResult.fingerprint}
              </div>
            {/if}
          </div>
        </div>
      {/if}

      <BootstrapMethodPicker
        onselect={(req) => { void onSelect(req); }}
      />

      {#if submitting}
        <div class="text-xs text-base-content/40">
          Submitting bootstrap request...
        </div>
      {/if}
    </div>
  </div>
</div>
