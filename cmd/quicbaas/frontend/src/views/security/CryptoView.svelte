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
  CryptoView is an operator "crypto console" for the low-level primitives
  exposed by Security().Crypto() — encrypt, decrypt, rewrap, sign, verify,
  hash, random, and data-key generation, all backed by KMS-held keys. Maps
  to /api/v1/crypto/* (see handlers_crypto.go). HMAC and batch operations
  exist on the MCP transport but have no REST route, so they are not
  exposed here (the browser only ever talks REST).

  Every binary field on the wire (plaintext, ciphertext, message, signature,
  hash, random bytes) is base64. Text inputs are UTF-8 encoded to base64
  before the request is sent; base64 inputs (ciphertext, signature) are
  passed straight through.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import type { CryptoHashAlgorithm } from '@quicbaas/sdk';
  import { Binary, Copy } from 'lucide-svelte';

  const HASH_ALGORITHMS: CryptoHashAlgorithm[] = [
    'SHA256',
    'SHA384',
    'SHA512',
    'SHA3-256',
    'SHA3-384',
    'SHA3-512',
    'BLAKE2B-256',
    'BLAKE2B-512',
  ];

  // ---------------------------------------------------------------------------
  // Base64 <-> UTF-8 helpers
  // ---------------------------------------------------------------------------

  function utf8ToBase64(text: string): string {
    const bytes = new TextEncoder().encode(text);
    let binary = '';
    for (const b of bytes) binary += String.fromCharCode(b);
    return btoa(binary);
  }

  function base64ToUtf8(b64: string): string {
    try {
      const binary = atob(b64);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      return new TextDecoder().decode(bytes);
    } catch {
      return '';
    }
  }

  async function copyToClipboard(text: string, label: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', `${label} copied to clipboard`);
    } catch {
      showToast('error', `Failed to copy ${label}`);
    }
  }

  // ---------------------------------------------------------------------------
  // Encrypt / Decrypt
  // ---------------------------------------------------------------------------

  let encKeyName = $state('');
  let encPlaintext = $state('');
  let encResult = $state<{ ciphertext: string; key_version: number } | null>(null);
  let encError = $state('');
  let encrypting = $state(false);

  async function submitEncrypt(): Promise<void> {
    encError = '';
    encResult = null;
    if (!encKeyName.trim()) { encError = 'Key name is required.'; return; }
    encrypting = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.encrypt({
        key_name: encKeyName.trim(),
        plaintext: utf8ToBase64(encPlaintext),
      });
      encResult = { ciphertext: resp.ciphertext, key_version: resp.key_version };
    } catch (err: unknown) {
      encError = err instanceof Error ? err.message : String(err);
    } finally {
      encrypting = false;
    }
  }

  let decKeyName = $state('');
  let decCiphertext = $state('');
  let decResult = $state<{ plaintext: string; key_version: number } | null>(null);
  let decError = $state('');
  let decrypting = $state(false);

  async function submitDecrypt(): Promise<void> {
    decError = '';
    decResult = null;
    if (!decKeyName.trim() || !decCiphertext.trim()) { decError = 'Key name and ciphertext are required.'; return; }
    decrypting = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.decrypt({
        key_name: decKeyName.trim(),
        ciphertext: decCiphertext.trim(),
      });
      decResult = { plaintext: base64ToUtf8(resp.plaintext), key_version: resp.key_version };
    } catch (err: unknown) {
      decError = err instanceof Error ? err.message : String(err);
    } finally {
      decrypting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Sign / Verify
  // ---------------------------------------------------------------------------

  let signKeyName = $state('');
  let signMessage = $state('');
  let signHashAlg = $state<CryptoHashAlgorithm>('SHA256');
  let signResult = $state<{ signature: string; key_version: number } | null>(null);
  let signError = $state('');
  let signing = $state(false);

  async function submitSign(): Promise<void> {
    signError = '';
    signResult = null;
    if (!signKeyName.trim()) { signError = 'Key name is required.'; return; }
    signing = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.sign({
        key_name: signKeyName.trim(),
        message: utf8ToBase64(signMessage),
        hash_algorithm: signHashAlg,
      });
      signResult = { signature: resp.signature, key_version: resp.key_version };
    } catch (err: unknown) {
      signError = err instanceof Error ? err.message : String(err);
    } finally {
      signing = false;
    }
  }

  let verifyKeyName = $state('');
  let verifyMessage = $state('');
  let verifySignature = $state('');
  let verifyHashAlg = $state<CryptoHashAlgorithm>('SHA256');
  let verifyResult = $state<boolean | null>(null);
  let verifyError = $state('');
  let verifying = $state(false);

  async function submitVerify(): Promise<void> {
    verifyError = '';
    verifyResult = null;
    if (!verifyKeyName.trim() || !verifySignature.trim()) { verifyError = 'Key name and signature are required.'; return; }
    verifying = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.verify({
        key_name: verifyKeyName.trim(),
        message: utf8ToBase64(verifyMessage),
        signature: verifySignature.trim(),
        hash_algorithm: verifyHashAlg,
      });
      verifyResult = resp.valid;
    } catch (err: unknown) {
      verifyError = err instanceof Error ? err.message : String(err);
    } finally {
      verifying = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Hash / Random
  // ---------------------------------------------------------------------------

  let hashData = $state('');
  let hashAlgorithm = $state<CryptoHashAlgorithm>('SHA256');
  let hashResult = $state('');
  let hashError = $state('');
  let hashing = $state(false);

  async function submitHash(): Promise<void> {
    hashError = '';
    hashResult = '';
    hashing = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.hash({ data: utf8ToBase64(hashData), algorithm: hashAlgorithm });
      hashResult = resp.hash;
    } catch (err: unknown) {
      hashError = err instanceof Error ? err.message : String(err);
    } finally {
      hashing = false;
    }
  }

  let randomNumBytes = $state(32);
  let randomResult = $state('');
  let randomError = $state('');
  let generatingRandom = $state(false);

  async function submitRandom(): Promise<void> {
    randomError = '';
    randomResult = '';
    generatingRandom = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.random(randomNumBytes);
      randomResult = resp.random_bytes;
    } catch (err: unknown) {
      randomError = err instanceof Error ? err.message : String(err);
    } finally {
      generatingRandom = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Data Key
  // ---------------------------------------------------------------------------

  let dkKeyName = $state('');
  let dkKeySizeBits = $state(256);
  let dkResult = $state<{ plaintext: string; ciphertext_blob: string } | null>(null);
  let dkError = $state('');
  let generatingDK = $state(false);

  async function submitDataKey(): Promise<void> {
    dkError = '';
    dkResult = null;
    if (!dkKeyName.trim()) { dkError = 'KEK name is required.'; return; }
    generatingDK = true;
    try {
      const client = await getSDKClient();
      const resp = await client.crypto.dataKey({ key_name: dkKeyName.trim(), key_size_bits: dkKeySizeBits });
      dkResult = { plaintext: resp.plaintext, ciphertext_blob: resp.ciphertext_blob };
    } catch (err: unknown) {
      dkError = err instanceof Error ? err.message : String(err);
    } finally {
      generatingDK = false;
    }
  }
</script>

<div class="p-6 space-y-6 max-w-5xl mx-auto">
  <PageHeader title="Crypto Console" subtitle="Low-level cryptographic primitives backed by KMS-held keys" icon={Binary} />

  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <!-- Encrypt -->
    <div class="card bg-base-200 rounded-box p-4">
      <h3 class="text-sm font-semibold mb-3">Encrypt</h3>
      <div class="space-y-2">
        <input class="input input-bordered input-sm w-full" type="text" placeholder="Key name" bind:value={encKeyName} aria-label="Encrypt key name" />
        <textarea class="textarea textarea-bordered textarea-sm w-full" rows="3" placeholder="Plaintext (UTF-8 text)" bind:value={encPlaintext} aria-label="Plaintext"></textarea>
      </div>
      {#if encError}<div class="alert alert-error text-xs mt-2" role="alert">{encError}</div>{/if}
      {#if encResult}
        <div class="mt-2 text-xs">
          <div class="flex items-center justify-between">
            <span class="text-base-content/50">Ciphertext (base64, v{encResult.key_version})</span>
            <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(encResult?.ciphertext ?? '', 'Ciphertext')}><Copy class="h-3 w-3" /></button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{encResult.ciphertext}</pre>
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={encrypting} onclick={() => void submitEncrypt()}>
        {encrypting ? 'Encrypting…' : 'Encrypt'}
      </button>
    </div>

    <!-- Decrypt -->
    <div class="card bg-base-200 rounded-box p-4">
      <h3 class="text-sm font-semibold mb-3">Decrypt</h3>
      <div class="space-y-2">
        <input class="input input-bordered input-sm w-full" type="text" placeholder="Key name" bind:value={decKeyName} aria-label="Decrypt key name" />
        <textarea class="textarea textarea-bordered textarea-sm w-full font-mono" rows="3" placeholder="Ciphertext (base64)" bind:value={decCiphertext} aria-label="Ciphertext"></textarea>
      </div>
      {#if decError}<div class="alert alert-error text-xs mt-2" role="alert">{decError}</div>{/if}
      {#if decResult}
        <div class="mt-2 text-xs">
          <div class="flex items-center justify-between">
            <span class="text-base-content/50">Plaintext (v{decResult.key_version})</span>
            <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(decResult?.plaintext ?? '', 'Plaintext')}><Copy class="h-3 w-3" /></button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{decResult.plaintext}</pre>
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={decrypting} onclick={() => void submitDecrypt()}>
        {decrypting ? 'Decrypting…' : 'Decrypt'}
      </button>
    </div>

    <!-- Sign -->
    <div class="card bg-base-200 rounded-box p-4">
      <h3 class="text-sm font-semibold mb-3">Sign</h3>
      <div class="space-y-2">
        <input class="input input-bordered input-sm w-full" type="text" placeholder="Key name" bind:value={signKeyName} aria-label="Sign key name" />
        <textarea class="textarea textarea-bordered textarea-sm w-full" rows="3" placeholder="Message (UTF-8 text)" bind:value={signMessage} aria-label="Message to sign"></textarea>
        <select class="select select-bordered select-sm w-full" bind:value={signHashAlg} aria-label="Sign hash algorithm">
          {#each HASH_ALGORITHMS as alg (alg)}<option value={alg}>{alg}</option>{/each}
        </select>
      </div>
      {#if signError}<div class="alert alert-error text-xs mt-2" role="alert">{signError}</div>{/if}
      {#if signResult}
        <div class="mt-2 text-xs">
          <div class="flex items-center justify-between">
            <span class="text-base-content/50">Signature (base64, v{signResult.key_version})</span>
            <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(signResult?.signature ?? '', 'Signature')}><Copy class="h-3 w-3" /></button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{signResult.signature}</pre>
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={signing} onclick={() => void submitSign()}>
        {signing ? 'Signing…' : 'Sign'}
      </button>
    </div>

    <!-- Verify -->
    <div class="card bg-base-200 rounded-box p-4">
      <h3 class="text-sm font-semibold mb-3">Verify</h3>
      <div class="space-y-2">
        <input class="input input-bordered input-sm w-full" type="text" placeholder="Key name" bind:value={verifyKeyName} aria-label="Verify key name" />
        <textarea class="textarea textarea-bordered textarea-sm w-full" rows="2" placeholder="Message (UTF-8 text)" bind:value={verifyMessage} aria-label="Message to verify"></textarea>
        <textarea class="textarea textarea-bordered textarea-sm w-full font-mono" rows="2" placeholder="Signature (base64)" bind:value={verifySignature} aria-label="Signature"></textarea>
        <select class="select select-bordered select-sm w-full" bind:value={verifyHashAlg} aria-label="Verify hash algorithm">
          {#each HASH_ALGORITHMS as alg (alg)}<option value={alg}>{alg}</option>{/each}
        </select>
      </div>
      {#if verifyError}<div class="alert alert-error text-xs mt-2" role="alert">{verifyError}</div>{/if}
      {#if verifyResult !== null}
        <div class="alert {verifyResult ? 'alert-success' : 'alert-error'} text-xs mt-2" role="alert">
          {verifyResult ? 'Signature is valid.' : 'Signature is NOT valid.'}
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={verifying} onclick={() => void submitVerify()}>
        {verifying ? 'Verifying…' : 'Verify'}
      </button>
    </div>

    <!-- Hash -->
    <div class="card bg-base-200 rounded-box p-4">
      <h3 class="text-sm font-semibold mb-3">Hash</h3>
      <div class="space-y-2">
        <textarea class="textarea textarea-bordered textarea-sm w-full" rows="3" placeholder="Data (UTF-8 text)" bind:value={hashData} aria-label="Data to hash"></textarea>
        <select class="select select-bordered select-sm w-full" bind:value={hashAlgorithm} aria-label="Hash algorithm">
          {#each HASH_ALGORITHMS as alg (alg)}<option value={alg}>{alg}</option>{/each}
        </select>
      </div>
      {#if hashError}<div class="alert alert-error text-xs mt-2" role="alert">{hashError}</div>{/if}
      {#if hashResult}
        <div class="mt-2 text-xs">
          <div class="flex items-center justify-between">
            <span class="text-base-content/50">Hash (base64)</span>
            <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(hashResult, 'Hash')}><Copy class="h-3 w-3" /></button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{hashResult}</pre>
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={hashing} onclick={() => void submitHash()}>
        {hashing ? 'Hashing…' : 'Hash'}
      </button>
    </div>

    <!-- Random -->
    <div class="card bg-base-200 rounded-box p-4">
      <h3 class="text-sm font-semibold mb-3">Random Bytes</h3>
      <div class="space-y-2">
        <input class="input input-bordered input-sm w-full" type="number" min="1" max="1048576" placeholder="Number of bytes" bind:value={randomNumBytes} aria-label="Number of random bytes" />
      </div>
      {#if randomError}<div class="alert alert-error text-xs mt-2" role="alert">{randomError}</div>{/if}
      {#if randomResult}
        <div class="mt-2 text-xs">
          <div class="flex items-center justify-between">
            <span class="text-base-content/50">Random bytes (base64)</span>
            <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(randomResult, 'Random bytes')}><Copy class="h-3 w-3" /></button>
          </div>
          <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{randomResult}</pre>
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={generatingRandom} onclick={() => void submitRandom()}>
        {generatingRandom ? 'Generating…' : 'Generate'}
      </button>
    </div>

    <!-- Data Key -->
    <div class="card bg-base-200 rounded-box p-4 lg:col-span-2">
      <h3 class="text-sm font-semibold mb-1">Generate Data Key</h3>
      <p class="text-xs text-warning mb-2">
        The plaintext data key is minted fresh per call and never re-servable — copy it now.
      </p>
      <div class="grid grid-cols-2 gap-2">
        <input class="input input-bordered input-sm w-full" type="text" placeholder="KEK name" bind:value={dkKeyName} aria-label="Key encryption key name" />
        <input class="input input-bordered input-sm w-full" type="number" min="8" max="2048" placeholder="Key size (bits)" bind:value={dkKeySizeBits} aria-label="Data key size in bits" />
      </div>
      {#if dkError}<div class="alert alert-error text-xs mt-2" role="alert">{dkError}</div>{/if}
      {#if dkResult}
        <div class="grid grid-cols-1 md:grid-cols-2 gap-3 mt-2 text-xs">
          <div>
            <div class="flex items-center justify-between">
              <span class="text-warning">Plaintext data key (shown once)</span>
              <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(dkResult?.plaintext ?? '', 'Plaintext data key')}><Copy class="h-3 w-3" /></button>
            </div>
            <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{dkResult.plaintext}</pre>
          </div>
          <div>
            <div class="flex items-center justify-between">
              <span class="text-base-content/50">Wrapped (ciphertext blob)</span>
              <button class="btn btn-ghost btn-xs" onclick={() => void copyToClipboard(dkResult?.ciphertext_blob ?? '', 'Ciphertext blob')}><Copy class="h-3 w-3" /></button>
            </div>
            <pre class="bg-base-300 rounded-box p-2 overflow-x-auto max-h-24 break-all">{dkResult.ciphertext_blob}</pre>
          </div>
        </div>
      {/if}
      <button class="btn btn-primary btn-sm mt-3" disabled={generatingDK} onclick={() => void submitDataKey()}>
        {generatingDK ? 'Generating…' : 'Generate Data Key'}
      </button>
    </div>
  </div>
</div>
