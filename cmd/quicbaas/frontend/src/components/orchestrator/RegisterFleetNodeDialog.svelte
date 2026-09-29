<!--
  RegisterFleetNodeDialog presents a modal form for registering a new fleet node
  with the fleet registry.
-->
<script lang="ts">
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import RefPicker from '../core/RefPicker.svelte';
  import type { RegisterFleetNodeRequest } from '@quicbaas/sdk';

  let {
    open = $bindable(false),
    onRegistered,
  }: {
    open: boolean;
    onRegistered?: () => void;
  } = $props();

  let name = $state('');
  let host = $state('');
  let backend = $state('');
  let nodeType = $state('');
  let model = $state('');
  let tenantID = $state(orchestratorStore.activeTenantID);
  let metadataJSON = $state('{}');
  let submitting = $state(false);
  let errorMsg = $state('');

  function resetForm(): void {
    name = '';
    host = '';
    backend = '';
    nodeType = '';
    model = '';
    tenantID = orchestratorStore.activeTenantID;
    metadataJSON = '{}';
    errorMsg = '';
  }

  function handleClose(): void {
    open = false;
    resetForm();
  }

  async function handleSubmit(): Promise<void> {
    if (!name.trim()) {
      errorMsg = 'Name is required.';
      return;
    }
    if (!host.trim()) {
      errorMsg = 'Host is required.';
      return;
    }

    let metadata: Record<string, string> = {};
    if (metadataJSON.trim() && metadataJSON.trim() !== '{}') {
      try {
        const parsed = JSON.parse(metadataJSON) as unknown;
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
          throw new TypeError('metadata must be a JSON object');
        }
        metadata = parsed as Record<string, string>;
      } catch {
        errorMsg = 'Metadata must be valid JSON (object).';
        return;
      }
    }

    submitting = true;
    errorMsg = '';

    const req: RegisterFleetNodeRequest = {
      tenant_id: tenantID || orchestratorStore.activeTenantID,
      name: name.trim(),
      host: host.trim(),
      backend: backend.trim(),
      node_type: nodeType.trim(),
      model: model.trim() || undefined,
      metadata: Object.keys(metadata).length > 0 ? metadata : undefined,
    };

    try {
      const client = await getSDKClient();
      await client.orchestrator.registerFleetNode(req);
      showToast('success', `Fleet node "${name.trim()}" registered`);
      open = false;
      resetForm();
      onRegistered?.();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      errorMsg = `Failed to register node: ${msg}`;
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
    aria-label="Register node"
    onkeydown={(e) => { if (e.key === 'Escape') handleClose(); }}
  >
    <div class="modal-box w-full max-w-xl">
      <h3 class="text-base font-semibold mb-4">Register node</h3>

      <form
        onsubmit={(e) => { e.preventDefault(); void handleSubmit(); }}
        class="space-y-4"
        novalidate
      >
        <!-- Name + Host in a row -->
        <div class="flex gap-3">
          <div class="form-control flex-1">
            <label class="label" for="ra-name">
              <span class="label-text">Name <span class="text-error">*</span></span>
            </label>
            <input
              id="ra-name"
              type="text"
              class="input input-bordered input-sm"
              placeholder="e.g. agent-01"
              bind:value={name}
            />
          </div>
          <div class="form-control flex-1">
            <label class="label" for="ra-host">
              <span class="label-text">Host <span class="text-error">*</span></span>
            </label>
            <input
              id="ra-host"
              type="text"
              class="input input-bordered input-sm font-mono"
              placeholder="e.g. 10.0.0.2:9090"
              bind:value={host}
            />
          </div>
        </div>

        <!-- Backend + Node type in a row -->
        <div class="flex gap-3">
          <div class="form-control flex-1">
            <label class="label" for="ra-backend">
              <span class="label-text">Backend</span>
            </label>
            <input
              id="ra-backend"
              type="text"
              class="input input-bordered input-sm"
              placeholder="e.g. claude, ollama"
              bind:value={backend}
            />
          </div>
          <div class="form-control flex-1">
            <label class="label" for="ra-type">
              <span class="label-text">Node type</span>
            </label>
            <input
              id="ra-type"
              type="text"
              class="input input-bordered input-sm"
              placeholder="e.g. worker, lead"
              bind:value={nodeType}
            />
          </div>
        </div>

        <!-- Model -->
        <div class="form-control">
          <label class="label" for="ra-model">
            <span class="label-text">Model</span>
            <span class="label-text-alt text-base-content/40">optional</span>
          </label>
          <input
            id="ra-model"
            type="text"
            class="input input-bordered input-sm"
            placeholder="e.g. claude-fable-5"
            bind:value={model}
          />
        </div>

        <!-- Tenant ID -->
        <div class="form-control">
          <label class="label" for="ra-tenant">
            <span class="label-text">Tenant ID</span>
          </label>
          <RefPicker id="ra-tenant" kind="tenant" bind:value={tenantID} />
        </div>

        <!-- Metadata JSON -->
        <div class="form-control">
          <label class="label" for="ra-meta">
            <span class="label-text">Metadata</span>
            <span class="label-text-alt text-base-content/40">JSON object</span>
          </label>
          <textarea
            id="ra-meta"
            class="textarea textarea-bordered font-mono text-xs"
            rows="3"
            placeholder="JSON object, e.g. env: prod"
            bind:value={metadataJSON}
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
            Register
          </button>
        </div>
      </form>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={handleClose} onkeydown={() => {}}></div>
  </dialog>
{/if}
