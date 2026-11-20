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
  AttestationView is the top-level Security > Attestation route component.

  It manages the attestation verifier's trust material through
  `client.attestation`:
    - Endorsements (the reference-value signers and attestation-key roots the
      verifier accepts): list with a kind filter, add (a certificate or a
      public key PEM), and delete after a confirmation dialog.
    - Reference values (signed CoRIM manifests): list, upload a signed CoRIM
      file (sent base64), and delete after a confirmation dialog.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type { Endorsement, EndorsementKind, ReferenceValues } from '@quicbaas/sdk';
  import { BadgeCheck } from 'lucide-svelte';

  type KindFilter = '' | EndorsementKind;
  type PemField = 'certificate_pem' | 'public_key_pem';
  type Pending = { kind: 'endorsement'; name: string } | { kind: 'reference'; name: string };

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let endorsements: Endorsement[] = $state([]);
  let endorsementsLoading = $state(false);
  let endorsementsError: string | null = $state(null);
  let kindFilter: KindFilter = $state('');

  let referenceValues: ReferenceValues[] = $state([]);
  let referenceLoading = $state(false);
  let referenceError: string | null = $state(null);

  let addName = $state('');
  let addKind: EndorsementKind = $state('reference-signer');
  let addPemField: PemField = $state('certificate_pem');
  let addPem = $state('');
  let adding = $state(false);

  let uploadName = $state('');
  let uploadFiles: FileList | undefined = $state(undefined);
  let uploading = $state(false);

  let pending: Pending | null = $state(null);
  let deleting = $state(false);

  const ENDORSEMENT_COLUMNS: Column<Endorsement>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'kind', label: 'Kind', sortable: true, searchable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  const REFERENCE_COLUMNS: Column<ReferenceValues>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'manifest_id', label: 'Manifest ID', searchable: true },
    { key: 'profile', label: 'Profile', searchable: true },
    { key: 'signer', label: 'Signer', searchable: true },
    { key: 'not_before', label: 'Not before', sortable: true },
    { key: 'not_after', label: 'Not after', sortable: true },
    { key: 'pcr_indices', label: 'PCR indices' },
    { key: 'stored_at', label: 'Stored', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadEndorsements(): Promise<void> {
    endorsementsLoading = true;
    endorsementsError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.attestation.listEndorsements(kindFilter || undefined);
      endorsements = resp.endorsements ?? [];
    } catch (e) {
      endorsementsError = e instanceof Error ? e.message : 'Failed to load endorsements';
    } finally {
      endorsementsLoading = false;
    }
  }

  async function loadReferenceValues(): Promise<void> {
    referenceLoading = true;
    referenceError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.attestation.listReferenceValues();
      referenceValues = resp.reference_values ?? [];
    } catch (e) {
      referenceError = e instanceof Error ? e.message : 'Failed to load reference values';
    } finally {
      referenceLoading = false;
    }
  }

  $effect(() => {
    void loadEndorsements();
    void loadReferenceValues();
  });

  // ---------------------------------------------------------------------------
  // Add / upload
  // ---------------------------------------------------------------------------

  async function addEndorsement(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const name = addName.trim();
    const pem = addPem.trim();
    if (!name || !pem) return;
    adding = true;
    try {
      const client = await getSDKClient();
      const req = addPemField === 'certificate_pem'
        ? { kind: addKind, certificate_pem: pem }
        : { kind: addKind, public_key_pem: pem };
      await client.attestation.putEndorsement(name, req);
      showToast('success', `Endorsement ${name} saved`);
      addName = '';
      addPem = '';
      void loadEndorsements();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Saving the endorsement failed');
    } finally {
      adding = false;
    }
  }

  function readBase64(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => {
        const url = typeof reader.result === 'string' ? reader.result : '';
        const comma = url.indexOf(',');
        resolve(comma >= 0 ? url.slice(comma + 1) : url);
      };
      reader.onerror = () => reject(reader.error ?? new Error('Reading the file failed'));
      reader.readAsDataURL(file);
    });
  }

  async function uploadReferenceValues(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const name = uploadName.trim();
    const file = uploadFiles?.[0];
    if (!name || !file) return;
    uploading = true;
    try {
      const signedCoRIM = await readBase64(file);
      const client = await getSDKClient();
      await client.attestation.putReferenceValues(name, signedCoRIM);
      showToast('success', `Reference values ${name} stored`);
      uploadName = '';
      uploadFiles = undefined;
      void loadReferenceValues();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Uploading the reference values failed');
    } finally {
      uploading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete
  // ---------------------------------------------------------------------------

  async function confirmDelete(): Promise<void> {
    if (!pending) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      if (pending.kind === 'endorsement') {
        await client.attestation.deleteEndorsement(pending.name);
        showToast('success', `Endorsement ${pending.name} deleted`);
        pending = null;
        void loadEndorsements();
      } else {
        await client.attestation.deleteReferenceValues(pending.name);
        showToast('success', `Reference values ${pending.name} deleted`);
        pending = null;
        void loadReferenceValues();
      }
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Delete failed');
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function formatDateTime(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  function formatPCRs(pcrs: number[] | undefined): string {
    return pcrs && pcrs.length > 0 ? pcrs.join(', ') : '—';
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Attestation" subtitle="Endorsements and reference values the attestation verifier trusts" />

  <!-- ===================================================================== -->
  <!-- Endorsements                                                          -->
  <!-- ===================================================================== -->
  <section class="space-y-3" aria-label="Endorsements">
    <div class="flex items-center justify-between gap-2">
      <h2 class="text-lg font-semibold">Endorsements</h2>
      <label class="flex items-center gap-2 text-sm">
        <span>Kind</span>
        <select
          class="select select-bordered select-sm"
          aria-label="Endorsement kind filter"
          bind:value={kindFilter}
          onchange={() => { void loadEndorsements(); }}
        >
          <option value="">All</option>
          <option value="reference-signer">reference-signer</option>
          <option value="ak-root">ak-root</option>
        </select>
      </label>
    </div>

    <form class="card bg-base-200 p-4 space-y-2" aria-label="Add endorsement" onsubmit={addEndorsement}>
      <div class="flex flex-wrap gap-2">
        <input
          class="input input-bordered input-sm"
          placeholder="Name"
          aria-label="Endorsement name"
          bind:value={addName}
        />
        <select class="select select-bordered select-sm" aria-label="Endorsement kind" bind:value={addKind}>
          <option value="reference-signer">reference-signer</option>
          <option value="ak-root">ak-root</option>
        </select>
        <label class="flex items-center gap-1 text-sm">
          <input type="radio" class="radio radio-sm" name="pem-field" value="certificate_pem" bind:group={addPemField} />
          Certificate
        </label>
        <label class="flex items-center gap-1 text-sm">
          <input type="radio" class="radio radio-sm" name="pem-field" value="public_key_pem" bind:group={addPemField} />
          Public key
        </label>
      </div>
      <textarea
        class="textarea textarea-bordered w-full font-mono text-xs"
        rows="4"
        placeholder="-----BEGIN CERTIFICATE-----"
        aria-label="PEM"
        bind:value={addPem}
      ></textarea>
      <button
        type="submit"
        class="btn btn-primary btn-sm"
        disabled={adding || !addName.trim() || !addPem.trim()}
      >
        {adding ? 'Adding…' : 'Add Endorsement'}
      </button>
    </form>

    {#if endorsementsError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{endorsementsError}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => { void loadEndorsements(); }}>Retry</button>
      </div>
    {/if}

    {#if endorsementsLoading}
      <div class="overflow-x-auto rounded-box">
        <table class="table table-sm" aria-label="Endorsements loading">
          <thead>
            <tr><th>Name</th><th>Kind</th><th>Created</th><th></th></tr>
          </thead>
          <tbody>
            {#each [1, 2, 3] as i (i)}
              <tr>
                <td><div class="skeleton h-4 w-32"></div></td>
                <td><div class="skeleton h-4 w-24"></div></td>
                <td><div class="skeleton h-4 w-24"></div></td>
                <td><div class="skeleton h-4 w-16"></div></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else if !endorsementsError}
      <DataTable
        columns={ENDORSEMENT_COLUMNS}
        rows={endorsements}
        rowKey={(e: Endorsement) => e.name}
        urlKey="endorsements"
        ariaLabel="Endorsements"
        searchPlaceholder="Search endorsements..."
        searchLabel="Search endorsements"
        noMatchText="No endorsements match the search."
      >
        {#snippet empty()}
          <EmptyState
            icon={BadgeCheck}
            title="No endorsements"
            description="Add a reference-value signer or an attestation-key root."
          />
        {/snippet}

        {#snippet row(e: Endorsement)}
          <tr class="hover">
            <td class="text-sm">{e.name}</td>
            <td><span class="badge badge-ghost badge-sm">{e.kind}</span></td>
            <td class="text-xs text-base-content/50">{formatDateTime(e.created_at)}</td>
            <td>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={() => { pending = { kind: 'endorsement', name: e.name }; }}
                aria-label={`Delete endorsement ${e.name}`}
              >
                Delete
              </button>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  </section>

  <!-- ===================================================================== -->
  <!-- Reference values                                                      -->
  <!-- ===================================================================== -->
  <section class="space-y-3" aria-label="Reference values">
    <h2 class="text-lg font-semibold">Reference Values</h2>

    <form class="card bg-base-200 p-4 space-y-2" aria-label="Upload reference values" onsubmit={uploadReferenceValues}>
      <div class="flex flex-wrap gap-2 items-center">
        <input
          class="input input-bordered input-sm"
          placeholder="Name"
          aria-label="Reference values name"
          bind:value={uploadName}
        />
        <input
          type="file"
          class="file-input file-input-bordered file-input-sm"
          aria-label="Signed CoRIM file"
          bind:files={uploadFiles}
        />
        <button
          type="submit"
          class="btn btn-primary btn-sm"
          disabled={uploading || !uploadName.trim() || !uploadFiles || uploadFiles.length === 0}
        >
          {uploading ? 'Uploading…' : 'Upload'}
        </button>
      </div>
    </form>

    {#if referenceError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{referenceError}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => { void loadReferenceValues(); }}>Retry</button>
      </div>
    {/if}

    {#if referenceLoading}
      <div class="overflow-x-auto rounded-box">
        <table class="table table-sm" aria-label="Reference values loading">
          <thead>
            <tr>
              <th>Name</th><th>Manifest ID</th><th>Profile</th><th>Signer</th>
              <th>Not before</th><th>Not after</th><th>PCR indices</th><th>Stored</th><th></th>
            </tr>
          </thead>
          <tbody>
            {#each [1, 2, 3] as i (i)}
              <tr>
                {#each [1, 2, 3, 4, 5, 6, 7, 8, 9] as j (j)}
                  <td><div class="skeleton h-4 w-16"></div></td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else if !referenceError}
      <DataTable
        columns={REFERENCE_COLUMNS}
        rows={referenceValues}
        rowKey={(r: ReferenceValues) => r.name}
        urlKey="referencevalues"
        ariaLabel="Reference values"
        searchPlaceholder="Search reference values..."
        searchLabel="Search reference values"
        noMatchText="No reference values match the search."
      >
        {#snippet empty()}
          <EmptyState
            icon={BadgeCheck}
            title="No reference values"
            description="Upload a signed CoRIM manifest."
          />
        {/snippet}

        {#snippet row(r: ReferenceValues)}
          <tr class="hover">
            <td class="text-sm">{r.name}</td>
            <td class="font-mono text-xs">{r.manifest_id}</td>
            <td class="text-xs">{r.profile || '—'}</td>
            <td class="text-xs">{r.signer || '—'}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(r.not_before)}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(r.not_after)}</td>
            <td class="font-mono text-xs">{formatPCRs(r.pcr_indices)}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(r.stored_at)}</td>
            <td>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={() => { pending = { kind: 'reference', name: r.name }; }}
                aria-label={`Delete reference values ${r.name}`}
              >
                Delete
              </button>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  </section>
</div>

<!-- Delete confirmation dialog -->
{#if pending !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">
        {pending.kind === 'endorsement' ? 'Delete Endorsement' : 'Delete Reference Values'}
      </h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-semibold">{pending.name}</span>? The verifier stops trusting it.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { pending = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deleting} onclick={confirmDelete}>
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { pending = null; }}
      onkeydown={() => { pending = null; }}
    ></div>
  </div>
{/if}
