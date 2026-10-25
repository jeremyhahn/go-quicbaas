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

    1. The barrier's key holders with claim status, share index, and factors.
    2. A share-claim flow: the officer requests a nonce, signs it offline
       with its key, and receives its share sealed to that key.
    3. An officer enrollment form (username + CSR).

  All HTTP traffic flows through the typed @quicbaas/sdk BarrierClient, which
  maps every method to the barrier holder routes the server serves:
  GET/POST /api/v1/barrier/officers and
  /api/v1/barrier/officers/{username}/claim/{begin,complete}. The legacy
  /api/v1/ceremony paths are gone; this view never references them.
-->
<script lang="ts">
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { Users } from 'lucide-svelte';
  import { APIError, createBarrierClient, isWailsRuntime } from '@quicbaas/sdk';
  import type { BarrierOfficer } from '@quicbaas/sdk';
  import {
    CeremonyOfficerList,
    CeremonyClaimChallenge,
  } from '@quicbaas/svelte-security';
  import type {
    CeremonyClient,
    CeremonyOfficer,
  } from '@quicbaas/svelte-security';

  // ---------------------------------------------------------------------------
  // SDK -> structural CeremonyClient adapter
  //
  // The svelte-security components consume a structurally-typed CeremonyClient
  // (listOfficers / beginClaim / claimShare / enrollOfficer). The SDK serves
  // these as the BarrierClient key-holder verbs (officers / shareClaimBegin /
  // shareClaimComplete / registerOfficer). We adapt one to the other so:
  //
  //   * URL routing flows through the SDK (drift-proof).
  //   * Components stay presentational (no transport knowledge).
  //   * /api/v1/seal/* and /api/v1/ceremony/* are never hit.
  // ---------------------------------------------------------------------------

  function resolveTransport(): 'wails' | 'rest' {
    return isWailsRuntime() ? 'wails' : 'rest';
  }

  // toCeremonyOfficer maps a barrier key holder onto the officer row the
  // list renders. Every holder authenticates with its issued certificate.
  function toCeremonyOfficer(o: BarrierOfficer): CeremonyOfficer {
    return {
      username: o.username,
      kind: 'cert',
      claim_status: o.share_pending ? 'enrolled' : 'claimed',
      share_index: o.share_index,
      auth_factor: (o.factors ?? []).join(', '),
    };
  }

  // makeClient builds the structural CeremonyClient that the svelte-security
  // components consume by adapting the typed SDK BarrierClient.
  async function makeClient(): Promise<CeremonyClient> {
    const baseURL = typeof window !== 'undefined' ? window.location.origin : '';
    const sdk = await createBarrierClient(resolveTransport(), { baseURL });

    return {
      async listOfficers() {
        const holders = await sdk.officers();
        return holders.map(toCeremonyOfficer);
      },
      async beginClaim(username) {
        return sdk.shareClaimBegin(username);
      },
      async claimShare(req) {
        return sdk.shareClaimComplete({
          username: req.username,
          nonce_hex: req.nonce_hex,
          signature: req.signature_b64,
        });
      },
      async enrollOfficer(req) {
        await sdk.registerOfficer({ username: req.username, csr_pem: req.csr_pem });
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
  // serviceUnavailable is set when the daemon's GET /api/v1/barrier/officers
  // returns a 503 typed envelope: the ceremony subsystem isn't active yet. It
  // triggers an informational empty state instead of a destructive error
  // banner.
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
      if (e instanceof APIError && e.status === 503) {
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
  // Enroll officer
  // ---------------------------------------------------------------------------

  let enrollUsername = $state('');
  let enrollCSR = $state('');
  let enrolling = $state(false);
  let enrollError: string | null = $state(null);

  async function onEnrollFile(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file === undefined) return;
    enrollCSR = await file.text();
  }

  async function submitEnroll(): Promise<void> {
    enrollError = null;
    if (!enrollUsername.trim() || !enrollCSR.trim()) {
      enrollError = 'username and CSR are required';
      return;
    }
    if (client === null) {
      enrollError = 'ceremony client not ready';
      return;
    }
    enrolling = true;
    try {
      const username = enrollUsername.trim();
      await client.enrollOfficer({ username, csr_pem: enrollCSR });
      showToast({ message: `Officer ${username} enrolled`, type: 'success' });
      enrollUsername = '';
      enrollCSR = '';
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
        <h2 class="text-lg font-semibold">Officers</h2>
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
       CLAIM SHARE
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      {#if client !== null}
        <CeremonyClaimChallenge
          client={client}
          disabled={serviceUnavailable}
          onclaimed={(share) => {
            showToast({ message: `Share ${share.share_index} claimed`, type: 'success' });
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
       ENROLL OFFICER
       =========================================================================== -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-6 gap-4">
      <h3 class="text-sm font-semibold">Enroll Officer</h3>

      <form
        class="space-y-3"
        data-testid="ceremony-enroll-form"
        onsubmit={(e) => { e.preventDefault(); void submitEnroll(); }}
      >
        {#if enrollError !== null}
          <div class="alert alert-error text-sm" role="alert">{enrollError}</div>
        {/if}

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
          <label class="label label-text" for="ceremony-enroll-file">Upload CSR</label>
          <input
            id="ceremony-enroll-file"
            class="file-input file-input-bordered file-input-sm"
            type="file"
            accept=".pem,.csr,.txt"
            onchange={onEnrollFile}
          />
        </div>

        <div class="form-control">
          <label class="label label-text" for="ceremony-enroll-csr">CSR (PEM)</label>
          <textarea
            id="ceremony-enroll-csr"
            class="textarea textarea-bordered font-mono text-xs"
            rows="4"
            placeholder="Paste or upload the officer's PEM CSR..."
            bind:value={enrollCSR}
          ></textarea>
        </div>

        <button
          class="btn btn-primary btn-sm"
          type="submit"
          disabled={enrolling || serviceUnavailable}
          title={serviceUnavailable ? 'Ceremony service is not active. Run `quicbaasd init` first.' : undefined}
        >
          {enrolling ? 'Enrolling...' : 'Enroll Officer'}
        </button>
      </form>
    </div>
  </div>
</div>
