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
  CeremonyView orchestrates the multi-officer key ceremony. It surfaces:

    1. The pending-officer list with claim status, share index, and auth factor.
    2. A claim-certificate flow (challenge-response with the officer's CSR).
    3. A claim-share flow for officers delivering Shamir share contributions.
    4. An enroll-external-officer form (cert/pubkey + auth factor + share index).

  All HTTP traffic flows through the typed @quicbaas/sdk CeremonyClient
  (RestCeremonyClient) which maps every method to the canonical
  /api/v1/init/* surface defined in go-qrdb. The legacy /api/v1/ceremony
  paths are gone; this view never references them.
-->
<script lang="ts">
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { Users } from 'lucide-svelte';
  import { createCeremonyClient, isWailsRuntime } from '@quicbaas/sdk';
  import type { CeremonyClient as SDKCeremonyClient } from '@quicbaas/sdk';
  import {
    CeremonyOfficerList,
    CeremonyClaimChallenge,
  } from '@quicbaas/svelte-security';
  import type {
    CeremonyClient,
    CeremonyOfficer,
    OfficerKind,
  } from '@quicbaas/svelte-security';

  // ---------------------------------------------------------------------------
  // SDK -> structural CeremonyClient adapter
  //
  // The svelte-security components consume a structurally-typed CeremonyClient
  // (listOfficers / claimCertificate / claimShare / enrollOfficer). The SDK
  // CeremonyClient surface is shaped around the canonical /api/v1/init routes
  // (claimCertBegin / claimCertComplete / claimShare / enrollExternalOfficer).
  // We adapt one to the other so:
  //
  //   * URL routing flows through the SDK (drift-proof).
  //   * Components stay presentational (no transport knowledge).
  //   * /api/v1/seal/* and /api/v1/ceremony/* are never hit.
  // ---------------------------------------------------------------------------

  function resolveTransport(): 'wails' | 'rest' {
    return isWailsRuntime() ? 'wails' : 'rest';
  }

  // listOfficersURL builds the /api/v1/init/officers URL. The SDK does not
  // expose listOfficers because the route is QuicBaaS-frontend-specific
  // (other consumers have their own roster). Routing through this helper
  // ensures the path stays under /api/v1/init/* alongside the other
  // ceremony endpoints.
  function listOfficersURL(): string {
    const base = typeof window !== 'undefined' ? window.location.origin : '';
    return `${base}/api/v1/init/officers`;
  }

  // ServiceUnavailableError is thrown when the daemon returns a 503 typed
  // envelope ({error: "...not available"}). Callers convert this into an
  // informational empty-state instead of a destructive error banner — the
  // ceremony service simply isn't active yet.
  class ServiceUnavailableError extends Error {
    readonly status = 503;
    constructor(message: string) {
      super(message);
      this.name = 'ServiceUnavailableError';
    }
  }

  async function fetchOfficers(): Promise<CeremonyOfficer[]> {
    const resp = await fetch(listOfficersURL(), {
      method: 'GET',
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) {
      let msg = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        msg = body.error ?? body.message ?? msg;
      } catch { /* keep statusText */ }
      // 503 with a typed envelope means the ceremony subsystem isn't
      // active yet — surface it as a typed error so the view renders an
      // informational empty state, not a destructive banner.
      if (resp.status === 503) {
        throw new ServiceUnavailableError(msg);
      }
      throw new Error(`${resp.status} ${msg}`);
    }
    const data = (await resp.json()) as { officers?: CeremonyOfficer[] };
    return data.officers ?? [];
  }

  // makeClient builds the structural CeremonyClient that the svelte-security
  // components consume by adapting the typed SDK CeremonyClient.
  async function makeClient(): Promise<CeremonyClient> {
    const baseURL = typeof window !== 'undefined' ? window.location.origin : '';
    const sdk: SDKCeremonyClient = await createCeremonyClient(resolveTransport(), { baseURL });

    return {
      async listOfficers() {
        return fetchOfficers();
      },
      async claimCertificate(req) {
        // The structural claimCertificate ships the daemon-issued challenge
        // plus a base64 signature over it. The SDK exposes this as
        // claimCertComplete which maps to POST /api/v1/init/claim-cert/complete.
        const resp = await sdk.claimCertComplete({
          username: req.username,
          nonce_hex: req.challenge,
          signature: req.signature_b64,
        });
        return {
          username: req.username,
          // The SDK returns the issued certificate; the structural ClaimResponse
          // only carries the username + status. "claimed" reflects the success
          // path; failures throw.
          status: resp.cert_pem ? 'claimed' : 'enrolled',
        };
      },
      async claimShare(req) {
        // The SDK claimShare retrieves the officer's sealed share material;
        // it requires only the username. The view's UI exposes a free-form
        // "share" field for symmetry with offline workflows but the SDK
        // surface ignores it (the daemon delivers the share by username).
        await sdk.claimShare({ username: req.username });
        return {
          username: req.username,
          status: 'claimed',
        };
      },
      async enrollOfficer(req) {
        const certPem = req.kind === 'cert' ? req.material : undefined;
        const pubkeyPem = req.kind === 'pubkey' ? req.material : undefined;
        await sdk.enrollExternalOfficer({
          username: req.username,
          cert_pem: certPem,
          pubkey_pem: pubkeyPem,
          share_index: req.share_index,
          auth_factor: req.auth_factor,
        });
        return {
          username: req.username,
          kind: req.kind,
          claim_status: 'enrolled',
          share_index: req.share_index,
          auth_factor: req.auth_factor,
        };
      },
    };
  }

  // ---------------------------------------------------------------------------
  // Reactive state
  // ---------------------------------------------------------------------------

  let client: CeremonyClient | null = $state(null);
  let officers: CeremonyOfficer[] = $state([]);
  let loading = $state(false);
  let listError: string | null = $state(null);
  // serviceUnavailable is set when the daemon's GET /api/v1/init/officers
  // returns a 503 typed envelope. It triggers an informational empty state
  // instead of a destructive error banner.
  let serviceUnavailable = $state(false);

  async function reload(): Promise<void> {
    loading = true;
    listError = null;
    serviceUnavailable = false;
    try {
      if (client === null) {
        client = await makeClient();
      }
      officers = await client.listOfficers();
    } catch (e) {
      if (e instanceof ServiceUnavailableError) {
        serviceUnavailable = true;
        officers = [];
      } else {
        listError = e instanceof Error ? e.message : 'Failed to load officers';
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void reload();
  });

  // ---------------------------------------------------------------------------
  // Claim share flow
  // ---------------------------------------------------------------------------

  let shareUsername = $state('');
  let shareValue = $state('');
  let shareSubmitting = $state(false);
  let shareError: string | null = $state(null);

  async function submitClaimShare(): Promise<void> {
    shareError = null;
    if (!shareUsername.trim() || !shareValue.trim()) {
      shareError = 'username and share are required';
      return;
    }
    if (client === null) {
      shareError = 'ceremony client not ready';
      return;
    }
    shareSubmitting = true;
    try {
      const resp = await client.claimShare({
        username: shareUsername.trim(),
        share: shareValue.trim(),
      });
      showToast({ message: `Share claimed for ${resp.username}`, type: 'success' });
      shareUsername = '';
      shareValue = '';
      void reload();
    } catch (e) {
      shareError = e instanceof Error ? e.message : 'Claim share failed';
    } finally {
      shareSubmitting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Enroll external officer
  // ---------------------------------------------------------------------------

  let enrollUsername = $state('');
  let enrollKind: OfficerKind = $state('cert');
  let enrollMaterial = $state('');
  let enrollShareIndex = $state(1);
  let enrollAuthFactor = $state('');
  let enrolling = $state(false);
  let enrollError: string | null = $state(null);

  async function onEnrollFile(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file === undefined) return;
    const text = await file.text();
    enrollMaterial = text;
  }

  async function submitEnroll(): Promise<void> {
    enrollError = null;
    if (
      !enrollUsername.trim() ||
      !enrollMaterial.trim() ||
      !enrollAuthFactor.trim() ||
      enrollShareIndex < 1
    ) {
      enrollError = 'all fields are required';
      return;
    }
    if (client === null) {
      enrollError = 'ceremony client not ready';
      return;
    }
    enrolling = true;
    try {
      const officer = await client.enrollOfficer({
        username: enrollUsername.trim(),
        kind: enrollKind,
        material: enrollMaterial,
        share_index: enrollShareIndex,
        auth_factor: enrollAuthFactor.trim(),
      });
      showToast({ message: `Officer ${officer.username} enrolled`, type: 'success' });
      enrollUsername = '';
      enrollMaterial = '';
      enrollAuthFactor = '';
      enrollShareIndex = 1;
      void reload();
    } catch (e) {
      enrollError = e instanceof Error ? e.message : 'Enrollment failed';
    } finally {
      enrolling = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader
    title="Ceremony"
    subtitle="Multi-officer key ceremony"
    icon={Users}
  />

  <!-- ===========================================================================
       OFFICER LIST
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      <div class="flex items-center justify-between">
        <h2 class="text-lg font-semibold">Pending Officers</h2>
        <button
          class="btn btn-ghost btn-xs"
          onclick={() => { void reload(); }}
          disabled={loading}
        >
          Refresh
        </button>
      </div>
      {#if serviceUnavailable}
        <div
          class="alert alert-info text-sm"
          role="status"
          data-testid="ceremony-service-unavailable"
        >
          <span>
            The ceremony service is not yet active on this server. Run
            <code class="font-mono">quicbaasd init</code>
            to bootstrap the ceremony, then refresh this page.
          </span>
        </div>
      {:else if listError !== null}
        <div class="alert alert-error text-sm" role="alert">{listError}</div>
      {/if}
      {#if !serviceUnavailable}
        <CeremonyOfficerList
          officers={officers}
          loading={loading}
        />
      {/if}
    </div>
  </div>

  <!-- ===========================================================================
       CLAIM CERTIFICATE
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      {#if client !== null}
        <CeremonyClaimChallenge
          client={client}
          disabled={serviceUnavailable}
          onclaimed={(resp) => {
            showToast({ message: `${resp.username}: ${resp.status}`, type: 'success' });
            void reload();
          }}
          onerror={(err) => {
            showToast({ message: err.message, type: 'error' });
          }}
        />
      {/if}
    </div>
  </div>

  <!-- ===========================================================================
       CLAIM SHARE
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      <h3 class="text-sm font-semibold">Claim Share</h3>

      {#if shareError !== null}
        <div class="alert alert-error text-sm" role="alert">{shareError}</div>
      {/if}

      <form
        class="space-y-3"
        data-testid="ceremony-claim-share"
        onsubmit={(e) => { e.preventDefault(); void submitClaimShare(); }}
      >
        <div class="form-control">
          <label class="label label-text" for="ceremony-share-username">Username</label>
          <input
            id="ceremony-share-username"
            class="input input-bordered input-sm font-mono"
            type="text"
            bind:value={shareUsername}
          />
        </div>
        <div class="form-control">
          <label class="label label-text" for="ceremony-share-value">Share</label>
          <textarea
            id="ceremony-share-value"
            class="textarea textarea-bordered font-mono text-xs"
            rows="2"
            placeholder="Paste base64 Shamir share..."
            bind:value={shareValue}
          ></textarea>
        </div>
        <button
          class="btn btn-primary btn-sm"
          type="submit"
          disabled={shareSubmitting || serviceUnavailable}
          title={serviceUnavailable ? 'Ceremony service is not active. Run `quicbaasd init` first.' : undefined}
        >
          {shareSubmitting ? 'Submitting...' : 'Submit Share'}
        </button>
      </form>
    </div>
  </div>

  <!-- ===========================================================================
       ENROLL EXTERNAL OFFICER
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      <h3 class="text-sm font-semibold">Enroll External Officer</h3>

      {#if enrollError !== null}
        <div class="alert alert-error text-sm" role="alert">{enrollError}</div>
      {/if}

      <form
        class="space-y-3"
        data-testid="ceremony-enroll-form"
        onsubmit={(e) => { e.preventDefault(); void submitEnroll(); }}
      >
        <div class="grid grid-cols-2 gap-3">
          <div class="form-control">
            <label class="label label-text" for="ceremony-enroll-username">Username</label>
            <input
              id="ceremony-enroll-username"
              class="input input-bordered input-sm font-mono"
              type="text"
              bind:value={enrollUsername}
            />
          </div>
          <div class="form-control">
            <label class="label label-text" for="ceremony-enroll-kind">Kind</label>
            <select
              id="ceremony-enroll-kind"
              class="select select-bordered select-sm"
              bind:value={enrollKind}
            >
              <option value="cert">cert</option>
              <option value="pubkey">pubkey</option>
            </select>
          </div>
          <div class="form-control">
            <label class="label label-text" for="ceremony-enroll-share">Share index</label>
            <input
              id="ceremony-enroll-share"
              class="input input-bordered input-sm"
              type="number"
              min="1"
              bind:value={enrollShareIndex}
            />
          </div>
          <div class="form-control">
            <label class="label label-text" for="ceremony-enroll-factor">Auth factor</label>
            <input
              id="ceremony-enroll-factor"
              class="input input-bordered input-sm"
              type="text"
              placeholder="yubikey, passphrase..."
              bind:value={enrollAuthFactor}
            />
          </div>
        </div>

        <div class="form-control">
          <label class="label label-text" for="ceremony-enroll-file">Upload material</label>
          <input
            id="ceremony-enroll-file"
            class="file-input file-input-bordered file-input-sm"
            type="file"
            accept=".pem,.crt,.cer,.pub,.txt"
            onchange={onEnrollFile}
          />
        </div>

        <div class="form-control">
          <label class="label label-text" for="ceremony-enroll-material">
            Material (PEM cert or SSH/SPKI pubkey)
          </label>
          <textarea
            id="ceremony-enroll-material"
            class="textarea textarea-bordered font-mono text-xs"
            rows="4"
            placeholder="Paste or upload PEM/pubkey material..."
            bind:value={enrollMaterial}
          ></textarea>
        </div>

        <button
          class="btn btn-primary btn-sm"
          type="submit"
          disabled={enrolling}
        >
          {enrolling ? 'Enrolling...' : 'Enroll Officer'}
        </button>
      </form>
    </div>
  </div>
</div>
