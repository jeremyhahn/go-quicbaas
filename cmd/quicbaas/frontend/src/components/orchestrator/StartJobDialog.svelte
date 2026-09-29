<!--
  StartJobDialog presents a modal form for starting an orchestration job.
  When prefillWorkflow is set (opened from WorkflowsView), the workflow_name
  field is pre-populated and read-only.
-->
<script lang="ts">
  import { orchestratorStore, refresh } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { navigateTo } from '../../stores/router.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import RefPicker from '../core/RefPicker.svelte';
  import type { StartJobRequest } from '@quicbaas/sdk';
  import { parseAgentRef } from '@quicbaas/sdk';

  let {
    open = $bindable(false),
    prefillWorkflow = '',
    defaultDescription = '',
  }: {
    open: boolean;
    prefillWorkflow?: string;
    // Optional default pre-filled when launching a job from a workflow row.
    defaultDescription?: string;
  } = $props();

  // Form fields matching StartJobRequest.
  let tenantID = $state(orchestratorStore.activeTenantID);
  let description = $state('');
  let workflowName = $state(prefillWorkflow);
  // lead is the Agent or pool the lead runs as, "agent:<id>" or "pool:<id>";
  // empty takes the project's default lead.
  let lead = $state('');
  let maxIterations = $state(10);
  let pollInterval = $state('2s');
  // promptAppend is run-time material appended to every session the job
  // spawns, the lead's included (prompt_append).
  let promptAppend = $state('');

  // leadChoices are the tenant's Agents that declare the lead role, and its
  // pools, read from the agent catalog when the dialog opens.
  let leadChoices = $state<{ value: string; label: string }[]>([]);
  let submitting = $state(false);
  let errorMsg = $state('');

  // Sync the prefill whenever the prop changes (dialog reused for different workflows).
  $effect(() => {
    workflowName = prefillWorkflow;
  });

  $effect(() => {
    if (!open) return;
    if (defaultDescription) description = defaultDescription;
  });

  // Load the lead choices from the tenant's agent catalog when the dialog opens.
  $effect(() => {
    if (!open) return;
    void (async () => {
      try {
        const client = await getSDKClient();
        const tenant = orchestratorStore.activeTenantID;
        const [agents, pools] = await Promise.all([client.agents.listAgents(tenant), client.agents.listPools(tenant)]);
        leadChoices = [
          ...agents
            .filter((a) => a.roles.includes('lead'))
            .map((a) => ({ value: `agent:${a.id}`, label: `${a.name || a.id} (${a.backend}/${a.model})` })),
          ...pools.map((p) => ({ value: `pool:${p.id}`, label: `Pool ${p.name || p.id}` })),
        ];
      } catch {
        leadChoices = [];
      }
    })();
  });

  function resetForm(): void {
    description = '';
    workflowName = prefillWorkflow;
    lead = '';
    maxIterations = 10;
    pollInterval = '2s';
    promptAppend = '';
    errorMsg = '';
  }

  function handleClose(): void {
    open = false;
    resetForm();
  }

  async function handleSubmit(): Promise<void> {
    if (!description.trim()) {
      errorMsg = 'Description is required.';
      return;
    }

    submitting = true;
    errorMsg = '';

    const req: StartJobRequest = {
      tenant_id: tenantID || orchestratorStore.activeTenantID,
      description: description.trim(),
      workflow_name: workflowName.trim(),
      lead: parseAgentRef(lead),
      project_id: '',
      max_iterations: maxIterations,
      poll_interval: pollInterval.trim() || '2s',
      prompt_append: promptAppend.trim() !== '' ? promptAppend : undefined,
    };

    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.startJob(req);
      showToast('success', `Job ${resp.job_id.slice(0, 8)} started`);
      open = false;
      resetForm();
      navigateTo('/jobs');
      void refresh('jobs');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      errorMsg = `Failed to start job: ${msg}`;
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
    aria-label="Start orchestration job"
    onkeydown={(e) => { if (e.key === 'Escape') handleClose(); }}
  >
    <div class="modal-box w-full max-w-xl">
      <h3 class="text-base font-semibold mb-4">Start Job</h3>

      <form
        onsubmit={(e) => { e.preventDefault(); void handleSubmit(); }}
        class="space-y-4"
        novalidate
      >
        <!-- Tenant ID -->
        <div class="form-control">
          <label class="label" for="sj-tenant">
            <span class="label-text">Tenant ID</span>
          </label>
          <RefPicker id="sj-tenant" kind="tenant" bind:value={tenantID} />
        </div>

        <!-- Description (required) -->
        <div class="form-control">
          <label class="label" for="sj-desc">
            <span class="label-text">Description <span class="text-error">*</span></span>
          </label>
          <textarea
            id="sj-desc"
            class="textarea textarea-bordered text-sm"
            rows="3"
            placeholder="Describe the goal of this job..."
            bind:value={description}
          ></textarea>
        </div>

        <!-- Workflow name (optional; read-only when prefilled) -->
        <div class="form-control">
          <label class="label" for="sj-workflow">
            <span class="label-text">Workflow Name</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          <RefPicker
            id="sj-workflow"
            kind="workflow"
            tenantID={orchestratorStore.activeTenantID}
            placeholder="e.g. dev-sprint-v2"
            bind:value={workflowName}
            disabled={prefillWorkflow !== ''}
          />
        </div>

        <!-- Lead Agent or pool (from the tenant's agent catalog) -->
        <div class="form-control">
          <label class="label" for="sj-lead">
            <span class="label-text">Lead Agent</span>
          </label>
          <select id="sj-lead" class="select select-bordered select-sm" bind:value={lead}>
            <option value="">Project default</option>
            {#each leadChoices as choice (choice.value)}
              <option value={choice.value}>{choice.label}</option>
            {/each}
          </select>
        </div>

        <!-- Max iterations + poll interval in a row -->
        <div class="flex gap-3">
          <div class="form-control flex-1">
            <label class="label" for="sj-iterations">
              <span class="label-text">Max Iterations</span>
            </label>
            <input
              id="sj-iterations"
              type="text"
              inputmode="numeric"
              pattern="[0-9]+"
              class="input input-bordered input-sm"
              bind:value={maxIterations}
            />
          </div>
          <div class="form-control flex-1">
            <label class="label" for="sj-poll">
              <span class="label-text">Poll Interval</span>
            </label>
            <input
              id="sj-poll"
              type="text"
              class="input input-bordered input-sm font-mono"
              placeholder="2s"
              bind:value={pollInterval}
            />
          </div>
        </div>

        <!-- Run-time prompt material for every session the job spawns -->
        <div class="form-control">
          <label class="label" for="sj-prompt-append">
            <span class="label-text">Append to every session's prompt</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          <textarea
            id="sj-prompt-append"
            class="textarea textarea-bordered text-sm font-mono"
            rows="3"
            placeholder="Context every session of this job reads after its agent's own prompt"
            bind:value={promptAppend}
            data-testid="start-job-prompt-append"
          ></textarea>
        </div>

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
            Start Job
          </button>
        </div>
      </form>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={handleClose} onkeydown={() => {}}></div>
  </dialog>
{/if}
