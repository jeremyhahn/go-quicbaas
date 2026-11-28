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
  AgentPoolEditDialog creates an agent pool or edits one: id (create only),
  name, description and the member Agents in failover order, chosen from the
  tenant's listed Agents. It hands the edited fields to onsubmit; the caller
  merges them onto the stored record. A pool with no member is refused here
  with no call.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import type { Agent, AgentPool } from '@quicbaas/sdk';
  import { ArrowUp, ArrowDown, X } from 'lucide-svelte';

  interface Props {
    mode: 'create' | 'edit';
    initial?: AgentPool | null;
    agents: Agent[];
    onsubmit: (fields: AgentPool) => Promise<void>;
    oncancel: () => void;
  }

  let { mode, initial = null, agents, onsubmit, oncancel }: Props = $props();

  // The form is seeded once from the record it opened with.
  const seed = untrack(() => initial);
  let id = $state(seed?.id ?? '');
  let name = $state(seed?.name ?? '');
  let description = $state(seed?.description ?? '');
  let members = $state<string[]>([...(seed?.agents ?? [])]);
  let toAdd = $state('');
  let saving = $state(false);
  let error = $state('');

  const addable = $derived(agents.filter((a) => !members.includes(a.id)));

  function add(): void {
    if (!toAdd || members.includes(toAdd)) return;
    members = [...members, toAdd];
    toAdd = '';
  }

  function move(index: number, by: number): void {
    const to = index + by;
    if (to < 0 || to >= members.length) return;
    const next = [...members];
    [next[index], next[to]] = [next[to]!, next[index]!];
    members = next;
  }

  function remove(member: string): void {
    members = members.filter((m) => m !== member);
  }

  function missing(): string {
    if (!id.trim()) return 'ID is required.';
    if (!name.trim()) return 'Name is required.';
    if (members.length === 0) return 'Choose at least one member.';
    return '';
  }

  async function submit(): Promise<void> {
    error = missing();
    if (error) return;
    saving = true;
    try {
      await onsubmit({
        id: id.trim(),
        name: name.trim(),
        description: description.trim() || undefined,
        agents: [...members],
      });
    } catch (err: unknown) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
  class="modal modal-open"
  aria-modal="true"
  aria-label={mode === 'create' ? 'New pool' : 'Edit pool'}
  data-testid="pool-edit-dialog"
  onkeydown={(e) => { if (e.key === 'Escape' && !saving) oncancel(); }}
>
  <div class="modal-box max-w-lg">
    <h3 class="font-bold text-base">{mode === 'create' ? 'New pool' : `Edit pool ${id}`}</h3>
    <div class="mt-3 space-y-3">
      <label class="form-control w-full">
        <span class="label-text text-xs">ID</span>
        <input id="pool-id" class="input input-sm input-bordered w-full" bind:value={id} readonly={mode === 'edit'} />
      </label>
      <label class="form-control w-full">
        <span class="label-text text-xs">Name</span>
        <input id="pool-name" class="input input-sm input-bordered w-full" bind:value={name} />
      </label>
      <label class="form-control w-full">
        <span class="label-text text-xs">Description (optional)</span>
        <input id="pool-description" class="input input-sm input-bordered w-full" bind:value={description} />
      </label>
      <div>
        <span class="label-text text-xs">Members, in failover order</span>
        {#if members.length === 0}
          <p class="text-sm text-base-content/60 mt-1">No members yet.</p>
        {:else}
          <ol class="mt-1 space-y-1" data-testid="pool-members">
            {#each members as member, i (member)}
              <li class="flex items-center gap-2 text-sm">
                <span class="font-mono flex-1">{i + 1}. {member}</span>
                <button class="btn btn-xs btn-ghost" aria-label="Move {member} up" title="Move up" disabled={i === 0} onclick={() => move(i, -1)}>
                  <ArrowUp class="h-3 w-3" />
                </button>
                <button class="btn btn-xs btn-ghost" aria-label="Move {member} down" title="Move down" disabled={i === members.length - 1} onclick={() => move(i, 1)}>
                  <ArrowDown class="h-3 w-3" />
                </button>
                <button class="btn btn-xs btn-ghost" aria-label="Remove {member}" title="Remove" onclick={() => remove(member)}>
                  <X class="h-3 w-3" />
                </button>
              </li>
            {/each}
          </ol>
        {/if}
        <div class="flex gap-2 mt-2">
          <select id="pool-add-agent" class="select select-sm select-bordered flex-1" aria-label="Agent to add" bind:value={toAdd}>
            <option value="">Choose an Agent</option>
            {#each addable as a (a.id)}
              <option value={a.id}>{a.id} ({a.name})</option>
            {/each}
          </select>
          <button class="btn btn-sm" onclick={add} disabled={!toAdd}>Add member</button>
        </div>
      </div>
    </div>
    {#if error}
      <div class="alert alert-error text-sm py-2 mt-3" data-testid="pool-edit-error">{error}</div>
    {/if}
    <div class="modal-action">
      <button class="btn btn-sm btn-ghost" onclick={oncancel} disabled={saving}>Cancel</button>
      <button class="btn btn-sm btn-primary" onclick={() => { void submit(); }} disabled={saving}>
        {#if saving}
          <span class="loading loading-spinner loading-xs"></span>
        {/if}
        {mode === 'create' ? 'Create' : 'Save'}
      </button>
    </div>
  </div>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={() => { if (!saving) oncancel(); }} onkeydown={() => {}}></div>
</dialog>
