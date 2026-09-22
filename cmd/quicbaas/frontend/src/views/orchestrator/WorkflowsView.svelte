<!--
  WorkflowsView

  Default view: workflow list, in SERVER MODE — the table hands its loader
  {limit, offset, sort, q} and the daemon answers one page with the count of
  everything that matched.

  The sortable columns are the ones the endpoint orders by: name and mode. A
  workflow carries no timestamp, and Description, Agents and Max Iter are not
  in the allow-list, so those headers do not claim to sort. The search box goes
  to the server, which matches the NAME — which is also the identifier.

  The wizard still reads a list of names, for the duplicate-name warning it
  gives before you submit. That list is one page of a hundred and the check is
  a courtesy: the server refuses a duplicate whatever this page believes.
  "+ New Workflow" opens WorkflowWizard as a full-screen modal.
  "Advanced" link opens a raw JSON paste modal.
  Each row has Run / Edit / Duplicate / Delete actions via an inline button group.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column, PageRequest } from '../../lib/dataTable.ts';
  import StartJobDialog from '../../components/orchestrator/StartJobDialog.svelte';
  import WorkflowWizard from '../../components/orchestrator/WorkflowWizard.svelte';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { WorkflowDefinition, CreateWorkflowRequest, AgentBackendInfo, AgentRegistration } from '@quicbaas/sdk';
  import { Plus, Play, Upload, Workflow, Edit, Trash2, Copy } from 'lucide-svelte';
  import { routerStore } from '../../stores/router.svelte.ts';

  // ---------------------------------------------------------------------------
  // Workflow list state
  // ---------------------------------------------------------------------------

  let workflows = $state<WorkflowDefinition[]>([]);
  let loadError = $state('');
  let highlightedName = $state('');

  // The keys are the SERVER's sort fields (docs/software-factory/api.md); a
  // column it does not order by is not marked sortable.
  const TABLE_COLUMNS: Column<WorkflowDefinition>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'description', label: 'Description', searchable: true },
    { key: 'mode', label: 'Mode', sortable: true, searchable: true },
    {
      key: 'agents',
      label: 'Agents',
      headerClass: 'text-center',
      value: (wf) => wf.agents?.length ?? 0,
    },
    {
      key: 'max_iterations',
      label: 'Max Iter',
      headerClass: 'text-center',
      value: (wf) => wf.settings?.max_iterations ?? 0,
    },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  /** How many names the wizard's duplicate check reads. */
  const NAME_PAGE_LIMIT = 100;

  /** refreshTick re-asks the table after a workflow is written or removed. */
  let refreshTick = $state(0);

  const reloadKey = $derived(`${orchestratorStore.activeTenantID}|${refreshTick}`);

  async function loadWorkflowsPage(req: PageRequest): Promise<unknown> {
    loadError = '';
    const client = await getSDKClient();
    const page = await client.orchestrator.listWorkflowsPage(orchestratorStore.activeTenantID, {
      limit: req.limit,
      offset: req.offset,
      sort: req.sort,
      q: req.q,
    });
    // The listing has answered a duplicate name before (two records, one
    // name); keeping the guard means the reconciler never sees a repeated key.
    const seen = new Set<string>();
    return {
      ...page,
      items: page.items.filter((wf) => {
        if (seen.has(wf.name)) return false;
        seen.add(wf.name);
        return true;
      }),
    };
  }

  /**
   * fetchWorkflows reads the NAMES the wizard warns on — not the table's rows,
   * which the table asks for itself. One page of a hundred: the check is a
   * courtesy and the server is what actually refuses a duplicate.
   */
  async function fetchWorkflows(): Promise<void> {
    loadError = '';
    try {
      const client = await getSDKClient();
      const page = await client.orchestrator.listWorkflowsPage(orchestratorStore.activeTenantID, {
        limit: NAME_PAGE_LIMIT,
      });
      const seen = new Set<string>();
      workflows = page.items.filter((wf) => {
        if (seen.has(wf.name)) return false;
        seen.add(wf.name);
        return true;
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to read the workflow names: ${msg}`;
    }
  }

  /** reloadWorkflows re-reads both the table's page and the name list. */
  async function reloadWorkflows(): Promise<void> {
    refreshTick += 1;
    await fetchWorkflows();
  }

  function highlightRow(name: string): void {
    highlightedName = name;
    setTimeout(() => { highlightedName = ''; }, 1000);
  }

  // Backends + agents for wizard suggestions
  let backends = $state<AgentBackendInfo[]>([]);
  let fleetAgents = $state<AgentRegistration[]>([]);

  async function fetchSupportData(): Promise<void> {
    try {
      const client = await getSDKClient();
      const [backendList, agentResp] = await Promise.all([
        client.orchestrator.listBackends(),
        client.orchestrator.listAgents({ tenant_id: orchestratorStore.activeTenantID }),
      ]);
      backends = backendList ?? [];
      fleetAgents = agentResp.agents ?? [];
    } catch {
      // Non-fatal — wizard degrades gracefully to free-text inputs.
    }
  }

  $effect(() => { void fetchWorkflows(); void fetchSupportData(); });

  // ---------------------------------------------------------------------------
  // Wizard modal state
  // ---------------------------------------------------------------------------

  let wizardOpen = $state(false);
  // When editing, prefill the wizard with these values; null = create mode.
  let wizardInitialDef = $state<WorkflowDefinition | null>(null);
  // When duplicating, name is cleared so user must rename.
  let wizardIsDuplicate = $state(false);
  let wizardIsEdit = $state(false);

  const workflowNames = $derived(workflows.map((wf) => wf.name));
  // In edit mode the current name is not a duplicate; in duplicate/create mode all names are excluded.
  const existingNamesForWizard = $derived(
    wizardIsEdit && wizardInitialDef
      ? workflowNames.filter((n) => n !== wizardInitialDef!.name)
      : workflowNames
  );

  function openCreateWizard(): void {
    wizardInitialDef = null;
    wizardIsEdit = false;
    wizardIsDuplicate = false;
    wizardOpen = true;
  }

  function openEditWizard(wf: WorkflowDefinition): void {
    wizardInitialDef = { ...wf };
    wizardIsEdit = true;
    wizardIsDuplicate = false;
    wizardOpen = true;
  }

  function uniqueCopyName(base: string, taken: string[]): string {
    const candidate = `${base}-copy`;
    if (!taken.includes(candidate)) return candidate;
    let n = 2;
    while (taken.includes(`${base}-copy-${n}`)) n++;
    return `${base}-copy-${n}`;
  }

  function openDuplicateWizard(wf: WorkflowDefinition): void {
    // Clone the workflow and suffix the name with -copy (or -copy-N) so the
    // user always gets a unique pre-filled name rather than the exact original.
    const copyName = uniqueCopyName(wf.name, workflowNames);
    wizardInitialDef = { ...wf, name: copyName };
    wizardIsEdit = false;
    wizardIsDuplicate = true;
    wizardOpen = true;
  }

  function closeWizard(): void {
    wizardOpen = false;
    wizardInitialDef = null;
    wizardIsEdit = false;
    wizardIsDuplicate = false;
  }

  // Close the wizard automatically when the user navigates away from /workflows.
  $effect(() => {
    if (routerStore.name !== 'workflows') {
      wizardOpen = false;
      wizardInitialDef = null;
      wizardIsEdit = false;
      wizardIsDuplicate = false;
    }
  });

  async function handleWizardComplete(def: WorkflowDefinition): Promise<void> {
    const client = await getSDKClient();
    if (wizardIsEdit) {
      // Update existing workflow via PUT.
      await client.orchestrator.updateWorkflow(def.name, def);
      showToast('success', `Workflow "${def.name}" updated`);
    } else {
      // Create new workflow via POST.
      const req: CreateWorkflowRequest = {
        tenant_id: orchestratorStore.activeTenantID,
        name: def.name,
        definition: def,
      };
      await client.orchestrator.createWorkflow(req);
      showToast('success', `Workflow "${def.name}" created`);
    }
    wizardOpen = false;
    await reloadWorkflows();
    highlightRow(def.name);
  }

  // ---------------------------------------------------------------------------
  // Advanced modal state (raw JSON paste)
  // ---------------------------------------------------------------------------

  let advancedOpen = $state(false);
  let advancedDefinitionText = $state('');
  let advancedName = $state('');
  let advancedError = $state('');
  let advancedCreating = $state(false);

  function openAdvancedModal(yaml: string = ''): void {
    advancedDefinitionText = yaml;
    advancedName = '';
    advancedError = '';
    advancedOpen = true;
  }

  async function handleFileUpload(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    const text = await file.text();
    advancedDefinitionText = text;
    input.value = '';
  }

  async function handleAdvancedCreate(): Promise<void> {
    if (!advancedName.trim()) {
      advancedError = 'Workflow name is required.';
      return;
    }
    if (!advancedDefinitionText.trim()) {
      advancedError = 'Paste or upload a workflow definition.';
      return;
    }

    advancedCreating = true;
    advancedError = '';

    let definition: WorkflowDefinition;
    try {
      definition = JSON.parse(advancedDefinitionText) as WorkflowDefinition;
    } catch {
      advancedError = 'Definition must be valid JSON. Convert your YAML to JSON first.';
      advancedCreating = false;
      return;
    }

    const req: CreateWorkflowRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      name: advancedName.trim(),
      definition,
    };

    try {
      const client = await getSDKClient();
      await client.orchestrator.createWorkflow(req);
      showToast('success', `Workflow "${advancedName.trim()}" created`);
      advancedOpen = false;
      advancedDefinitionText = '';
      advancedName = '';
      await reloadWorkflows();
      highlightRow(advancedName.trim());
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      advancedError = `Server error: ${msg}`;
    } finally {
      advancedCreating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete confirm modal state
  // ---------------------------------------------------------------------------

  let deleteTarget = $state<WorkflowDefinition | null>(null);
  let deleting = $state(false);

  function requestDelete(wf: WorkflowDefinition): void {
    deleteTarget = wf;
  }

  function dismissDelete(): void {
    deleteTarget = null;
  }

  async function confirmDelete(): Promise<void> {
    if (!deleteTarget) return;
    deleting = true;
    const name = deleteTarget.name;
    try {
      const client = await getSDKClient();
      await client.orchestrator.deleteWorkflow(orchestratorStore.activeTenantID, name);
      showToast('success', `Workflow "${name}" deleted`);
      deleteTarget = null;
      await reloadWorkflows();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Delete failed: ${msg}`);
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // StartJob dialog state
  // ---------------------------------------------------------------------------

  let startJobOpen = $state(false);
  let startJobWorkflow = $state('');
  let startJobDescription = $state('');
  let startJobLeadBackend = $state('');
  let startJobLeadModel = $state('');

  function openStartJob(wf: WorkflowDefinition): void {
    startJobWorkflow = wf.name;
    startJobDescription = `Run of ${wf.name}`;
    startJobLeadBackend = wf.agents?.[0]?.backend ?? '';
    startJobLeadModel = wf.agents?.[0]?.model ?? '';
    startJobOpen = true;
  }

  function modeVariant(mode: string): 'primary' | 'info' | 'ghost' {
    if (mode === 'autonomous') return 'primary';
    if (mode === 'constrained') return 'info';
    return 'ghost';
  }
</script>

<!-- StartJobDialog (shared, prefilled) -->
<StartJobDialog
  bind:open={startJobOpen}
  prefillWorkflow={startJobWorkflow}
  defaultDescription={startJobDescription}
  defaultLeadBackend={startJobLeadBackend}
  defaultLeadModel={startJobLeadModel}
/>

<!-- ============================================================
     Wizard full-screen modal
     ============================================================ -->
{#if wizardOpen}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="modal modal-open"
    role="dialog"
    aria-modal="true"
    aria-label="Workflow wizard"
    onkeydown={(e: KeyboardEvent) => { if (e.key === 'Escape') closeWizard(); }}
  >
    <div class="modal-box w-full max-w-4xl max-h-[90vh] overflow-y-auto p-0">
      <button
        class="btn btn-sm btn-circle btn-ghost absolute right-3 top-3 z-10"
        onclick={closeWizard}
        aria-label="Close wizard"
      >
        ✕
      </button>
      <WorkflowWizard
        existingNames={existingNamesForWizard}
        {backends}
        agents={fleetAgents}
        initialDef={wizardInitialDef}
        onComplete={handleWizardComplete}
        onCancel={closeWizard}
        onEditAdvanced={(yaml) => { closeWizard(); openAdvancedModal(yaml); }}
      />
    </div>
    <!-- Backdrop click dismisses -->
    <button class="modal-backdrop" onclick={closeWizard} aria-label="Close"></button>
  </div>
{/if}

<!-- ============================================================
     Advanced JSON modal
     ============================================================ -->
{#if advancedOpen}
  <div
    class="modal modal-open"
    role="dialog"
    aria-modal="true"
    aria-label="Advanced workflow editor"
  >
    <div class="modal-box w-full max-w-2xl">
      <button
        class="btn btn-sm btn-circle btn-ghost absolute right-3 top-3"
        onclick={() => { advancedOpen = false; }}
        aria-label="Close advanced modal"
      >
        ✕
      </button>
      <h3 class="text-base font-semibold mb-3">New Workflow — Raw JSON Definition</h3>
      <p class="text-xs text-base-content/50 mb-3">
        Paste a JSON workflow definition or upload a file (.json). YAML files must be
        converted to JSON before uploading.
      </p>

      <div class="form-control mb-3">
        <label class="label" for="adv-wf-name">
          <span class="label-text">Workflow Name <span class="text-error">*</span></span>
        </label>
        <input
          id="adv-wf-name"
          type="text"
          class="input input-bordered input-sm"
          placeholder="e.g. dev-sprint-v2"
          bind:value={advancedName}
        />
      </div>

      <div class="form-control mb-3">
        <label class="label" for="adv-wf-def">
          <span class="label-text">Definition JSON</span>
          <label class="btn btn-xs btn-ghost gap-1 cursor-pointer" for="adv-file-upload">
            <Upload class="h-3 w-3" />
            Upload
          </label>
        </label>
        <input
          id="adv-file-upload"
          type="file"
          accept=".json,.yaml,.yml"
          class="hidden"
          onchange={(e) => { void handleFileUpload(e); }}
        />
        <textarea
          id="adv-wf-def"
          class="textarea textarea-bordered font-mono text-xs h-48"
          placeholder='Paste workflow JSON here…'
          bind:value={advancedDefinitionText}
        ></textarea>
      </div>

      {#if advancedError}
        <div class="alert alert-error text-sm py-2 mb-3">
          <span>{advancedError}</span>
        </div>
      {/if}

      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={() => { advancedOpen = false; }}>Cancel</button>
        <button
          class="btn btn-sm btn-primary"
          onclick={() => { void handleAdvancedCreate(); }}
          disabled={advancedCreating}
        >
          {#if advancedCreating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Save Workflow
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { advancedOpen = false; }} aria-label="Close"></button>
  </div>
{/if}

<!-- ============================================================
     Delete confirm modal
     ============================================================ -->
{#if deleteTarget}
  <div
    class="modal modal-open"
    role="dialog"
    aria-modal="true"
    aria-label="Confirm delete workflow"
  >
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete workflow?</h3>
      <p class="text-sm text-base-content/70 mb-4">
        Delete <span class="font-mono font-semibold">{deleteTarget.name}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={dismissDelete} disabled={deleting}>
          Cancel
        </button>
        <button
          class="btn btn-sm btn-error"
          onclick={() => { void confirmDelete(); }}
          disabled={deleting}
        >
          {#if deleting}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Delete
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={dismissDelete} aria-label="Close"></button>
  </div>
{/if}

<!-- ============================================================
     Main view
     ============================================================ -->
<div class="p-6 space-y-6">
  <PageHeader title="Workflows" subtitle="Named orchestration workflow definitions" icon={Workflow}>
    {#snippet children()}
      <div class="flex items-center gap-2">
        <button
          class="btn btn-sm btn-primary gap-1"
          onclick={openCreateWizard}
          aria-label="Create workflow"
        >
          <Plus class="h-4 w-4" />
          New Workflow
        </button>
        <button
          class="btn btn-sm btn-ghost text-xs opacity-60 hover:opacity-100"
          onclick={() => openAdvancedModal()}
          aria-label="Advanced JSON editor"
        >
          Advanced
        </button>
      </div>
    {/snippet}
  </PageHeader>

  <!--
    The banner is about the NAME LIST the wizard's duplicate check reads, which
    is a second, smaller request. It does not hide the table: a table that
    loaded its page perfectly well must not be blanked because a courtesy
    lookup failed, and the table reports its OWN read failing.
  -->
  {#if loadError}
    <div class="alert alert-warning text-sm">
      <span>{loadError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void reloadWorkflows(); }}>Retry</button>
    </div>
  {/if}

    <DataTable
      columns={TABLE_COLUMNS}
      load={loadWorkflowsPage}
      itemsKey="items"
      {reloadKey}
      rowKey={(wf: WorkflowDefinition) => wf.name}
      urlKey="workflows"
      ariaLabel="Workflow definitions"
      searchPlaceholder="Search workflows..."
      searchLabel="Search workflows"
      noMatchText="No workflows match the current search."
    >
      {#snippet empty()}
        <EmptyState
          icon={Workflow}
          title="No workflow definitions registered."
          description="Workflows define how your agent fleet collaborates on a job."
          actionLabel="+ New Workflow"
          onaction={openCreateWizard}
        />
      {/snippet}

      {#snippet row(wf: WorkflowDefinition)}
        <tr
          class="hover:bg-base-300/40 transition-colors duration-300"
          class:bg-primary={highlightedName === wf.name}
          class:bg-opacity-10={highlightedName === wf.name}
        >
          <td class="font-mono text-xs font-semibold">{wf.name}</td>
          <td class="max-w-xs truncate text-sm">{wf.description || '—'}</td>
          <td>
            <StatusBadge text={wf.mode} variant={modeVariant(wf.mode)} />
          </td>
          <td class="text-center text-sm">{wf.agents?.length ?? 0}</td>
          <td class="text-center text-sm">{wf.settings?.max_iterations ?? '—'}</td>
          <td>
            <div class="flex items-center gap-1">
              <button
                class="btn btn-xs btn-primary gap-1"
                onclick={() => openStartJob(wf)}
                aria-label="Run workflow {wf.name}"
                title="Run"
              >
                <Play class="h-3 w-3" />
                Run
              </button>
              <button
                class="btn btn-xs btn-ghost gap-1"
                onclick={() => openEditWizard(wf)}
                aria-label="Edit workflow {wf.name}"
                title="Edit"
              >
                <Edit class="h-3 w-3" />
              </button>
              <button
                class="btn btn-xs btn-ghost gap-1"
                onclick={() => openDuplicateWizard(wf)}
                aria-label="Duplicate workflow {wf.name}"
                title="Duplicate"
              >
                <Copy class="h-3 w-3" />
              </button>
              <button
                class="btn btn-xs btn-ghost text-error gap-1"
                onclick={() => requestDelete(wf)}
                aria-label="Delete workflow {wf.name}"
                title="Delete"
              >
                <Trash2 class="h-3 w-3" />
              </button>
            </div>
          </td>
        </tr>
      {/snippet}
    </DataTable>
</div>
