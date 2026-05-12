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
  GenerateKeyModal allows the user to generate a new SSH key pair via xkms.

  Props:
    scope     — The SSH scope (user | tenant | service). Shown read-only.
    owner     — Derived owner string for the active scope. Shown read-only.
    onClose   — Called when the user cancels or after successful creation.
    onCreated — Called with the created SSHKey on success.
-->
<script lang="ts">
  import { getSDKClient } from '../../../stores/sdkClient.ts';
  import type { SSHKey, SSHScope } from '@quicbaas/sdk';
  import { SSHDuplicateError, SSHInvalidError } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let {
    scope,
    owner,
    onClose,
    onCreated,
  }: {
    /** SSH scope for the new key. Displayed read-only in the form. */
    scope: SSHScope;
    /** Owner namespace for the new key. Displayed read-only in the form. */
    owner: string;
    /** Called when the modal should be dismissed without a result. */
    onClose: () => void;
    /** Called with the newly created key on success. */
    onCreated: (key: SSHKey) => void;
  } = $props();

  // ---------------------------------------------------------------------------
  // Algorithm options
  // ---------------------------------------------------------------------------

  const algorithms = [
    'ed25519',
    'rsa-2048',
    'rsa-4096',
    'ecdsa-p256',
    'ecdsa-p384',
    'ecdsa-p521',
  ] as const;

  type Algorithm = (typeof algorithms)[number];

  // ---------------------------------------------------------------------------
  // Form state
  // ---------------------------------------------------------------------------

  let name = $state('');
  let algorithm: Algorithm = $state('ed25519');
  let submitting = $state(false);
  let nameError: string | null = $state(null);
  let globalError: string | null = $state(null);

  // Slug validation: lowercase letters, digits, hyphens.
  const slugRE = /^[a-z0-9][a-z0-9-]*$/;

  function validateName(): boolean {
    if (!name.trim()) {
      nameError = 'Name is required';
      return false;
    }
    if (!slugRE.test(name.trim())) {
      nameError = 'Name must be lowercase letters, digits, and hyphens (e.g. my-key)';
      return false;
    }
    nameError = null;
    return true;
  }

  // ---------------------------------------------------------------------------
  // Submit
  // ---------------------------------------------------------------------------

  async function submit(): Promise<void> {
    globalError = null;
    if (!validateName()) return;

    submitting = true;
    try {
      const client = await getSDKClient();
      const key = await client.ssh.keys.create({
        name: name.trim(),
        algorithm,
        scope,
        owner: owner || undefined,
      });
      onCreated(key);
    } catch (e) {
      if (e instanceof SSHDuplicateError) {
        nameError = 'A key with this name already exists in this scope';
      } else if (e instanceof SSHInvalidError) {
        nameError = e.message;
      } else {
        globalError = e instanceof Error ? e.message : 'Key generation failed';
      }
    } finally {
      submitting = false;
    }
  }
</script>

<div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Generate SSH key dialog">
  <div class="modal-box max-w-md">
    <h3 class="text-base font-semibold mb-4">Generate SSH Key</h3>

    {#if globalError}
      <div class="alert alert-error text-sm mb-3" role="alert">{globalError}</div>
    {/if}

    <div class="space-y-4">

      <!-- Name -->
      <div class="form-control">
        <label class="label label-text" for="gen-key-name">
          Name <span class="text-error ml-0.5" aria-hidden="true">*</span>
        </label>
        <input
          id="gen-key-name"
          class="input input-bordered input-sm"
          class:input-error={nameError !== null}
          type="text"
          placeholder="my-key"
          bind:value={name}
          aria-required="true"
          aria-describedby={nameError ? 'gen-key-name-err' : undefined}
        />
        {#if nameError}
          <div id="gen-key-name-err" class="label-text-alt text-error mt-1 text-xs" role="alert">
            {nameError}
          </div>
        {/if}
      </div>

      <!-- Algorithm -->
      <div class="form-control">
        <label class="label label-text" for="gen-key-algo">Algorithm</label>
        <select
          id="gen-key-algo"
          class="select select-bordered select-sm"
          bind:value={algorithm}
        >
          {#each algorithms as algo}
            <option value={algo}>{algo}</option>
          {/each}
        </select>
      </div>

      <!-- Scope (read-only) -->
      <div class="form-control">
        <label class="label label-text" for="gen-key-scope">Scope</label>
        <input
          id="gen-key-scope"
          class="input input-bordered input-sm bg-base-200 cursor-not-allowed"
          type="text"
          value={scope}
          readonly
          aria-readonly="true"
        />
      </div>

      <!-- Owner (read-only) -->
      <div class="form-control">
        <label class="label label-text" for="gen-key-owner">Owner</label>
        <input
          id="gen-key-owner"
          class="input input-bordered input-sm bg-base-200 cursor-not-allowed font-mono text-xs"
          type="text"
          value={owner || '(server default)'}
          readonly
          aria-readonly="true"
        />
      </div>

    </div>

    <div class="modal-action">
      <button
        class="btn btn-ghost btn-sm"
        onclick={onClose}
        aria-label="Cancel key generation"
      >
        Cancel
      </button>
      <button
        class="btn btn-primary btn-sm"
        disabled={submitting}
        onclick={submit}
        aria-label="Generate key"
      >
        {submitting ? 'Generating…' : 'Generate'}
      </button>
    </div>
  </div>
  <div
    class="modal-backdrop"
    role="none"
    onclick={onClose}
    onkeydown={onClose}
  ></div>
</div>
