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
  SignDigestModal signs a raw digest with one SSH key through keys.sign
  (POST /api/v1/ssh/keys/{scope}/{owner}/{name}/sign).

  The digest is entered as hex (even length, 2-128 hex characters) and sent
  as a number[] of its bytes: the server reads a Go []byte, which JSON carries
  as base64 or as an array of numbers, and a Uint8Array would serialize as an
  object. The signature blob arrives as a base64 string (Go marshals []byte
  that way) although the SDK types it number[]; a string is shown as is and
  an array is base64-encoded.

  Props:
    key     — The SSHKey to sign with.
    scope   — The key's scope.
    owner   — The key's owner.
    onClose — Called when the modal is dismissed.
-->
<script lang="ts">
  import { getSDKClient } from '../../../stores/sdkClient.ts';
  import { showToast } from '../../../stores/toast.svelte.ts';
  import type { SSHKey, SSHScope } from '@quicbaas/sdk';

  let {
    key,
    scope,
    owner,
    onClose,
  }: {
    /** The SSH key to sign with. */
    key: SSHKey;
    /** The key's scope. */
    scope: SSHScope;
    /** The key's owner. */
    owner: string;
    /** Called when the modal should close. */
    onClose: () => void;
  } = $props();

  let digestHex = $state('');
  let signing = $state(false);
  let error: string | null = $state(null);
  let format: string | null = $state(null);
  let signature: string | null = $state(null);

  const HEX = /^[0-9a-fA-F]+$/;

  /** Parses an even-length hex string of 2-128 chars into bytes, or null. */
  function parseHex(raw: string): number[] | null {
    const s = raw.trim();
    if (s.length < 2 || s.length > 128 || s.length % 2 !== 0 || !HEX.test(s)) return null;
    const bytes: number[] = [];
    for (let i = 0; i < s.length; i += 2) {
      bytes.push(parseInt(s.slice(i, i + 2), 16));
    }
    return bytes;
  }

  function toBase64(bytes: number[]): string {
    let bin = '';
    for (const b of bytes) bin += String.fromCharCode(b & 0xff);
    return btoa(bin);
  }

  /** Renders the blob: a string as is, an array of bytes as base64. */
  function renderBlob(blob: unknown): string {
    if (typeof blob === 'string') return blob;
    if (Array.isArray(blob)) return toBase64(blob.map((b) => Number(b)));
    return '';
  }

  async function submit(): Promise<void> {
    error = null;
    format = null;
    signature = null;
    const digest = parseHex(digestHex);
    if (digest === null) {
      error = 'Digest must be 2-128 hex characters of even length';
      return;
    }
    signing = true;
    try {
      const client = await getSDKClient();
      const resp = await client.ssh.keys.sign(scope, owner, key.name, { digest });
      format = resp.format;
      signature = renderBlob(resp.blob);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Sign failed';
    } finally {
      signing = false;
    }
  }

  async function copySignature(): Promise<void> {
    try {
      await navigator.clipboard.writeText(signature ?? '');
      showToast('success', 'Signature copied');
    } catch {
      showToast('error', 'Copy failed — check clipboard permissions');
    }
  }
</script>

<div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Sign a digest with ${key.name}`}>
  <div class="modal-box max-w-2xl">
    <h3 class="text-base font-semibold mb-1">Sign a Digest</h3>
    <p class="text-sm text-base-content/50 mb-4">
      <span class="font-mono">{key.name}</span>
      {#if key.algorithm}
        &nbsp;·&nbsp; {key.algorithm}
      {/if}
    </p>

    {#if error}
      <div class="alert alert-error text-sm mb-3" role="alert">{error}</div>
    {/if}

    <div class="form-control">
      <label class="label label-text" for="sign-digest-hex">
        Digest (hex) <span class="text-error ml-0.5" aria-hidden="true">*</span>
      </label>
      <input
        id="sign-digest-hex"
        class="input input-bordered input-sm font-mono"
        type="text"
        placeholder="e3b0c44298fc1c14…"
        bind:value={digestHex}
        aria-required="true"
      />
    </div>

    {#if signature !== null}
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm mt-4">
        <dt class="text-base-content/50">Format</dt><dd class="font-mono" aria-label="Signature format">{format}</dd>
      </dl>
      <pre
        class="bg-base-200 rounded-lg p-4 mt-2 text-xs font-mono whitespace-pre-wrap break-all"
        aria-label="Signature (base64)"
      >{signature}</pre>
    {/if}

    <div class="modal-action">
      <button class="btn btn-ghost btn-sm" onclick={onClose} aria-label="Close sign digest dialog">
        Close
      </button>
      {#if signature !== null}
        <button class="btn btn-ghost btn-sm" onclick={copySignature} aria-label="Copy signature">
          Copy
        </button>
      {/if}
      <button
        class="btn btn-primary btn-sm"
        disabled={signing}
        onclick={submit}
        aria-label="Sign digest"
      >
        {signing ? 'Signing…' : 'Sign'}
      </button>
    </div>
  </div>
  <div class="modal-backdrop" role="none" onclick={onClose} onkeydown={onClose}></div>
</div>
