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
  import type { StartJobRequest } from '@quicbaas/sdk';

  let {
    open = $bindable(false),
    prefillWorkflow = '',
    defaultDescription = '',
    defaultLeadBackend = '',
    defaultLeadModel = '',
  }: {
    open: boolean;
    prefillWorkflow?: string;
    // Optional defaults pre-filled when launching a job from a workflow row.
    defaultDescription?: string;
    defaultLeadBackend?: string;
    defaultLeadModel?: string;
  } = $props();

  // Form fields matching StartJobRequest.
  let tenantID = $state(orchestratorStore.activeTenantID);
  let description = $state('');
  let workflowName = $state(prefillWorkflow);
  let leadBackend = $state('');
  let leadModel = $state('');
  let maxIterations = $state(10);
  let pollInterval = $state('2s');

  // agentBackends holds the distinct backend names derived from the live
  // agent registry. Using the agent registry (not listBackends) guarantees
  // we only offer backends that have at least one registered, reachable
  // agent — never a preconfigured-but-offline entry like "claude (acp)".
  let agentBackends = $state<string[]>([]);
  let backendsLoaded = $state(false);
  let submitting = $state(false);
  let errorMsg = $state('');

  // Sync the prefill whenever the prop changes (dialog reused for different workflows).
  $effect(() => {
    workflowName = prefillWorkflow;
  });

  // Apply workflow-level defaults when the dialog opens. defaultDescription,
  // defaultLeadBackend, and defaultLeadModel are set by WorkflowsView when
  // launching a job from a workflow row.
  $effect(() => {
    if (!open) return;
    if (defaultDescription) description = defaultDescription;
    if (defaultLeadModel) leadModel = defaultLeadModel;
    // defaultLeadBackend is honoured below in the agent-load effect when set.
  });

  // Load the live agent registry when the dialog opens and derive the set of
  // available backends from it. The first agent's backend becomes the default.
  // Hardcoded defaults (e.g. "claude") are never used — the selection must
  // reflect what is actually registered server-side.
  $effect(() => {
    if (!open) return;
    backendsLoaded = false;
    void (async () => {
      try {
        const client = await getSDKClient();
        const resp = await client.orchestrator.listAgents({
          tenant_id: orchestratorStore.activeTenantID,
        });
        const agents = resp.agents ?? [];
        // Deduplicate backend names while preserving insertion order so the
        // first registered agent's backend is always the top entry.
        const seen = new Set<string>();
        const distinct: string[] = [];
        for (const a of agents) {
          if (a.backend && !seen.has(a.backend)) {
            seen.add(a.backend);
            distinct.push(a.backend);
          }
        }
        agentBackends = distinct;
        if (agentBackends.length > 0) {
          // Prefer the workflow's lead backend when provided and registered;
          // otherwise re-anchor to the first registered backend.
          const preferred = defaultLeadBackend || leadBackend;
          const stillRegistered = agentBackends.includes(preferred);
          leadBackend = stillRegistered ? preferred : (agentBackends[0] ?? '');
        } else {
          leadBackend = '';
        }
      } catch {
        agentBackends = [];
        leadBackend = '';
      } finally {
        backendsLoaded = true;
      }
    })();
  });

  function resetForm(): void {
    description = '';
    workflowName = prefillWorkflow;
    leadBackend = agentBackends.length > 0 ? (agentBackends[0] ?? '') : '';
    leadModel = '';
    maxIterations = 10;
    pollInterval = '2s';
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
    if (!leadModel.trim()) {
      errorMsg = 'Lead model is required.';
      return;
    }

    submitting = true;
    errorMsg = '';

    const req: StartJobRequest = {
      tenant_id: tenantID || orchestratorStore.activeTenantID,
      description: description.trim(),
      workflow_name: workflowName.trim(),
      lead_backend: leadBackend.trim(),
      lead_model: leadModel.trim(),
      project_id: '',
      max_iterations: maxIterations,
      poll_interval: pollInterval.trim() || '2s',
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
          <input
            id="sj-tenant"
            type="text"
            class="input input-bordered input-sm font-mono"
            bind:value={tenantID}
          />
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
          <input
            id="sj-workflow"
            type="text"
            class="input input-bordered input-sm"
            placeholder="e.g. dev-sprint-v2"
            bind:value={workflowName}
            readonly={prefillWorkflow !== ''}
          />
        </div>

        <!-- Lead backend (combobox populated from live agent registry) -->
        <div class="form-control">
          <label class="label" for="sj-backend">
            <span class="label-text">Lead Backend</span>
          </label>
          {#if agentBackends.length > 0}
            <select id="sj-backend" class="select select-bordered select-sm" bind:value={leadBackend}>
              {#each agentBackends as backend (backend)}
                <option value={backend}>{backend}</option>
              {/each}
            </select>
          {:else if backendsLoaded}
            <div
              class="alert alert-warning py-2 px-3 text-sm"
              role="alert"
              data-testid="no-backends-banner"
            >
              No agents registered. Register an agent first to enable job orchestration.
            </div>
          {:else}
            <div class="text-xs text-base-content/50">Loading agents…</div>
          {/if}
        </div>

        <!-- Lead model (required) -->
        <div class="form-control">
          <label class="label" for="sj-model">
            <span class="label-text">Lead Model <span class="text-error">*</span></span>
          </label>
          <input
            id="sj-model"
            type="text"
            class="input input-bordered input-sm"
            placeholder="e.g. claude-opus-4-5, llama3.2"
            bind:value={leadModel}
          />
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
            disabled={submitting || (backendsLoaded && agentBackends.length === 0)}
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
