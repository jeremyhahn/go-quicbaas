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
  TrustView renders an operator console for the Trust composition surface —
  Security().Trust() — covering cross-signed root pool retrieval, AIA
  auto-import, and DANE TLSA publication. This is a pure composition layer
  over the xkms TrustedCA; it does not reimplement cross-signing, AIA
  walking, or TLSA hashing. See BootstrapView for the related first-contact
  trust bootstrap wizard (DANE/Noise/SPKI CA-bundle retrieval), which is a
  separate concern from the composition operations here.

  Actions:
    - View the CA's cross-sign-aware root pool
    - Auto-import issuing CAs from a certificate chain's AIA extension
    - Publish DANE TLSA records for a certificate at name:port
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { routeBootstrap } from '../../stores/router.svelte.ts';
  import { ShieldCheck, RefreshCw, Upload, Radio, ExternalLink } from 'lucide-svelte';
  import { downloadText } from '../../lib/download.ts';

  // ---------------------------------------------------------------------------
  // Cross-signed root pool
  // ---------------------------------------------------------------------------

  let rootPEMs = $state<string[]>([]);
  let rootPoolLoading = $state(false);
  let rootPoolError = $state('');

  async function loadRootPool(): Promise<void> {
    rootPoolLoading = true;
    rootPoolError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.trust.crossSignedRootPool();
      rootPEMs = resp.root_pems;
    } catch (err: unknown) {
      rootPoolError = err instanceof Error ? err.message : String(err);
    } finally {
      rootPoolLoading = false;
    }
  }

  $effect(() => { void loadRootPool(); });

  // ---------------------------------------------------------------------------
  // AIA auto-import
  // ---------------------------------------------------------------------------

  let chainPEMsText = $state('');
  let importingAIA = $state(false);
  let aiaError = $state('');
  let aiaResult = $state<number | null>(null);

  function parsePEMChain(text: string): string[] {
    const matches = text.match(/-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----/g);
    return matches ?? [];
  }

  async function submitAutoImportAIA(): Promise<void> {
    const chain = parsePEMChain(chainPEMsText);
    if (chain.length === 0) {
      aiaError = 'Paste at least one PEM-encoded certificate.';
      return;
    }
    importingAIA = true;
    aiaError = '';
    aiaResult = null;
    try {
      const client = await getSDKClient();
      const resp = await client.trust.autoImportAIA({ chain_pems: chain });
      aiaResult = resp.imported;
      showToast('success', `Imported ${resp.imported} issuing CA(s) from AIA`);
    } catch (err: unknown) {
      aiaError = err instanceof Error ? err.message : String(err);
    } finally {
      importingAIA = false;
    }
  }

  // ---------------------------------------------------------------------------
  // DANE publication
  // ---------------------------------------------------------------------------

  let danePEM = $state('');
  let daneName = $state('');
  let danePort = $state(443);
  let publishingDANE = $state(false);
  let daneError = $state('');
  let daneResult = $state<number | null>(null);

  async function submitPublishDANE(): Promise<void> {
    if (!danePEM.trim() || !daneName.trim()) {
      daneError = 'Certificate PEM and DNS name are required.';
      return;
    }
    publishingDANE = true;
    daneError = '';
    daneResult = null;
    try {
      const client = await getSDKClient();
      const resp = await client.trust.publishDANE({
        name: daneName.trim(),
        port: danePort,
        cert_pem: danePEM.trim(),
      });
      daneResult = resp.published;
      showToast('success', `Published ${resp.published} DANE TLSA record(s)`);
    } catch (err: unknown) {
      daneError = err instanceof Error ? err.message : String(err);
    } finally {
      publishingDANE = false;
    }
  }
</script>

<div class="p-6 space-y-6 max-w-4xl mx-auto">
  <PageHeader title="Trust" subtitle="Cross-Sign Root Pool, AIA Auto-Import, and DANE Publication" icon={ShieldCheck}>
    {#snippet children()}
      <button class="btn btn-ghost btn-sm gap-1" onclick={routeBootstrap} aria-label="Go to trust bootstrap wizard">
        <ExternalLink class="h-3.5 w-3.5" />
        Bootstrap Wizard
      </button>
    {/snippet}
  </PageHeader>

  <!-- Cross-signed root pool -->
  <div class="card bg-base-200 rounded-box p-4">
    <div class="flex items-center justify-between mb-3">
      <h3 class="text-sm font-semibold">Cross-Signed Root Pool</h3>
      <button class="btn btn-ghost btn-xs gap-1" onclick={() => void loadRootPool()} disabled={rootPoolLoading} aria-label="Refresh root pool">
        <RefreshCw class="h-3 w-3 {rootPoolLoading ? 'animate-spin' : ''}" />
        Refresh
      </button>
    </div>
    {#if rootPoolError}
      <div class="alert alert-error text-sm" role="alert">{rootPoolError}</div>
    {:else if rootPoolLoading && rootPEMs.length === 0}
      <div class="skeleton h-16 w-full rounded-box"></div>
    {:else if rootPEMs.length === 0}
      <p class="text-sm text-base-content/40">No cross-signed roots configured.</p>
    {:else}
      <div class="space-y-2">
        {#each rootPEMs as pem, i (i)}
          <details class="collapse collapse-arrow bg-base-100 rounded-box">
            <summary class="collapse-title text-xs font-mono py-2 min-h-0">Root #{i + 1}</summary>
            <div class="collapse-content">
              <div class="flex justify-end mb-1">
                <button class="btn btn-ghost btn-xs" onclick={() => downloadText(`cross-signed-root-${i + 1}.pem`, pem)}>
                  Download
                </button>
              </div>
              <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto max-h-40">{pem}</pre>
            </div>
          </details>
        {/each}
      </div>
    {/if}
  </div>

  <!-- AIA auto-import -->
  <div class="card bg-base-200 rounded-box p-4">
    <h3 class="text-sm font-semibold mb-1 flex items-center gap-2"><Upload class="h-4 w-4" /> AIA Auto-Import</h3>
    <p class="text-xs text-base-content/50 mb-3">
      Paste one or more PEM-encoded certificates. Their Authority Information Access (AIA)
      extensions will be walked and the referenced issuing CAs imported into the CA's
      certificate store.
    </p>
    <div class="form-control">
      <label class="label label-text" for="aia-chain">Certificate Chain (PEM)</label>
      <textarea
        id="aia-chain"
        class="textarea textarea-bordered font-mono text-xs"
        rows="8"
        placeholder="-----BEGIN CERTIFICATE-----"
        bind:value={chainPEMsText}
      ></textarea>
    </div>
    {#if aiaError}
      <div class="alert alert-error text-sm mt-2" role="alert">{aiaError}</div>
    {/if}
    {#if aiaResult !== null}
      <div class="alert alert-success text-sm mt-2" role="alert">Imported {aiaResult} issuing CA(s).</div>
    {/if}
    <div class="mt-3">
      <button class="btn btn-primary btn-sm" disabled={importingAIA} onclick={() => void submitAutoImportAIA()}>
        {importingAIA ? 'Importing…' : 'Import Issuing CAs'}
      </button>
    </div>
  </div>

  <!-- DANE publication -->
  <div class="card bg-base-200 rounded-box p-4">
    <h3 class="text-sm font-semibold mb-1 flex items-center gap-2"><Radio class="h-4 w-4" /> DANE TLSA Publication</h3>
    <p class="text-xs text-base-content/50 mb-3">
      Derives DANE TLSA records for the certificate and hands them to the server's registered
      DANE publisher. Returns a typed "not configured" error when no publisher is registered.
    </p>
    <div class="grid grid-cols-2 gap-3 mb-3">
      <div class="form-control">
        <label class="label label-text" for="dane-name">DNS Name</label>
        <input id="dane-name" class="input input-bordered input-sm" type="text" placeholder="app.example.com" bind:value={daneName} />
      </div>
      <div class="form-control">
        <label class="label label-text" for="dane-port">Port</label>
        <input id="dane-port" class="input input-bordered input-sm" type="number" min="1" max="65535" bind:value={danePort} />
      </div>
    </div>
    <div class="form-control">
      <label class="label label-text" for="dane-pem">Certificate (PEM)</label>
      <textarea
        id="dane-pem"
        class="textarea textarea-bordered font-mono text-xs"
        rows="6"
        placeholder="-----BEGIN CERTIFICATE-----"
        bind:value={danePEM}
      ></textarea>
    </div>
    {#if daneError}
      <div class="alert alert-error text-sm mt-2" role="alert">{daneError}</div>
    {/if}
    {#if daneResult !== null}
      <div class="alert alert-success text-sm mt-2" role="alert">Published {daneResult} TLSA record(s).</div>
    {/if}
    <div class="mt-3">
      <button class="btn btn-primary btn-sm" disabled={publishingDANE} onclick={() => void submitPublishDANE()}>
        {publishingDANE ? 'Publishing…' : 'Publish DANE Records'}
      </button>
    </div>
  </div>
</div>
