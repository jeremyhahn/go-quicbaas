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
  KMSKeyPanel manages one named KMS key: its metadata (description and tags),
  its access policy, its rotation policy, and encrypting and decrypting text
  with it. Each tab loads on first open and keeps its own loading and error
  state; a response that arrives after the key changed is dropped.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type {
    KMSAdminClient,
    KMSKeyMetadata,
    KMSKeyPolicy,
    KMSKeyPolicyInput,
    KMSRotationPolicy,
    KMSRotationPolicyInput,
    UpdateKMSKeyMetadataRequest,
  } from '@quicbaas/sdk';
  import { X, Pencil, Plus, Trash2, Lock, Unlock } from 'lucide-svelte';

  interface Props {
    keyName: string;
    onclose: () => void;
  }

  let { keyName, onclose }: Props = $props();

  type Tab = 'metadata' | 'policy' | 'rotation' | 'encrypt';
  const TABS: { id: Tab; label: string }[] = [
    { id: 'metadata', label: 'Metadata' },
    { id: 'policy', label: 'Policy' },
    { id: 'rotation', label: 'Rotation' },
    { id: 'encrypt', label: 'Encrypt' },
  ];
  const NOT_AVAILABLE = 'Key administration is not available on this connection.';
  const ENCRYPT_NOT_AVAILABLE = 'Encryption with a KMS key is not available on this connection.';

  let tab = $state<Tab>('metadata');

  function errText(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  function isUnsupported(err: unknown): boolean {
    return err instanceof TransportNotSupportedError;
  }

  function yesNo(v: boolean | undefined): string {
    return v ? 'Yes' : 'No';
  }

  function splitList(s: string): string[] {
    return s.split(',').map((p) => p.trim()).filter((p) => p !== '');
  }

  function utf8ToBase64(text: string): string {
    let bin = '';
    for (const b of new TextEncoder().encode(text)) bin += String.fromCharCode(b);
    return btoa(bin);
  }

  /** Decodes base64 to UTF-8 text; bytes that are not UTF-8 stay base64. */
  function base64ToText(b64: string): { text: string; utf8: boolean } {
    try {
      const bin = atob(b64);
      const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
      return { text: new TextDecoder('utf-8', { fatal: true }).decode(bytes), utf8: true };
    } catch {
      return { text: b64, utf8: false };
    }
  }

  // ---------------------------------------------------------------------------
  // Generation guard: a key change bumps the generation, so a response still
  // in flight for the old key is dropped.
  // ---------------------------------------------------------------------------

  let generation = 0;
  let loadedTabs = new Set<Tab>();

  /** Resolves the admin client, or null when this connection has none. */
  async function adminClient(): Promise<KMSAdminClient | null> {
    const client = await getSDKClient();
    const admin: KMSAdminClient | null = client.kmsAdmin ?? null;
    return admin;
  }

  // ---------------------------------------------------------------------------
  // Metadata
  // ---------------------------------------------------------------------------

  let metadata = $state<KMSKeyMetadata | null>(null);
  let metaLoading = $state(false);
  let metaError = $state('');
  let metaUnavailable = $state(false);
  let metaEditing = $state(false);
  let metaSaving = $state(false);
  let editDescription = $state('');
  let editTags = $state<Record<string, string>>({});
  let newTagKey = $state('');
  let newTagValue = $state('');

  async function loadMetadata(name: string, gen: number): Promise<void> {
    metaLoading = true;
    metaError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { metaUnavailable = true; return; }
      const res = await admin.getKeyMetadata(name);
      if (gen !== generation) return;
      metadata = res.metadata;
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) metaUnavailable = true;
      else metaError = errText(err);
    } finally {
      if (gen === generation) metaLoading = false;
    }
  }

  function startMetaEdit(): void {
    if (!metadata) return;
    editDescription = metadata.description ?? '';
    editTags = { ...(metadata.tags ?? {}) };
    newTagKey = '';
    newTagValue = '';
    metaError = '';
    metaEditing = true;
  }

  function addTag(): void {
    const k = newTagKey.trim();
    if (k === '') return;
    editTags = { ...editTags, [k]: newTagValue };
    newTagKey = '';
    newTagValue = '';
  }

  function removeTag(k: string): void {
    const next = { ...editTags };
    delete next[k];
    editTags = next;
  }

  async function saveMetadata(): Promise<void> {
    if (!metadata) return;
    const req: UpdateKMSKeyMetadataRequest = {};
    if (editDescription !== (metadata.description ?? '')) req.description = editDescription;
    const orig = metadata.tags ?? {};
    const changed: Record<string, string> = {};
    for (const [k, v] of Object.entries(editTags)) {
      if (orig[k] !== v) changed[k] = v;
    }
    const removed = Object.keys(orig).filter((k) => !(k in editTags));
    if (Object.keys(changed).length > 0) req.tags = changed;
    if (removed.length > 0) req.remove_tags = removed;
    if (Object.keys(req).length === 0) { metaEditing = false; return; }

    const gen = generation;
    const name = keyName;
    metaSaving = true;
    metaError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { metaUnavailable = true; return; }
      const res = await admin.updateKeyMetadata(name, req);
      if (gen !== generation) return;
      metadata = res.metadata;
      metaEditing = false;
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) metaUnavailable = true;
      else metaError = errText(err);
    } finally {
      if (gen === generation) metaSaving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Access policy
  // ---------------------------------------------------------------------------

  let policy = $state<KMSKeyPolicy | null>(null);
  let policyLoading = $state(false);
  let policyError = $state('');
  let policyUnavailable = $state(false);
  let policyEditing = $state(false);
  let policySaving = $state(false);
  let pOperations = $state('');
  let pAllowed = $state('');
  let pDenied = $state('');
  let pExpires = $state('');
  let pMaxUsage = $state('');
  let pMfa = $state(false);
  let pRotationRequired = $state(false);

  async function loadPolicy(name: string, gen: number): Promise<void> {
    policyLoading = true;
    policyError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { policyUnavailable = true; return; }
      const res = await admin.getKeyPolicy(name);
      if (gen !== generation) return;
      policy = res.policy;
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) policyUnavailable = true;
      else policyError = errText(err);
    } finally {
      if (gen === generation) policyLoading = false;
    }
  }

  function startPolicyEdit(): void {
    if (!policy) return;
    pOperations = policy.allowed_operations.join(', ');
    pAllowed = policy.allowed_principals.join(', ');
    pDenied = policy.denied_principals.join(', ');
    pExpires = policy.expires_at ?? '';
    pMaxUsage = policy.max_usage_count === null ? '' : String(policy.max_usage_count);
    pMfa = policy.require_mfa;
    pRotationRequired = policy.rotation_required;
    policyError = '';
    policyEditing = true;
  }

  async function savePolicy(): Promise<void> {
    if (!policy) return;
    const maxText = pMaxUsage.trim();
    if (maxText !== '' && !/^\d+$/.test(maxText)) {
      policyError = 'Max usage must be a whole number, or empty for unlimited.';
      return;
    }
    const expText = pExpires.trim();
    if (expText !== '' && Number.isNaN(Date.parse(expText))) {
      policyError = 'Expires at must be an RFC 3339 time, or empty for never.';
      return;
    }
    const input: KMSKeyPolicyInput = {
      version: policy.version,
      allowed_operations: splitList(pOperations),
      allowed_principals: splitList(pAllowed),
      denied_principals: splitList(pDenied),
      expires_at: expText === '' ? null : expText,
      rotation_required: pRotationRequired,
      max_usage_count: maxText === '' ? null : Number(maxText),
      require_mfa: pMfa,
    };

    const gen = generation;
    const name = keyName;
    policySaving = true;
    policyError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { policyUnavailable = true; return; }
      const res = await admin.setKeyPolicy(name, input);
      if (gen !== generation) return;
      policy = { ...input, key_id: res.key_id, version: res.policy_version, updated_at: res.timestamp };
      policyEditing = false;
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) policyUnavailable = true;
      else policyError = errText(err);
    } finally {
      if (gen === generation) policySaving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotation policy
  // ---------------------------------------------------------------------------

  let rotation = $state<KMSRotationPolicy | null>(null);
  let rotLoading = $state(false);
  let rotError = $state('');
  let rotUnavailable = $state(false);
  let rotSaving = $state(false);
  let rotConfirmRemove = $state(false);
  let rEnabled = $state(true);
  let rInterval = $state('');
  let rRetain = $state('');
  let rAutoRotate = $state(false);

  function fillRotationForm(p: KMSRotationPolicy | null): void {
    rEnabled = p ? p.enabled : true;
    rInterval = p ? String(p.interval_seconds) : '';
    rRetain = p && p.retain_versions !== undefined ? String(p.retain_versions) : '';
    rAutoRotate = p ? p.auto_rotate : false;
  }

  async function loadRotation(name: string, gen: number): Promise<void> {
    rotLoading = true;
    rotError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { rotUnavailable = true; return; }
      const res = await admin.getKeyRotationPolicy(name);
      if (gen !== generation) return;
      rotation = res.policy ?? null;
      fillRotationForm(rotation);
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) rotUnavailable = true;
      else rotError = errText(err);
    } finally {
      if (gen === generation) rotLoading = false;
    }
  }

  async function saveRotation(): Promise<void> {
    const intervalText = rInterval.trim();
    if (!/^\d+$/.test(intervalText) || Number(intervalText) <= 0) {
      rotError = 'Interval must be a positive whole number of seconds.';
      return;
    }
    const retainText = rRetain.trim();
    if (retainText !== '' && !/^\d+$/.test(retainText)) {
      rotError = 'Retain versions must be a whole number, or empty.';
      return;
    }
    const req: KMSRotationPolicyInput = {
      enabled: rEnabled,
      interval_seconds: Number(intervalText),
      auto_rotate: rAutoRotate,
    };
    if (retainText !== '') req.retain_versions = Number(retainText);

    const gen = generation;
    const name = keyName;
    rotSaving = true;
    rotError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { rotUnavailable = true; return; }
      const res = await admin.setKeyRotationPolicy(name, req);
      if (gen !== generation) return;
      rotation = res.policy;
      fillRotationForm(rotation);
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) rotUnavailable = true;
      else rotError = errText(err);
    } finally {
      if (gen === generation) rotSaving = false;
    }
  }

  async function removeRotation(): Promise<void> {
    const gen = generation;
    const name = keyName;
    rotConfirmRemove = false;
    rotSaving = true;
    rotError = '';
    try {
      const admin = await adminClient();
      if (gen !== generation) return;
      if (!admin) { rotUnavailable = true; return; }
      await admin.deleteKeyRotationPolicy(name);
      if (gen !== generation) return;
      rotation = null;
      fillRotationForm(null);
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) rotUnavailable = true;
      else rotError = errText(err);
    } finally {
      if (gen === generation) rotSaving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Encrypt and decrypt. Plaintext and ciphertext are bytes on the server, so
  // they travel as base64; the plaintext is UTF-8 text in the form.
  // ---------------------------------------------------------------------------

  let plainText = $state('');
  let cipherText = $state('');
  let decrypted = $state<{ text: string; utf8: boolean } | null>(null);
  let cryptoError = $state('');
  let cryptoUnavailable = $state(false);
  let cryptoBusy = $state(false);

  async function encrypt(): Promise<void> {
    if (plainText === '') return;
    const gen = generation;
    const name = keyName;
    cryptoBusy = true;
    cryptoError = '';
    try {
      const client = await getSDKClient();
      const res = await client.kms.encrypt({ key_id: name, plaintext: utf8ToBase64(plainText) });
      if (gen !== generation) return;
      cipherText = res.ciphertext;
      decrypted = null;
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) cryptoUnavailable = true;
      else cryptoError = errText(err);
    } finally {
      if (gen === generation) cryptoBusy = false;
    }
  }

  async function decrypt(): Promise<void> {
    const ciphertext = cipherText.trim();
    if (ciphertext === '') return;
    const gen = generation;
    const name = keyName;
    cryptoBusy = true;
    cryptoError = '';
    try {
      const client = await getSDKClient();
      const res = await client.kms.decrypt({ key_id: name, ciphertext });
      if (gen !== generation) return;
      decrypted = base64ToText(res.plaintext);
    } catch (err: unknown) {
      if (gen !== generation) return;
      if (isUnsupported(err)) cryptoUnavailable = true;
      else cryptoError = errText(err);
    } finally {
      if (gen === generation) cryptoBusy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Tabs and key changes
  // ---------------------------------------------------------------------------

  function ensureLoaded(t: Tab): void {
    if (loadedTabs.has(t)) return;
    loadedTabs.add(t);
    const name = keyName;
    const gen = generation;
    if (t === 'metadata') void loadMetadata(name, gen);
    else if (t === 'policy') void loadPolicy(name, gen);
    else if (t === 'rotation') void loadRotation(name, gen);
  }

  function selectTab(t: Tab): void {
    tab = t;
    ensureLoaded(t);
  }

  function resetAll(): void {
    metadata = null; metaLoading = false; metaError = ''; metaUnavailable = false;
    metaEditing = false; metaSaving = false;
    policy = null; policyLoading = false; policyError = ''; policyUnavailable = false;
    policyEditing = false; policySaving = false;
    rotation = null; rotLoading = false; rotError = ''; rotUnavailable = false;
    rotSaving = false; rotConfirmRemove = false;
    fillRotationForm(null);
    plainText = ''; cipherText = ''; decrypted = null; cryptoError = '';
    cryptoUnavailable = false; cryptoBusy = false;
  }

  $effect(() => {
    void keyName;
    untrack(() => {
      generation += 1;
      loadedTabs = new Set<Tab>();
      resetAll();
      ensureLoaded(tab);
    });
  });
</script>

<div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Manage key ${keyName}`}>
  <div class="modal-box max-w-2xl">
    <div class="flex items-start justify-between mb-3">
      <div>
        <h3 class="text-base font-semibold">Manage key</h3>
        <p class="text-xs font-mono text-base-content/50">{keyName}</p>
      </div>
      <button class="btn btn-ghost btn-sm btn-square" onclick={onclose} aria-label="Close key panel" title="Close">
        <X class="h-4 w-4" />
      </button>
    </div>

    <div role="tablist" class="tabs tabs-bordered mb-4">
      {#each TABS as t (t.id)}
        <button
          role="tab"
          class="tab {tab === t.id ? 'tab-active' : ''}"
          aria-selected={tab === t.id}
          onclick={() => selectTab(t.id)}
        >{t.label}</button>
      {/each}
    </div>

    {#if tab === 'metadata'}
      <section aria-label="Key metadata">
        {#if metaUnavailable}
          <div class="alert alert-info text-sm">{NOT_AVAILABLE}</div>
        {:else if metaLoading}
          <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
        {:else}
          {#if metaError}
            <div class="alert alert-error text-sm mb-3" role="alert">{metaError}</div>
          {/if}
          {#if metadata && !metaEditing}
            <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
              <dt class="text-base-content/60">Key ID</dt><dd class="font-mono text-xs">{metadata.key_id}</dd>
              <dt class="text-base-content/60">Description</dt><dd>{metadata.description || '—'}</dd>
              <dt class="text-base-content/60">Algorithm</dt><dd class="font-mono text-xs">{metadata.algorithm}</dd>
              <dt class="text-base-content/60">Status</dt><dd>{metadata.status}</dd>
              <dt class="text-base-content/60">Version</dt><dd>{metadata.version}</dd>
              <dt class="text-base-content/60">Owner</dt><dd>{metadata.owner || '—'}</dd>
              <dt class="text-base-content/60">Usage</dt><dd>{metadata.usage?.length ? metadata.usage.join(', ') : '—'}</dd>
              <dt class="text-base-content/60">Exportable</dt><dd>{yesNo(metadata.exportable)}</dd>
              <dt class="text-base-content/60">Deletion protection</dt><dd>{yesNo(metadata.deletion_protection)}</dd>
              <dt class="text-base-content/60">Created</dt><dd class="text-xs">{formatDateTime(metadata.created_at)}</dd>
              <dt class="text-base-content/60">Updated</dt><dd class="text-xs">{formatDateTime(metadata.updated_at)}</dd>
              <dt class="text-base-content/60">Last used</dt><dd class="text-xs">{formatDateTime(metadata.last_used_at)}</dd>
              <dt class="text-base-content/60">Tags</dt>
              <dd class="flex flex-wrap gap-1">
                {#each Object.entries(metadata.tags ?? {}) as [k, v] (k)}
                  <span class="badge badge-sm badge-ghost font-mono">{k}={v}</span>
                {:else}
                  —
                {/each}
              </dd>
            </dl>
            <div class="mt-4">
              <button class="btn btn-sm btn-outline gap-1" onclick={startMetaEdit} aria-label="Edit metadata">
                <Pencil class="h-3 w-3" /> Edit
              </button>
            </div>
          {:else if metadata && metaEditing}
            <div class="flex flex-col gap-3">
              <label class="form-control">
                <span class="label-text text-xs mb-1">Description</span>
                <input class="input input-bordered input-sm" bind:value={editDescription} />
              </label>
              <div>
                <span class="label-text text-xs">Tags</span>
                <ul class="flex flex-wrap gap-1 mt-1">
                  {#each Object.entries(editTags) as [k, v] (k)}
                    <li class="badge badge-sm badge-ghost font-mono gap-1">
                      {k}={v}
                      <button class="btn btn-ghost btn-xs px-0" onclick={() => removeTag(k)} aria-label={`Remove tag ${k}`}>
                        <Trash2 class="h-3 w-3" />
                      </button>
                    </li>
                  {/each}
                </ul>
                <div class="flex gap-2 mt-2 items-end">
                  <label class="form-control">
                    <span class="label-text text-xs mb-1">Tag key</span>
                    <input class="input input-bordered input-sm" bind:value={newTagKey} />
                  </label>
                  <label class="form-control">
                    <span class="label-text text-xs mb-1">Tag value</span>
                    <input class="input input-bordered input-sm" bind:value={newTagValue} />
                  </label>
                  <button class="btn btn-sm btn-outline gap-1" onclick={addTag} aria-label="Add tag">
                    <Plus class="h-3 w-3" /> Add
                  </button>
                </div>
              </div>
              <div class="flex gap-2">
                <button class="btn btn-sm btn-primary" disabled={metaSaving} onclick={() => void saveMetadata()} aria-label="Save metadata">Save</button>
                <button class="btn btn-sm btn-ghost" onclick={() => { metaEditing = false; }}>Cancel</button>
              </div>
            </div>
          {/if}
        {/if}
      </section>
    {:else if tab === 'policy'}
      <section aria-label="Key access policy">
        {#if policyUnavailable}
          <div class="alert alert-info text-sm">{NOT_AVAILABLE}</div>
        {:else if policyLoading}
          <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
        {:else}
          {#if policyError}
            <div class="alert alert-error text-sm mb-3" role="alert">{policyError}</div>
          {/if}
          {#if policy && !policyEditing}
            <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
              <dt class="text-base-content/60">Policy version</dt><dd>{policy.version}</dd>
              <dt class="text-base-content/60">Operations</dt><dd>{policy.allowed_operations.length ? policy.allowed_operations.join(', ') : '—'}</dd>
              <dt class="text-base-content/60">Allowed principals</dt><dd>{policy.allowed_principals.length ? policy.allowed_principals.join(', ') : '—'}</dd>
              <dt class="text-base-content/60">Denied principals</dt><dd>{policy.denied_principals.length ? policy.denied_principals.join(', ') : '—'}</dd>
              <dt class="text-base-content/60">Expires</dt><dd>{policy.expires_at ? formatDateTime(policy.expires_at) : 'Never'}</dd>
              <dt class="text-base-content/60">Max usage</dt><dd>{policy.max_usage_count === null ? 'Unlimited' : policy.max_usage_count}</dd>
              <dt class="text-base-content/60">Require MFA</dt><dd data-testid="policy-mfa">{yesNo(policy.require_mfa)}</dd>
              <dt class="text-base-content/60">Rotation required</dt><dd data-testid="policy-rotation-required">{yesNo(policy.rotation_required)}</dd>
              <dt class="text-base-content/60">Updated</dt><dd class="text-xs">{formatDateTime(policy.updated_at)}</dd>
            </dl>
            <div class="mt-4">
              <button class="btn btn-sm btn-outline gap-1" onclick={startPolicyEdit} aria-label="Edit policy">
                <Pencil class="h-3 w-3" /> Edit
              </button>
            </div>
          {:else if policy && policyEditing}
            <div class="flex flex-col gap-3">
              <label class="form-control">
                <span class="label-text text-xs mb-1">Allowed operations</span>
                <input class="input input-bordered input-sm" bind:value={pOperations} placeholder="encrypt, decrypt" />
              </label>
              <label class="form-control">
                <span class="label-text text-xs mb-1">Allowed principals</span>
                <input class="input input-bordered input-sm" bind:value={pAllowed} placeholder="comma-separated" />
              </label>
              <label class="form-control">
                <span class="label-text text-xs mb-1">Denied principals</span>
                <input class="input input-bordered input-sm" bind:value={pDenied} placeholder="comma-separated" />
              </label>
              <label class="form-control">
                <span class="label-text text-xs mb-1">Expires at</span>
                <input class="input input-bordered input-sm" bind:value={pExpires} placeholder="RFC 3339; empty for never" />
              </label>
              <label class="form-control">
                <span class="label-text text-xs mb-1">Max usage</span>
                <input class="input input-bordered input-sm" bind:value={pMaxUsage} placeholder="empty for unlimited" />
              </label>
              <label class="label cursor-pointer justify-start gap-2">
                <input type="checkbox" class="checkbox checkbox-sm" bind:checked={pMfa} />
                <span class="label-text text-sm">Require MFA</span>
              </label>
              <label class="label cursor-pointer justify-start gap-2">
                <input type="checkbox" class="checkbox checkbox-sm" bind:checked={pRotationRequired} />
                <span class="label-text text-sm">Rotation required</span>
              </label>
              <div class="flex gap-2">
                <button class="btn btn-sm btn-primary" disabled={policySaving} onclick={() => void savePolicy()} aria-label="Save policy">Save</button>
                <button class="btn btn-sm btn-ghost" onclick={() => { policyEditing = false; policyError = ''; }}>Cancel</button>
              </div>
            </div>
          {/if}
        {/if}
      </section>
    {:else if tab === 'rotation'}
      <section aria-label="Key rotation policy">
        {#if rotUnavailable}
          <div class="alert alert-info text-sm">{NOT_AVAILABLE}</div>
        {:else if rotLoading}
          <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
        {:else}
          {#if rotError}
            <div class="alert alert-error text-sm mb-3" role="alert">{rotError}</div>
          {/if}
          {#if rotation}
            <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm mb-4">
              <dt class="text-base-content/60">Enabled</dt><dd>{yesNo(rotation.enabled)}</dd>
              <dt class="text-base-content/60">Interval (seconds)</dt><dd data-testid="rotation-interval">{rotation.interval_seconds}</dd>
              <dt class="text-base-content/60">Retain versions</dt><dd>{rotation.retain_versions ?? '—'}</dd>
              <dt class="text-base-content/60">Auto-rotate</dt><dd>{yesNo(rotation.auto_rotate)}</dd>
              <dt class="text-base-content/60">Next rotation</dt><dd class="text-xs">{formatDateTime(rotation.next_rotation_unix)}</dd>
            </dl>
          {:else}
            <p class="text-sm text-base-content/60 mb-4">No rotation policy</p>
          {/if}

          <div class="flex flex-col gap-3">
            <label class="label cursor-pointer justify-start gap-2">
              <input type="checkbox" class="checkbox checkbox-sm" bind:checked={rEnabled} />
              <span class="label-text text-sm">Enabled</span>
            </label>
            <label class="form-control">
              <span class="label-text text-xs mb-1">Interval (seconds)</span>
              <input class="input input-bordered input-sm" inputmode="numeric" bind:value={rInterval} />
            </label>
            <label class="form-control">
              <span class="label-text text-xs mb-1">Retain versions</span>
              <input class="input input-bordered input-sm" inputmode="numeric" bind:value={rRetain} placeholder="optional" />
            </label>
            <label class="label cursor-pointer justify-start gap-2">
              <input type="checkbox" class="checkbox checkbox-sm" bind:checked={rAutoRotate} />
              <span class="label-text text-sm">Auto-rotate</span>
            </label>
            <div class="flex gap-2">
              <button class="btn btn-sm btn-primary" disabled={rotSaving} onclick={() => void saveRotation()} aria-label="Set rotation policy">Set policy</button>
              {#if rotation && !rotConfirmRemove}
                <button class="btn btn-sm btn-outline btn-error gap-1" disabled={rotSaving} onclick={() => { rotConfirmRemove = true; }} aria-label="Remove rotation policy">
                  <Trash2 class="h-3 w-3" /> Remove
                </button>
              {/if}
            </div>
            {#if rotation && rotConfirmRemove}
              <div class="alert alert-warning text-sm flex flex-wrap gap-2">
                <span>Remove the rotation policy of this key? Automatic rotation stops.</span>
                <button class="btn btn-xs btn-error" onclick={() => void removeRotation()}>Confirm remove</button>
                <button class="btn btn-xs btn-ghost" onclick={() => { rotConfirmRemove = false; }}>Cancel</button>
              </div>
            {/if}
          </div>
        {/if}
      </section>
    {:else}
      <section aria-label="Encrypt and decrypt with the key">
        {#if cryptoUnavailable}
          <div class="alert alert-info text-sm">{ENCRYPT_NOT_AVAILABLE}</div>
        {:else}
          {#if cryptoError}
            <div class="alert alert-error text-sm mb-3" role="alert">{cryptoError}</div>
          {/if}
          <div class="flex flex-col gap-3">
            <label class="form-control">
              <span class="label-text text-xs mb-1">Plaintext</span>
              <textarea class="textarea textarea-bordered textarea-sm font-mono" rows="3" bind:value={plainText}></textarea>
            </label>
            <div>
              <button class="btn btn-sm btn-primary gap-1" disabled={cryptoBusy} onclick={() => void encrypt()} aria-label="Encrypt">
                <Lock class="h-3 w-3" /> Encrypt
              </button>
            </div>
            <label class="form-control">
              <span class="label-text text-xs mb-1">Ciphertext</span>
              <textarea class="textarea textarea-bordered textarea-sm font-mono" rows="3" bind:value={cipherText}></textarea>
            </label>
            <div>
              <button class="btn btn-sm btn-outline gap-1" disabled={cryptoBusy} onclick={() => void decrypt()} aria-label="Decrypt">
                <Unlock class="h-3 w-3" /> Decrypt
              </button>
            </div>
            {#if decrypted}
              <div>
                <span class="label-text text-xs">
                  Decrypted {decrypted.utf8 ? 'text' : '(not UTF-8, shown as base64)'}
                </span>
                <pre class="bg-base-200 rounded p-2 text-xs whitespace-pre-wrap break-all" data-testid="decrypted-text">{decrypted.text}</pre>
              </div>
            {/if}
          </div>
        {/if}
      </section>
    {/if}
  </div>
  <div class="modal-backdrop" role="none" onclick={onclose} onkeydown={onclose}></div>
</div>
