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
  AgentEditDialog creates an Agent or edits one. It shows the fields an
  operator sets by hand: id (create only), name, description, roles, backend,
  model and reasoning effort. It hands the edited fields to onsubmit; the
  caller merges them onto the stored record, so the fields this form does not
  show are kept. A missing required field is refused here with no call; the
  server's refusals (an unknown model, a role a slot still uses) are shown as
  returned.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import type { Agent, AgentRole } from '@quicbaas/sdk';

  const ROLES: AgentRole[] = ['coding', 'review', 'triage', 'lead', 'project_manager', 'general'];
  const EFFORTS = ['minimal', 'low', 'medium', 'high', 'xhigh'];

  interface Props {
    mode: 'create' | 'edit';
    initial?: Agent | null;
    onsubmit: (fields: Agent) => Promise<void>;
    oncancel: () => void;
  }

  let { mode, initial = null, onsubmit, oncancel }: Props = $props();

  // The form is seeded once from the record it opened with.
  const seed = untrack(() => initial);
  let id = $state(seed?.id ?? '');
  let name = $state(seed?.name ?? '');
  let description = $state(seed?.description ?? '');
  let roles = $state<AgentRole[]>([...(seed?.roles ?? [])]);
  let backend = $state(seed?.backend ?? '');
  let model = $state(seed?.model ?? '');
  let reasoningEffort = $state(seed?.reasoning_effort ?? '');
  let saving = $state(false);
  let error = $state('');

  const effortOptions = $derived(
    reasoningEffort && !EFFORTS.includes(reasoningEffort) ? [...EFFORTS, reasoningEffort] : EFFORTS,
  );

  function toggleRole(role: AgentRole, on: boolean): void {
    roles = on ? ROLES.filter((r) => r === role || roles.includes(r)) : roles.filter((r) => r !== role);
  }

  function missing(): string {
    if (!id.trim()) return 'ID is required.';
    if (!name.trim()) return 'Name is required.';
    if (roles.length === 0) return 'Choose at least one role.';
    if (!backend.trim()) return 'Backend is required.';
    if (!model.trim()) return 'Model is required.';
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
        roles: [...roles],
        backend: backend.trim(),
        model: model.trim(),
        description: description.trim() || undefined,
        reasoning_effort: reasoningEffort || undefined,
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
  aria-label={mode === 'create' ? 'New agent' : 'Edit agent'}
  data-testid="agent-edit-dialog"
  onkeydown={(e) => { if (e.key === 'Escape' && !saving) oncancel(); }}
>
  <div class="modal-box max-w-lg">
    <h3 class="font-bold text-base">{mode === 'create' ? 'New agent' : `Edit agent ${id}`}</h3>
    <div class="mt-3 space-y-3">
      <label class="form-control w-full">
        <span class="label-text text-xs">ID</span>
        <input id="agent-id" class="input input-sm input-bordered w-full" bind:value={id} readonly={mode === 'edit'} />
      </label>
      <label class="form-control w-full">
        <span class="label-text text-xs">Name</span>
        <input id="agent-name" class="input input-sm input-bordered w-full" bind:value={name} />
      </label>
      <label class="form-control w-full">
        <span class="label-text text-xs">Description (optional)</span>
        <input id="agent-description" class="input input-sm input-bordered w-full" bind:value={description} />
      </label>
      <fieldset>
        <legend class="label-text text-xs">Roles</legend>
        <div class="flex flex-wrap gap-3 mt-1">
          {#each ROLES as role (role)}
            <label class="flex items-center gap-1 text-sm">
              <input
                type="checkbox"
                class="checkbox checkbox-xs"
                checked={roles.includes(role)}
                onchange={(e) => toggleRole(role, (e.currentTarget as HTMLInputElement).checked)}
              />
              {role}
            </label>
          {/each}
        </div>
      </fieldset>
      <label class="form-control w-full">
        <span class="label-text text-xs">Backend</span>
        <input id="agent-backend" class="input input-sm input-bordered w-full" bind:value={backend} placeholder="claude, codex, opencode" />
      </label>
      <label class="form-control w-full">
        <span class="label-text text-xs">Model</span>
        <input id="agent-model" class="input input-sm input-bordered w-full" bind:value={model} />
      </label>
      <label class="form-control w-full">
        <span class="label-text text-xs">Reasoning effort (optional)</span>
        <select id="agent-reasoning-effort" class="select select-sm select-bordered w-full" bind:value={reasoningEffort}>
          <option value="">Backend default</option>
          {#each effortOptions as effort (effort)}
            <option value={effort}>{effort}</option>
          {/each}
        </select>
      </label>
    </div>
    {#if error}
      <div class="alert alert-error text-sm py-2 mt-3" data-testid="agent-edit-error">{error}</div>
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
