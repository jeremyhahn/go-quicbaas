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
  SdlcRulerPicker picks a binding's rulers from the repository's
  collaborators: the forge logins, beyond the owner and the node's configured
  rulers, whose comment on a parked issue is an owner ruling.

  A RULER IS A REFERENCE, SO IT IS PICKED, NEVER TYPED. The candidates are the
  repository's own collaborators as the forge reports them; the server refuses
  anyone else at bind time. A saved ruler who has since lost access stays
  visible and flagged, so the operator sees why the next save would fail.
-->
<script lang="ts">
  import type { ForgeUser } from '@quicbaas/sdk';
  import { rulerCandidates, rulerHasAccess } from '../../lib/sdlcBinding.ts';
  import { X } from 'lucide-svelte';

  let {
    idPrefix,
    rulers = $bindable(),
    owner,
    collaborators,
    status,
    error = '',
  }: {
    idPrefix: string;
    rulers: string[];
    owner: string;
    collaborators: ForgeUser[];
    status: 'loading' | 'ready' | 'unbound' | 'error';
    error?: string;
  } = $props();

  let pick = $state('');

  const candidates = $derived(rulerCandidates(collaborators, owner, rulers));

  function add(): void {
    if (pick === '') return;
    rulers = [...rulers, pick];
    pick = '';
  }

  function remove(index: number): void {
    rulers = rulers.filter((_, i) => i !== index);
  }
</script>

<div data-testid="sdlc-rulers">
  <span class="label-text text-xs">Rulers</span>
  <p class="text-xs text-base-content/40 mt-1 mb-2">
    Whose comment on a parked issue readmits the task, beyond the node's configured rulers.
  </p>
  <div class="flex flex-wrap gap-1 mb-2">
    {#if owner}
      <span class="badge badge-sm badge-ghost" title="The repository's owner always rules.">{owner} · owner</span>
    {/if}
    {#each rulers as ruler, i (ruler)}
      {@const hasAccess = status !== 'ready' || rulerHasAccess(ruler, owner, collaborators)}
      <span
        class="badge badge-sm gap-1 {hasAccess ? 'badge-primary badge-outline' : 'badge-warning'}"
        title={hasAccess ? '' : `${ruler} is no longer a collaborator; saving will be refused.`}
        data-testid="sdlc-ruler-chip"
      >
        {ruler}
        <button
          type="button"
          class="btn btn-ghost btn-xs btn-circle min-h-0 h-4 w-4"
          onclick={() => { remove(i); }}
          aria-label="Remove ruler {ruler}"
        >
          <X class="h-3 w-3" />
        </button>
      </span>
    {/each}
  </div>
  {#if status === 'loading'}
    <p class="text-xs text-base-content/40">Reading the repository's collaborators…</p>
  {:else if status === 'unbound'}
    <p class="text-xs text-base-content/40">Bind the project first; rulers are picked from its repository's collaborators.</p>
  {:else if status === 'error'}
    <p class="text-xs text-error">{error || "Could not read the repository's collaborators."}</p>
  {:else}
    <div class="flex items-center gap-2">
      <select
        id="{idPrefix}-sdlc-ruler-pick"
        class="select select-bordered select-xs w-full"
        aria-label="Add a ruler"
        bind:value={pick}
        disabled={candidates.length === 0}
      >
        <option value="">{candidates.length === 0 ? 'No other collaborators' : 'Choose a collaborator…'}</option>
        {#each candidates as user (user.login)}
          <option value={user.login}>{user.login}{user.full_name ? ` — ${user.full_name}` : ''}</option>
        {/each}
      </select>
      <button class="btn btn-xs btn-ghost" type="button" onclick={add} disabled={pick === ''}>Add</button>
    </div>
  {/if}
</div>
