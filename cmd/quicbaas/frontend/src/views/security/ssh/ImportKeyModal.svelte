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
  Browser-side passphrase handling caveat:
  JavaScript strings are immutable — there is no way to zero a passphrase
  after use. The security guarantee lives server-side: the quicbaasd REST
  handler uses a custom json.Unmarshaler that decodes the base64 passphrase
  directly into a mlocked secure.Buffer, so the Go process never holds the
  plaintext in an unprotected string. After submit, we unmount this
  component immediately so the password input is gone from the DOM, but
  the JS heap retains the passphrase bytes until GC — a limitation of
  the browser platform.
-->

<!--
  ImportKeyModal allows importing an existing PEM-encoded private key into xkms.

  Props:
    scope     — The SSH scope (user | tenant | service). Shown read-only.
    owner     — Derived owner string for the active scope. Shown read-only.
    onClose   — Called when the user cancels.
    onCreated — Called with the imported SSHKey on success.

  After submit (success or failure), the component clears PEM and passphrase
  fields and calls onClose immediately so the password input is gone from the DOM.
-->
<script lang="ts">
  import { getSDKClient } from '../../../stores/sdkClient.ts';
  import type { SSHKey, SSHScope } from '@quicbaas/sdk';
  import { SSHDuplicateError, SSHInvalidError, SSHUnavailableError } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let {
    scope,
    owner,
    onClose,
    onCreated,
  }: {
    /** SSH scope for the imported key. Displayed read-only in the form. */
    scope: SSHScope;
    /** Owner namespace for the imported key. Displayed read-only in the form. */
    owner: string;
    /** Called when the modal should be dismissed without a result. */
    onClose: () => void;
    /** Called with the newly imported key on success. */
    onCreated: (key: SSHKey) => void;
  } = $props();

  // ---------------------------------------------------------------------------
  // Form state
  // ---------------------------------------------------------------------------

  let name = $state('');
  let pem = $state('');
  let passphrase = $state('');
  let submitting = $state(false);
  let nameError: string | null = $state(null);
  let pemError: string | null = $state(null);
  let globalError: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // File picker — reads PEM text from a selected file
  // ---------------------------------------------------------------------------

  function handleFileChange(e: Event): void {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => {
      pem = (ev.target?.result as string) ?? '';
      pemError = null;
    };
    reader.readAsText(file);
  }

  // ---------------------------------------------------------------------------
  // Validation
  // ---------------------------------------------------------------------------

  function validate(): boolean {
    let ok = true;
    nameError = null;
    pemError = null;

    if (!name.trim()) {
      nameError = 'Name is required';
      ok = false;
    }
    if (!pem.trim()) {
      pemError = 'PEM data is required — paste or upload a private key file';
      ok = false;
    }
    return ok;
  }

  // ---------------------------------------------------------------------------
  // Submit
  // ---------------------------------------------------------------------------

  async function submit(): Promise<void> {
    globalError = null;
    if (!validate()) return;

    submitting = true;

    // Capture passphrase and encode before clearing. btoa is sufficient for
    // ASCII passphrases; non-ASCII users should use a file-based key store
    // with an encrypted PEM file instead.
    const passphrase_b64 = passphrase ? btoa(passphrase) : undefined;

    // Capture PEM text before clearing.
    const pemText = pem.trim();
    const nameText = name.trim();

    // Clear sensitive fields immediately — before the await — so even if the
    // request hangs, the values are gone from the bound inputs as fast as
    // possible. The JS heap will still retain the string until GC (see the
    // header comment above).
    pem = '';
    passphrase = '';

    try {
      const client = await getSDKClient();
      const key = await client.ssh.keys.import_({
        name: nameText,
        pem: pemText,
        passphrase_b64,
        scope,
        owner: owner || undefined,
      });
      onCreated(key);
    } catch (e) {
      if (e instanceof SSHDuplicateError) {
        nameError = 'A key with this name already exists in this scope';
      } else if (e instanceof SSHInvalidError) {
        // Heuristic: if the message mentions "passphrase" or "decrypt", show
        // a passphrase-level hint rather than a PEM error.
        const msg = e.message.toLowerCase();
        if (msg.includes('passphrase') || msg.includes('decrypt') || msg.includes('password')) {
          pemError = 'Invalid passphrase or encrypted key format: ' + e.message;
        } else {
          pemError = 'Invalid PEM data: ' + e.message;
        }
      } else if (e instanceof SSHUnavailableError) {
        globalError = 'SSH is not configured for this QuicBaaS instance — set XKMS configuration to enable SSH key management.';
      } else {
        globalError = e instanceof Error ? e.message : 'Key import failed';
      }
    } finally {
      submitting = false;
      // Always close after the request completes so the password input is
      // unmounted from the DOM regardless of success or failure.
      onClose();
    }
  }
</script>

<div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Import SSH key dialog">
  <div class="modal-box max-w-lg">
    <h3 class="text-base font-semibold mb-4">Import SSH Key</h3>

    {#if globalError}
      <div class="alert alert-error text-sm mb-3" role="alert">{globalError}</div>
    {/if}

    <div class="space-y-4">

      <!-- Name -->
      <div class="form-control">
        <label class="label label-text" for="import-key-name">
          Name <span class="text-error ml-0.5" aria-hidden="true">*</span>
        </label>
        <input
          id="import-key-name"
          class="input input-bordered input-sm"
          class:input-error={nameError !== null}
          type="text"
          placeholder="my-imported-key"
          bind:value={name}
          aria-required="true"
          aria-describedby={nameError ? 'import-key-name-err' : undefined}
        />
        {#if nameError}
          <div id="import-key-name-err" class="label-text-alt text-error mt-1 text-xs" role="alert">
            {nameError}
          </div>
        {/if}
      </div>

      <!-- PEM — textarea + file picker -->
      <div class="form-control">
        <label class="label label-text" for="import-key-pem">
          Private Key (PEM) <span class="text-error ml-0.5" aria-hidden="true">*</span>
        </label>
        <textarea
          id="import-key-pem"
          class="textarea textarea-bordered font-mono text-xs resize-y min-h-[120px]"
          class:textarea-error={pemError !== null}
          placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n…\n-----END OPENSSH PRIVATE KEY-----"}
          bind:value={pem}
          aria-required="true"
          aria-describedby={pemError ? 'import-key-pem-err' : undefined}
        ></textarea>
        <div class="label">
          <span class="label-text-alt text-base-content/50">or</span>
          <label class="label-text-alt link link-primary cursor-pointer" for="import-key-file">
            Upload file
          </label>
        </div>
        <input
          id="import-key-file"
          type="file"
          class="sr-only"
          accept=".pem,.key,.ppk,text/plain"
          onchange={handleFileChange}
          aria-label="Upload private key file"
        />
        {#if pemError}
          <div id="import-key-pem-err" class="label-text-alt text-error mt-1 text-xs" role="alert">
            {pemError}
          </div>
        {/if}
      </div>

      <!-- Passphrase (optional) -->
      <div class="form-control">
        <label class="label label-text" for="import-key-passphrase">
          Passphrase <span class="text-base-content/40 text-xs ml-1">(optional — only for encrypted keys)</span>
        </label>
        <input
          id="import-key-passphrase"
          class="input input-bordered input-sm"
          type="password"
          placeholder="Leave blank for unencrypted keys"
          bind:value={passphrase}
          autocomplete="off"
        />
        <div class="label">
          <span class="label-text-alt text-base-content/40 text-xs">
            The passphrase is base64-encoded and sent to the server where it is decoded
            directly into a memory-locked buffer (never a Go string).
          </span>
        </div>
      </div>

      <!-- Scope (read-only) -->
      <div class="form-control">
        <label class="label label-text" for="import-key-scope">Scope</label>
        <input
          id="import-key-scope"
          class="input input-bordered input-sm bg-base-200 cursor-not-allowed"
          type="text"
          value={scope}
          readonly
          aria-readonly="true"
        />
      </div>

      <!-- Owner (read-only) -->
      <div class="form-control">
        <label class="label label-text" for="import-key-owner">Owner</label>
        <input
          id="import-key-owner"
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
        aria-label="Cancel key import"
      >
        Cancel
      </button>
      <button
        class="btn btn-primary btn-sm"
        disabled={submitting}
        onclick={submit}
        aria-label="Import key"
      >
        {submitting ? 'Importing…' : 'Import'}
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
