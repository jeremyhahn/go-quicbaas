<!--
  CreateTaskDialog presents a modal form for creating a new orchestration task.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { CreateTaskRequest, FleetNode } from '@quicbaas/sdk';

  let {
    open = $bindable(false),
    onCreated,
  }: {
    open: boolean;
    onCreated?: () => void;
  } = $props();

  let subject = $state('');
  let teamName = $state('');
  let description = $state('');
  let hostAffinity = $state('');
  let hostAffinityStrict = $state(false);
  let gpuRequired = $state(false);
  let autoSpawn = $state(false);
  let backend = $state('');
  // promptAppend is run-time material every attempt at the task carries to
  // the sessions it spawns (prompt_append).
  let promptAppend = $state('');
  let submitting = $state(false);
  let errorMsg = $state('');

  // Agent list for host affinity dropdown.
  let agents: FleetNode[] = $state([]);
  let agentsLoading = $state(false);

  onMount(() => {
    void loadFleet();
  });

  async function loadFleet(): Promise<void> {
    agentsLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.listFleetNodes({
        tenant_id: orchestratorStore.activeTenantID,
      });
      agents = resp.nodes ?? [];
    } catch {
      agents = [];
    } finally {
      agentsLoading = false;
    }
  }

  function resetForm(): void {
    subject = '';
    teamName = '';
    description = '';
    hostAffinity = '';
    hostAffinityStrict = false;
    gpuRequired = false;
    autoSpawn = false;
    backend = '';
    promptAppend = '';
    errorMsg = '';
  }

  function handleClose(): void {
    open = false;
    resetForm();
  }

  async function handleSubmit(): Promise<void> {
    if (!subject.trim()) {
      errorMsg = 'Subject is required.';
      return;
    }

    submitting = true;
    errorMsg = '';

    const req: CreateTaskRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      subject: subject.trim(),
      team_name: teamName.trim(),
      description: description.trim() || undefined,
      host_affinity: hostAffinity || undefined,
      host_affinity_strict: hostAffinityStrict || undefined,
      gpu_required: gpuRequired || undefined,
      auto_spawn: autoSpawn || undefined,
      backend: (autoSpawn && backend.trim()) ? backend.trim() : undefined,
      prompt_append: promptAppend.trim() !== '' ? promptAppend : undefined,
    };

    try {
      const client = await getSDKClient();
      await client.orchestrator.createTask(req);
      showToast('success', `Task "${subject.trim()}" created`);
      open = false;
      resetForm();
      onCreated?.();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      errorMsg = `Failed to create task: ${msg}`;
    } finally {
      submitting = false;
    }
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    open
    aria-modal="true"
    aria-label="Create task"
    onkeydown={(e) => { if (e.key === 'Escape') handleClose(); }}
  >
    <div class="modal-box w-full max-w-lg">
      <h3 class="text-base font-semibold mb-4">New Task</h3>

      <form
        onsubmit={(e) => { e.preventDefault(); void handleSubmit(); }}
        class="space-y-4"
        novalidate
      >
        <!-- Subject (required) -->
        <div class="form-control">
          <label class="label" for="ct-subject">
            <span class="label-text">Subject <span class="text-error">*</span></span>
          </label>
          <input
            id="ct-subject"
            type="text"
            class="input input-bordered input-sm"
            placeholder="Brief task subject"
            bind:value={subject}
          />
        </div>

        <!-- Team name -->
        <div class="form-control">
          <label class="label" for="ct-team">
            <span class="label-text">Team Name</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          <input
            id="ct-team"
            type="text"
            class="input input-bordered input-sm"
            placeholder="e.g. backend, frontend"
            bind:value={teamName}
          />
        </div>

        <!-- Description -->
        <div class="form-control">
          <label class="label" for="ct-desc">
            <span class="label-text">Description</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          <textarea
            id="ct-desc"
            class="textarea textarea-bordered text-sm"
            rows="3"
            placeholder="Detailed task description..."
            bind:value={description}
          ></textarea>
        </div>

        <!-- Run-time prompt material for every attempt's sessions -->
        <div class="form-control">
          <label class="label" for="ct-prompt-append">
            <span class="label-text">Append to the agent's prompt</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          <textarea
            id="ct-prompt-append"
            class="textarea textarea-bordered text-sm font-mono"
            rows="3"
            placeholder="Context every attempt's sessions read after their agent's own prompt"
            bind:value={promptAppend}
            data-testid="create-task-prompt-append"
          ></textarea>
        </div>

        <!-- Host affinity -->
        <div class="form-control">
          <label class="label" for="ct-host">
            <span class="label-text">Host Affinity</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          {#if agentsLoading}
            <div class="skeleton h-8 w-full rounded"></div>
          {:else}
            <select
              id="ct-host"
              class="select select-bordered select-sm"
              bind:value={hostAffinity}
              aria-label="Host affinity — pin task to a specific fleet node (optional)"
            >
              <option value="">Any</option>
              {#each agents as agent (agent.id ?? agent.name)}
                <option value={agent.name}>{agent.name} @ {agent.host}</option>
              {/each}
            </select>
          {/if}
          {#if hostAffinity}
            <label class="label cursor-pointer mt-2" for="ct-host-strict">
              <span class="label-text-alt">Strict — fail if host offline</span>
              <input
                id="ct-host-strict"
                type="checkbox"
                class="checkbox checkbox-xs"
                bind:checked={hostAffinityStrict}
                aria-label="Fail the request if the selected host is offline instead of falling back to any fleet node"
              />
            </label>
          {/if}
        </div>

        <!-- GPU required toggle -->
        <div class="form-control">
          <label class="label cursor-pointer" for="ct-gpu">
            <span class="label-text">GPU Required</span>
            <input
              id="ct-gpu"
              type="checkbox"
              class="toggle toggle-sm toggle-primary"
              bind:checked={gpuRequired}
              aria-label="Require a GPU-capable fleet node for this task"
            />
          </label>
          {#if gpuRequired}
            <p class="text-xs text-base-content/40 -mt-1">
              Task will only be assigned to fleet nodes that advertise GPU capability.
            </p>
          {/if}
        </div>

        <!-- Run immediately (auto-spawn) toggle -->
        <div class="form-control">
          <label class="label cursor-pointer" for="ct-auto-spawn">
            <div>
              <span class="label-text font-medium">Run immediately</span>
              <p class="text-xs text-base-content/40">
                If enabled, the task spawns a one-shot job on the best-matching agent.
                If disabled, it waits in the queue until claimed.
              </p>
            </div>
            <input
              id="ct-auto-spawn"
              type="checkbox"
              class="toggle toggle-sm toggle-secondary"
              bind:checked={autoSpawn}
              aria-label="Spawn a job immediately on the best-matching fleet node"
            />
          </label>
        </div>

        <!-- Backend hint for auto-spawn -->
        {#if autoSpawn}
          <div class="form-control">
            <label class="label" for="ct-backend">
              <span class="label-text">Backend</span>
              <span class="label-text-alt text-base-content/40">routing hint</span>
            </label>
            <input
              id="ct-backend"
              type="text"
              class="input input-bordered input-sm"
              placeholder="e.g. ollama, claude (optional)"
              bind:value={backend}
              aria-label="Backend to route the auto-spawned job to (optional)"
            />
            <p class="text-xs text-base-content/40 mt-1">
              Host Affinity and GPU selections above are also used as routing hints.
            </p>
          </div>
        {/if}

        {#if errorMsg}
          <div class="alert alert-error text-sm py-2">
            <span>{errorMsg}</span>
          </div>
        {/if}

        <div class="modal-action mt-2">
          <button
            type="button"
            class="btn btn-sm btn-ghost"
            onclick={handleClose}
            disabled={submitting}
          >
            Cancel
          </button>
          <button
            type="submit"
            class="btn btn-sm btn-primary"
            disabled={submitting}
          >
            {#if submitting}
              <span class="loading loading-spinner loading-xs"></span>
            {/if}
            {autoSpawn ? 'Create & Run Now' : 'Create Task'}
          </button>
        </div>
      </form>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={handleClose} onkeydown={() => {}}></div>
  </dialog>
{/if}
