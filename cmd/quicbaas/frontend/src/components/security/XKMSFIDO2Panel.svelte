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
  XKMSFIDO2Panel is the Key Store page's FIDO2 passkey section.

  Actions:
    - Add a passkey to my account: begins on credentials/begin and finishes
      on register/finish, for the caller's own account and tenant
    - Register a passkey for a user (email and display name)
    - Test a passkey (email): runs the authentication ceremony and shows
      whether the server accepted the assertion

  Each ceremony parses the server's options JSON with src/lib/webauthn.ts,
  runs navigator.credentials.create or get, and sends the encoded credential
  as JSON text. Nothing is called on mount. A token in a finish answer is a
  credential: it is never shown, kept, logged or toasted; the result says
  only that one was issued. A response that lands after a sign-out or
  another login is dropped, and a new session clears the results.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type { XKMSFIDO2Client, XKMSFIDO2Challenge } from '@quicbaas/sdk';
  import {
    parseCreationOptions,
    parseRequestOptions,
    encodeAttestationResponse,
    encodeAssertionResponse,
    WebAuthnEnvelopeError,
  } from '../../lib/webauthn.ts';

  const NOT_AVAILABLE = 'FIDO2 passkeys are not available on this connection.';
  const UNSUPPORTED = 'Passkeys are not supported in this browser.';
  const NO_CREDENTIAL = 'The browser returned no passkey. Nothing was sent to the server.';

  interface RegisterResult { success: boolean; credentialId: string; message: string; tokenIssued: boolean }
  interface AuthResult { success: boolean; username: string; message: string; tokenIssued: boolean }

  /** Thrown for a ceremony that stops before the finish call; its message is shown as is. */
  class CeremonyStop extends Error {}

  function errText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  const supported =
    typeof navigator !== 'undefined' &&
    !!navigator.credentials &&
    typeof navigator.credentials.create === 'function' &&
    typeof navigator.credentials.get === 'function';

  let unavailable = $state(false);

  /** Resolves the FIDO2 client, or null when this connection has none. */
  async function fido2Client(): Promise<XKMSFIDO2Client | null> {
    const client = await getSDKClient();
    const fido2 = client.xkmsFido2 ?? null;
    if (!fido2) unavailable = true;
    return fido2;
  }

  // Only finds out whether this connection has FIDO2; no FIDO2 call is made.
  if (supported) void fido2Client().catch(() => {});

  /** Parses the begin answer's options text; a bad shape stops the ceremony. */
  function parseOptions(ch: XKMSFIDO2Challenge): unknown {
    try {
      return JSON.parse(ch.options_json);
    } catch {
      throw new CeremonyStop('The server passkey options are not JSON.');
    }
  }

  function creationOptions(ch: XKMSFIDO2Challenge): PublicKeyCredentialCreationOptions {
    const raw = parseOptions(ch);
    try {
      return parseCreationOptions(raw);
    } catch (e) {
      if (e instanceof WebAuthnEnvelopeError) throw new CeremonyStop(e.message);
      throw new CeremonyStop('The server passkey options are not usable.');
    }
  }

  function requestOptions(ch: XKMSFIDO2Challenge): PublicKeyCredentialRequestOptions {
    const raw = parseOptions(ch);
    try {
      return parseRequestOptions(raw);
    } catch (e) {
      if (e instanceof WebAuthnEnvelopeError) throw new CeremonyStop(e.message);
      throw new CeremonyStop('The server passkey options are not usable.');
    }
  }

  /** Turns a browser ceremony failure into the message shown inline. */
  function browserStop(e: unknown): CeremonyStop {
    if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) {
      return new CeremonyStop('The passkey ceremony was cancelled or not allowed. Nothing was sent to the server.');
    }
    return new CeremonyStop(`The browser passkey ceremony failed: ${errText(e, 'unknown error')}`);
  }

  async function createCredential(publicKey: PublicKeyCredentialCreationOptions): Promise<PublicKeyCredential> {
    let cred: Credential | null;
    try {
      cred = await navigator.credentials.create({ publicKey });
    } catch (e) {
      throw browserStop(e);
    }
    if (!cred) throw new CeremonyStop(NO_CREDENTIAL);
    return cred as PublicKeyCredential;
  }

  async function getCredential(publicKey: PublicKeyCredentialRequestOptions): Promise<PublicKeyCredential> {
    let cred: Credential | null;
    try {
      cred = await navigator.credentials.get({ publicKey });
    } catch (e) {
      throw browserStop(e);
    }
    if (!cred) throw new CeremonyStop(NO_CREDENTIAL);
    return cred as PublicKeyCredential;
  }

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let adding = $state(false);
  let addError = $state<string | null>(null);
  let addResult = $state<RegisterResult | null>(null);

  let regEmail = $state('');
  let regDisplayName = $state('');
  let registering = $state(false);
  let regError = $state<string | null>(null);
  let regResult = $state<RegisterResult | null>(null);

  let authEmail = $state('');
  let authenticating = $state(false);
  let authError = $state<string | null>(null);
  let authResult = $state<AuthResult | null>(null);

  // A new session clears what the previous one entered or saw.
  let lastSession = authStore.sessionKey;
  $effect(() => {
    const key = authStore.sessionKey;
    untrack(() => {
      if (key === lastSession) return;
      lastSession = key;
      addError = null; addResult = null;
      regEmail = ''; regDisplayName = ''; regError = null; regResult = null;
      authEmail = ''; authError = null; authResult = null;
    });
  });

  /** Handles a ceremony error for the session that started it. */
  function failText(e: unknown, fallback: string): string | null {
    if (e instanceof TransportNotSupportedError) {
      unavailable = true;
      return null;
    }
    return errText(e, fallback);
  }

  // ---------------------------------------------------------------------------
  // Registration ceremonies
  // ---------------------------------------------------------------------------

  /** Runs create and register/finish on a begin answer; null when the session changed. */
  async function finishCreate(
    fido2: XKMSFIDO2Client,
    ch: XKMSFIDO2Challenge,
    forSession: string,
  ): Promise<RegisterResult | null> {
    const publicKey = creationOptions(ch);
    const cred = await createCredential(publicKey);
    if (forSession !== authStore.sessionKey) return null;
    const res = await fido2.finishRegistration({
      session_id: ch.session_id,
      attestation_json: JSON.stringify(encodeAttestationResponse(cred)),
    });
    if (forSession !== authStore.sessionKey) return null;
    return {
      success: res.success,
      credentialId: res.credential_id ?? '',
      message: res.message ?? '',
      tokenIssued: typeof res.token === 'string' && res.token !== '',
    };
  }

  async function addCredential(): Promise<void> {
    const forSession = authStore.sessionKey;
    addError = null;
    addResult = null;
    adding = true;
    try {
      const fido2 = await fido2Client();
      if (forSession !== authStore.sessionKey || !fido2) return;
      const ch = await fido2.beginAddCredential();
      if (forSession !== authStore.sessionKey) return;
      const res = await finishCreate(fido2, ch, forSession);
      if (res === null) return;
      addResult = res;
      if (res.success) showToast('success', `Passkey added: ${res.credentialId || 'credential'}`);
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      addError = failText(e, 'Failed to add a passkey');
    } finally {
      adding = false;
    }
  }

  async function register(): Promise<void> {
    const email = regEmail.trim();
    const displayName = regDisplayName.trim();
    if (email === '') { regError = 'Email is required.'; return; }
    if (displayName === '') { regError = 'Display name is required.'; return; }
    const forSession = authStore.sessionKey;
    regError = null;
    regResult = null;
    registering = true;
    try {
      const fido2 = await fido2Client();
      if (forSession !== authStore.sessionKey || !fido2) return;
      const ch = await fido2.beginRegistration({ email, display_name: displayName });
      if (forSession !== authStore.sessionKey) return;
      const res = await finishCreate(fido2, ch, forSession);
      if (res === null) return;
      regResult = res;
      if (res.success) showToast('success', `Passkey registered for ${email}`);
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      regError = failText(e, 'Failed to register a passkey');
    } finally {
      registering = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Authentication ceremony
  // ---------------------------------------------------------------------------

  async function authenticate(): Promise<void> {
    const email = authEmail.trim();
    if (email === '') { authError = 'Email is required.'; return; }
    const forSession = authStore.sessionKey;
    authError = null;
    authResult = null;
    authenticating = true;
    try {
      const fido2 = await fido2Client();
      if (forSession !== authStore.sessionKey || !fido2) return;
      const ch = await fido2.beginAuthentication({ email });
      if (forSession !== authStore.sessionKey) return;
      const publicKey = requestOptions(ch);
      const cred = await getCredential(publicKey);
      if (forSession !== authStore.sessionKey) return;
      const res = await fido2.finishAuthentication({
        session_id: ch.session_id,
        email,
        assertion_json: JSON.stringify(encodeAssertionResponse(cred)),
      });
      if (forSession !== authStore.sessionKey) return;
      authResult = {
        success: res.success,
        username: res.username ?? '',
        message: res.message ?? '',
        tokenIssued: typeof res.token === 'string' && res.token !== '',
      };
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      authError = failText(e, 'Failed to test the passkey');
    } finally {
      authenticating = false;
    }
  }
</script>

<div class="space-y-3">
  <h2 class="text-base font-semibold">FIDO2 Passkeys</h2>

  {#if !supported}
    <div class="alert alert-warning text-sm" data-testid="fido2-unsupported">{UNSUPPORTED}</div>
  {:else if unavailable}
    <div class="alert alert-info text-sm" data-testid="xkms-fido2-not-available">{NOT_AVAILABLE}</div>
  {:else}
    <div class="grid gap-4 lg:grid-cols-3">
      <div class="card bg-base-100 border border-base-300">
        <div class="card-body p-4 space-y-3">
          <h3 class="font-semibold text-sm">Add a passkey</h3>
          <p class="text-xs opacity-70">Adds another passkey to your own account, in your tenant.</p>
          {#if addError}
            <div class="alert alert-error text-sm" role="alert">{addError}</div>
          {/if}
          <button class="btn btn-sm btn-primary" disabled={adding} onclick={() => void addCredential()}>Add a passkey to my account</button>
          {#if addResult}
            <div class="text-xs space-y-1" data-testid="fido2-add-result">
              <div>Success: <span class="font-semibold">{addResult.success ? 'Yes' : 'No'}</span></div>
              <div>Credential ID: <span class="font-mono break-all">{addResult.credentialId || '—'}</span></div>
              {#if addResult.message}<div>{addResult.message}</div>{/if}
              {#if addResult.tokenIssued}<div>A token was issued; it is not shown.</div>{/if}
            </div>
          {/if}
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300">
        <div class="card-body p-4 space-y-3">
          <h3 class="font-semibold text-sm">Register a passkey for a user</h3>
          {#if regError}
            <div class="alert alert-error text-sm" role="alert">{regError}</div>
          {/if}
          <div class="form-control">
            <label class="label label-text" for="xkms-fido2-reg-email">Register email</label>
            <input id="xkms-fido2-reg-email" class="input input-bordered input-sm" type="email" bind:value={regEmail} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="xkms-fido2-reg-name">Display name</label>
            <input id="xkms-fido2-reg-name" class="input input-bordered input-sm" type="text" bind:value={regDisplayName} />
          </div>
          <button class="btn btn-sm btn-primary" disabled={registering} onclick={() => void register()}>Register passkey</button>
          {#if regResult}
            <div class="text-xs space-y-1" data-testid="fido2-register-result">
              <div>Success: <span class="font-semibold">{regResult.success ? 'Yes' : 'No'}</span></div>
              <div>Credential ID: <span class="font-mono break-all">{regResult.credentialId || '—'}</span></div>
              {#if regResult.message}<div>{regResult.message}</div>{/if}
              {#if regResult.tokenIssued}<div>A token was issued; it is not shown.</div>{/if}
            </div>
          {/if}
        </div>
      </div>

      <div class="card bg-base-100 border border-base-300">
        <div class="card-body p-4 space-y-3">
          <h3 class="font-semibold text-sm">Test a passkey</h3>
          {#if authError}
            <div class="alert alert-error text-sm" role="alert">{authError}</div>
          {/if}
          <div class="form-control">
            <label class="label label-text" for="xkms-fido2-auth-email">Test email</label>
            <input id="xkms-fido2-auth-email" class="input input-bordered input-sm" type="email" bind:value={authEmail} />
          </div>
          <button class="btn btn-sm" disabled={authenticating} onclick={() => void authenticate()}>Test passkey</button>
          {#if authResult}
            <div class="text-xs space-y-1" data-testid="fido2-auth-result">
              <div>Success: <span class="font-semibold">{authResult.success ? 'Yes' : 'No'}</span></div>
              <div>Username: <span class="font-mono">{authResult.username || '—'}</span></div>
              {#if authResult.message}<div>{authResult.message}</div>{/if}
              {#if authResult.tokenIssued}<div>A token was issued; it is not shown.</div>{/if}
            </div>
          {/if}
        </div>
      </div>
    </div>
  {/if}
</div>
