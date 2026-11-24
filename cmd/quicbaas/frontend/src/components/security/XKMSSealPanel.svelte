<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  XKMSSealPanel is the Key Store page's seal and unseal section.

  Actions:
    - Check whether a backend can seal
    - Seal UTF-8 text under a key, and show the sealed envelope as JSON
    - Unseal a pasted envelope and show the plaintext as UTF-8 text, or as
      base64 when it is not UTF-8

  Data, aad and plaintext are text in the page and base64 on the wire. Both
  forms' backend fields start from the page's backend filter. Nothing is
  called on mount. A response that lands after a sign-out or another login
  is dropped, and a new session clears the passwords, the envelope and the
  plaintext. No toast or log carries data, a password or plaintext.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type {
    XKMSClient,
    XKMSSealCapabilityResponse,
    XKMSSealRequest,
    XKMSUnsealRequest,
  } from '@quicbaas/sdk';

  let { backend = '' }: { backend?: string } = $props();

  const NOT_AVAILABLE = 'Seal and unseal are not available on this connection.';
  const ENVELOPE_BYTE_FIELDS = ['nonce', 'tag', 'tpm_public', 'tpm_private', 'wrapped_dek'] as const;

  function errText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  /** Encodes UTF-8 text as base64. */
  function utf8ToBase64(text: string): string {
    let bin = '';
    for (const b of new TextEncoder().encode(text)) bin += String.fromCharCode(b);
    return btoa(bin);
  }

  /** Decodes base64 to UTF-8 text, or keeps the base64 when it is not UTF-8. */
  function base64ToText(b64: string): { text: string; utf8: boolean } {
    try {
      const bin = atob(b64);
      const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
      return { text: new TextDecoder('utf-8', { fatal: true }).decode(bytes), utf8: true };
    } catch {
      return { text: b64, utf8: false };
    }
  }

  let unavailable = $state(false);

  /** Resolves the xkms client, or null when this connection has none. */
  async function xkmsClient(): Promise<XKMSClient | null> {
    const client = await getSDKClient();
    const xkms = client.xkms ?? null;
    if (!xkms) unavailable = true;
    return xkms;
  }

  // Only finds out whether this connection has xkms; no xkms call is made.
  void xkmsClient().catch(() => {});

  // ---------------------------------------------------------------------------
  // Form state
  // ---------------------------------------------------------------------------

  let sealBackend = $state('');
  let sealKeyId = $state('');
  let sealData = $state('');
  let sealAad = $state('');
  let sealPassword = $state('');
  let sealError = $state<string | null>(null);
  let sealing = $state(false);
  let envelopeText = $state<string | null>(null);
  let sealedKeyId = $state('');
  let sealedBackend = $state('');

  let capability = $state<XKMSSealCapabilityResponse | null>(null);
  let capError = $state<string | null>(null);
  let checking = $state(false);

  let unsealBackend = $state('');
  let unsealKeyId = $state('');
  let unsealEnvelope = $state('');
  let unsealAad = $state('');
  let unsealPassword = $state('');
  let unsealError = $state<string | null>(null);
  let unsealing = $state(false);
  let plaintext = $state<{ text: string; utf8: boolean } | null>(null);

  // Both backend fields start from the page's filter and follow it.
  $effect(() => {
    const b = backend;
    untrack(() => {
      sealBackend = b;
      unsealBackend = b;
    });
  });

  // A new session clears what the previous one entered or saw.
  let lastSession = authStore.sessionKey;
  $effect(() => {
    const key = authStore.sessionKey;
    untrack(() => {
      if (key === lastSession) return;
      lastSession = key;
      sealData = ''; sealAad = ''; sealPassword = ''; sealError = null;
      envelopeText = null; sealedKeyId = ''; sealedBackend = '';
      capability = null; capError = null;
      unsealEnvelope = ''; unsealAad = ''; unsealPassword = ''; unsealError = null;
      plaintext = null;
    });
  });

  // ---------------------------------------------------------------------------
  // Capability
  // ---------------------------------------------------------------------------

  async function check(): Promise<void> {
    const forSession = authStore.sessionKey;
    const b = sealBackend.trim();
    capError = null;
    capability = null;
    checking = true;
    try {
      const xkms = await xkmsClient();
      if (forSession !== authStore.sessionKey || !xkms) return;
      const res = b === '' ? await xkms.canSeal() : await xkms.canSeal(b);
      if (forSession !== authStore.sessionKey) return;
      capability = res;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else capError = errText(e, 'Failed to check the seal capability');
    } finally {
      checking = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Seal
  // ---------------------------------------------------------------------------

  async function seal(): Promise<void> {
    const b = sealBackend.trim();
    const keyId = sealKeyId.trim();
    if (b === '') { sealError = 'Backend is required.'; return; }
    if (keyId === '') { sealError = 'Key ID is required.'; return; }
    if (sealData === '') { sealError = 'Data is required.'; return; }
    const req: XKMSSealRequest = { backend: b, key_id: keyId, data: utf8ToBase64(sealData) };
    if (sealAad !== '') req.aad = utf8ToBase64(sealAad);
    if (sealPassword !== '') req.password = sealPassword;
    const forSession = authStore.sessionKey;
    sealError = null;
    envelopeText = null;
    sealing = true;
    try {
      const xkms = await xkmsClient();
      if (forSession !== authStore.sessionKey || !xkms) return;
      const res = await xkms.seal(req);
      if (forSession !== authStore.sessionKey) return;
      envelopeText = JSON.stringify(res, null, 2);
      sealedKeyId = res.key_id || keyId;
      sealedBackend = res.backend || b;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else sealError = errText(e, 'Failed to seal');
    } finally {
      sealing = false;
    }
  }

  function useForUnseal(): void {
    if (envelopeText === null) return;
    unsealEnvelope = envelopeText;
    unsealBackend = sealedBackend;
    unsealKeyId = sealedKeyId;
    unsealError = null;
    plaintext = null;
  }

  // ---------------------------------------------------------------------------
  // Unseal
  // ---------------------------------------------------------------------------

  /** Builds the unseal request from the envelope's known fields only. */
  function buildUnseal(b: string, keyId: string): XKMSUnsealRequest | null {
    let env: unknown;
    try {
      env = JSON.parse(unsealEnvelope);
    } catch {
      unsealError = 'The sealed envelope must be JSON.';
      return null;
    }
    if (env === null || typeof env !== 'object' || Array.isArray(env)) {
      unsealError = 'The sealed envelope must be a JSON object.';
      return null;
    }
    const o = env as Record<string, unknown>;
    if (typeof o.ciphertext !== 'string' || o.ciphertext === '') {
      unsealError = 'The sealed envelope needs a ciphertext.';
      return null;
    }
    const req: XKMSUnsealRequest = { backend: b, key_id: keyId, ciphertext: o.ciphertext };
    for (const f of ENVELOPE_BYTE_FIELDS) {
      const v = o[f];
      if (typeof v === 'string' && v !== '') req[f] = v;
    }
    if (typeof o.key_version === 'number') req.key_version = o.key_version;
    const m = o.metadata;
    if (m !== null && typeof m === 'object' && !Array.isArray(m)) {
      const metadata: Record<string, string> = {};
      for (const [k, v] of Object.entries(m)) if (typeof v === 'string') metadata[k] = v;
      req.metadata = metadata;
    }
    if (unsealAad !== '') req.aad = utf8ToBase64(unsealAad);
    if (unsealPassword !== '') req.password = unsealPassword;
    return req;
  }

  async function unseal(): Promise<void> {
    const b = unsealBackend.trim();
    const keyId = unsealKeyId.trim();
    if (b === '') { unsealError = 'Backend is required.'; return; }
    if (keyId === '') { unsealError = 'Key ID is required.'; return; }
    const req = buildUnseal(b, keyId);
    if (req === null) return;
    const forSession = authStore.sessionKey;
    unsealError = null;
    plaintext = null;
    unsealing = true;
    try {
      const xkms = await xkmsClient();
      if (forSession !== authStore.sessionKey || !xkms) return;
      const res = await xkms.unseal(req);
      if (forSession !== authStore.sessionKey) return;
      plaintext = base64ToText(res.plaintext);
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else unsealError = errText(e, 'Failed to unseal');
    } finally {
      unsealing = false;
    }
  }
</script>

<div class="space-y-3">
  <h2 class="text-base font-semibold">Seal and Unseal</h2>

  {#if unavailable}
    <div class="alert alert-info text-sm" data-testid="xkms-seal-not-available">{NOT_AVAILABLE}</div>
  {:else}
    <div class="grid gap-4 lg:grid-cols-2">
      <div class="card bg-base-100 border border-base-300">
        <div class="card-body p-4 space-y-3">
          <h3 class="font-semibold text-sm">Seal</h3>
          {#if sealError}
            <div class="alert alert-error text-sm" role="alert">{sealError}</div>
          {/if}
          <div class="form-control">
            <label class="label label-text" for="xkms-seal-backend">Seal backend</label>
            <div class="flex gap-2">
              <input id="xkms-seal-backend" class="input input-bordered input-sm flex-1" type="text" bind:value={sealBackend} />
              <button class="btn btn-sm" disabled={checking} onclick={() => void check()}>Check seal capability</button>
            </div>
          </div>
          {#if capError}
            <div class="alert alert-error text-sm" role="alert">{capError}</div>
          {/if}
          {#if capability}
            <div class="text-xs space-y-1" data-testid="seal-capability">
              <div>Can seal: <span class="font-semibold">{capability.can_seal ? 'Yes' : 'No'}</span></div>
              <div>Backend: <span class="font-mono">{capability.backend || '—'}</span></div>
              {#if capability.message}<div>{capability.message}</div>{/if}
            </div>
          {/if}
          <div class="form-control">
            <label class="label label-text" for="xkms-seal-key">Seal key ID</label>
            <input id="xkms-seal-key" class="input input-bordered input-sm" type="text" bind:value={sealKeyId} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-seal-data">Data</label>
            <textarea id="xkms-seal-data" class="textarea textarea-bordered text-xs h-20" bind:value={sealData}></textarea>
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-seal-aad">Seal AAD</label>
            <input id="xkms-seal-aad" class="input input-bordered input-sm" type="text" bind:value={sealAad} placeholder="optional" />
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-seal-password">Seal password</label>
            <input id="xkms-seal-password" class="input input-bordered input-sm" type="password" autocomplete="off" bind:value={sealPassword} placeholder="optional" />
          </div>
          <div class="flex justify-end">
            <button class="btn btn-primary btn-sm" disabled={sealing} onclick={() => void seal()}>Seal</button>
          </div>
          {#if envelopeText !== null}
            <div class="form-control">
              <label class="label label-text" for="xkms-sealed-envelope">Sealed envelope</label>
              <textarea id="xkms-sealed-envelope" class="textarea textarea-bordered font-mono text-xs h-40" readonly
                data-testid="sealed-envelope" value={envelopeText}></textarea>
            </div>
            <div class="flex justify-end">
              <button class="btn btn-sm" onclick={useForUnseal}>Use for unseal</button>
            </div>
          {/if}
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300">
        <div class="card-body p-4 space-y-3">
          <h3 class="font-semibold text-sm">Unseal</h3>
          {#if unsealError}
            <div class="alert alert-error text-sm" role="alert">{unsealError}</div>
          {/if}
          <div class="form-control">
            <label class="label label-text" for="xkms-unseal-backend">Unseal backend</label>
            <input id="xkms-unseal-backend" class="input input-bordered input-sm" type="text" bind:value={unsealBackend} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-unseal-key">Unseal key ID</label>
            <input id="xkms-unseal-key" class="input input-bordered input-sm" type="text" bind:value={unsealKeyId} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-unseal-envelope">Sealed envelope (JSON)</label>
            <textarea id="xkms-unseal-envelope" class="textarea textarea-bordered font-mono text-xs h-40" bind:value={unsealEnvelope}
              placeholder={'{"ciphertext": "..."}'}></textarea>
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-unseal-aad">Unseal AAD</label>
            <input id="xkms-unseal-aad" class="input input-bordered input-sm" type="text" bind:value={unsealAad} placeholder="optional" />
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-unseal-password">Unseal password</label>
            <input id="xkms-unseal-password" class="input input-bordered input-sm" type="password" autocomplete="off" bind:value={unsealPassword} placeholder="optional" />
          </div>
          <div class="flex justify-end">
            <button class="btn btn-primary btn-sm" disabled={unsealing} onclick={() => void unseal()}>Unseal</button>
          </div>
          {#if plaintext}
            <div>
              <div class="text-xs font-semibold mb-1">Plaintext{plaintext.utf8 ? '' : ' (base64, not UTF-8)'}</div>
              <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="unsealed-plaintext">{plaintext.text}</pre>
            </div>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</div>
