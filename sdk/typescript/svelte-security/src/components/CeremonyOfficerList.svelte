<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  CeremonyOfficerList enumerates the officers participating in a key
  ceremony. Each row shows the username, claim status, share index, and
  authentication factor; rows are colour-coded by claim_status.
-->
<script lang="ts">
  import type { CeremonyOfficer, OfficerClaimStatus } from '../types.ts';

  interface Props {
    officers: CeremonyOfficer[];
    loading?: boolean;
    onselect?: (officer: CeremonyOfficer) => void;
  }

  const { officers, loading = false, onselect }: Props = $props();

  const statusBadgeClass: Record<OfficerClaimStatus, string> = {
    pending:    'badge-ghost',
    challenged: 'badge-info',
    claimed:    'badge-warning',
    enrolled:   'badge-success',
    revoked:    'badge-error',
  };
</script>

<div class="overflow-x-auto" data-testid="ceremony-officer-list">
  <table class="table table-sm">
    <thead>
      <tr>
        <th>Username</th>
        <th>Status</th>
        <th>Share #</th>
        <th>Auth Factor</th>
        <th>Kind</th>
        <th></th>
      </tr>
    </thead>
    <tbody>
      {#if loading}
        <tr>
          <td colspan="6" class="text-center py-4">
            <span class="loading loading-spinner loading-sm"></span>
          </td>
        </tr>
      {:else if officers.length === 0}
        <tr>
          <td colspan="6" class="text-center text-base-content/40 py-4">
            No officers enrolled.
          </td>
        </tr>
      {:else}
        {#each officers as officer (officer.username)}
          <tr data-testid="officer-row" data-username={officer.username}>
            <td class="font-mono text-xs">{officer.username}</td>
            <td>
              <span
                class="badge badge-sm {statusBadgeClass[officer.claim_status]}"
                data-testid="officer-status"
              >
                {officer.claim_status}
              </span>
            </td>
            <td class="font-mono text-xs">{officer.share_index}</td>
            <td class="text-xs">{officer.auth_factor}</td>
            <td>
              <span class="badge badge-xs badge-outline">{officer.kind}</span>
            </td>
            <td class="text-right">
              {#if onselect}
                <button
                  class="btn btn-ghost btn-xs"
                  aria-label="Select officer {officer.username}"
                  onclick={() => onselect(officer)}
                >
                  Inspect
                </button>
              {/if}
            </td>
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
</div>
