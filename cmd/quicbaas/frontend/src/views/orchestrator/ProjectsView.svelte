<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  ProjectsView lists all projects for the active tenant and exposes the agent
  role permission matrix for each project.  Each project row can be expanded
  to show per-agent access levels.  Operators can grant or revoke (deny) roles
  with a single click.

  The create and edit dialogs edit the whole project record — workspace, git
  identity and signing, quotas, and the autonomous-development configuration.
  The edit dialog additionally edits the project's SDLC binding in place,
  through the SDLC module's own endpoints, and only where that module is
  mounted: the binding probe distinguishes "the module is not running on this
  node" from "the module is running and this project is not bound".
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type {
    Project,
    ProjectRole,
    GrantAgentRoleRequest,
    RevokeAgentRoleRequest,
  } from '@quicbaas/sdk';
  import type { AgentRegistration } from '@quicbaas/sdk';
  import {
    FolderKanban,
    ChevronDown,
    ChevronRight,
    Plus,
    RefreshCw,
    Trash2,
    Pencil,
    ShieldOff,
    Shield,
    Users,
    Bot,
  } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import FilterBar from '../../components/core/FilterBar.svelte';
  import { computeFacets, filterByFacets } from '../../lib/filters.ts';
  import { FilterState } from '../../lib/filterState.svelte.ts';
  import { projectFilters } from '../../lib/listFilters.ts';
  import ProjectFormFields from '../../components/orchestrator/ProjectFormFields.svelte';
  import SdlcBindingFields from '../../components/orchestrator/SdlcBindingFields.svelte';
  import SdlcConformancePanel from '../../components/orchestrator/SdlcConformancePanel.svelte';
  import {
    createProjectRequestFrom,
    emptyProjectForm,
    projectFormFrom,
    updateProjectRequestFrom,
    validateProjectForm,
    type ProjectFormState,
  } from '../../lib/projectForm.ts';
  import {
    bindingAvailabilityFor,
    bindingFormFrom,
    bindingFrom,
    emptyBindingForm,
    validateBindingForm,
    type BindingAvailability,
    type BindingFormState,
  } from '../../lib/sdlcBinding.ts';
  import {
    conformanceAvailabilityFor,
    type ConformanceState,
  } from '../../lib/sdlcConformance.ts';
  import type { ConformanceReport } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // Project list state
  // ---------------------------------------------------------------------------

  let projects = $state<Project[]>([]);
  let agents = $state<AgentRegistration[]>([]);
  let loading = $state(false);
  let loadError = $state('');

  /** IDs of projects whose permission matrix is currently expanded. */
  let expanded = $state<Set<string>>(new Set());

  // ---------------------------------------------------------------------------
  // Finding one project
  //
  // The list is an expandable ROW per project, which is the right shape for a
  // permission matrix and gives the operator nothing to search with. The
  // controls are the app's own — the same FilterBar and the same URL grammar
  // (`#/projects?projects_autonomous=true`) as every table — applied to the
  // collection this view already holds.
  // ---------------------------------------------------------------------------

  const PROJECT_FILTERS = projectFilters<Project>((project) =>
    project.autonomous === true ? 'true' : 'false',
  );

  const projectNarrowing = new FilterState('projects', PROJECT_FILTERS);

  let projectSearch = $state('');

  /** The searched set — what the facet counts are taken over. */
  const searchedProjects = $derived.by(() => {
    const needle = projectSearch.trim().toLowerCase();
    if (needle === '') return projects;
    return projects.filter((project) =>
      [project.name, project.id, project.description, project.git_repo_url]
        .some((field) => (field ?? '').toLowerCase().includes(needle)),
    );
  });

  const projectFacets = $derived(computeFacets(searchedProjects, PROJECT_FILTERS));

  const visibleProjects = $derived(
    filterByFacets(searchedProjects, PROJECT_FILTERS, projectNarrowing.values),
  );

  /** narrowed says the emptiness is a control's doing, not the tenant's. */
  const projectsNarrowed = $derived(
    projectSearch.trim() !== '' || Object.keys(projectNarrowing.values).length > 0,
  );

  async function fetchAll(): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const client = await getSDKClient();
      const [projResp, agentResp] = await Promise.all([
        client.projects.listProjects(orchestratorStore.activeTenantID),
        client.orchestrator.listAgents({ tenant_id: orchestratorStore.activeTenantID }),
      ]);
      projects = projResp.projects ?? [];
      agents = agentResp.agents ?? [];
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to load projects: ${msg}`;
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchAll(); });

  function toggleExpand(projectId: string): void {
    const next = new Set(expanded);
    if (next.has(projectId)) {
      next.delete(projectId);
    } else {
      next.add(projectId);
    }
    expanded = next;
  }

  function errorMessage(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  // ---------------------------------------------------------------------------
  // Create project dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createForm = $state<ProjectFormState>(emptyProjectForm());
  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    createForm = emptyProjectForm();
    createError = '';
    showCreateDialog = true;
  }

  async function submitCreate(): Promise<void> {
    const invalid = validateProjectForm(createForm, true);
    if (invalid !== '') {
      createError = invalid;
      return;
    }
    creating = true;
    createError = '';
    const name = createForm.name.trim();
    try {
      const client = await getSDKClient();
      await client.projects.createProject(
        createProjectRequestFrom(orchestratorStore.activeTenantID, createForm),
      );
      showToast('success', `Project "${name}" created`);
      showCreateDialog = false;
      await fetchAll();
    } catch (err: unknown) {
      createError = `Create failed: ${errorMessage(err)}`;
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete project
  // ---------------------------------------------------------------------------

  let deleteTarget = $state<Project | null>(null);
  let deleting = $state(false);

  async function confirmDelete(): Promise<void> {
    if (!deleteTarget || deleting) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.projects.deleteProject(orchestratorStore.activeTenantID, deleteTarget.id);
      showToast('success', `Project "${deleteTarget.name}" deleted`);
      deleteTarget = null;
      await fetchAll();
    } catch (err: unknown) {
      showToast('error', `Delete failed: ${errorMessage(err)}`);
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit project
  // ---------------------------------------------------------------------------

  let editTarget = $state<Project | null>(null);
  let editForm = $state<ProjectFormState>(emptyProjectForm());
  let editError = $state('');
  let editing = $state(false);

  // The project's SDLC binding, probed when the dialog opens.
  let bindingAvailability = $state<BindingAvailability>('absent');
  let bindingProbeError = $state('');
  let bindingForm = $state<BindingFormState>(emptyBindingForm());
  let bindingEnabled = $state(false);
  let wasBound = $state(false);
  let availableForges = $state<string[]>([]);

  // The project's ONBOARDING conformance: whether the forge is actually
  // configured for QuicBaaS to drive this repository. Read-only — the repair
  // is the separate `onboard` verb behind its own button.
  let conformanceState = $state<ConformanceState>('absent');
  let conformanceReport = $state<ConformanceReport | null>(null);
  let conformanceError = $state('');

  function openEditDialog(project: Project): void {
    editTarget = project;
    editForm = projectFormFrom(project);
    editError = '';
    bindingAvailability = 'absent';
    bindingProbeError = '';
    bindingForm = emptyBindingForm();
    bindingEnabled = false;
    wasBound = false;
    availableForges = [];
    conformanceState = 'absent';
    conformanceReport = null;
    conformanceError = '';
    void probeBinding(project.id);
  }

  /**
   * probeBinding asks the SDLC module for this project's binding. The three
   * answers are genuinely different: the module is not running on this node
   * (503 — the section is hidden entirely), it is running and this project is
   * not bound (404 — the section is shown, unbound), or it is bound (200).
   */
  async function probeBinding(projectId: string): Promise<void> {
    let client;
    try {
      client = await getSDKClient();
    } catch {
      bindingAvailability = 'absent';
      return;
    }
    const sdlc = client.sdlc;
    if (!sdlc) {
      bindingAvailability = 'absent';
      return;
    }
    try {
      const binding = await sdlc.binding(orchestratorStore.activeTenantID, projectId);
      bindingForm = bindingFormFrom(binding);
      bindingAvailability = 'bound';
      bindingEnabled = true;
      wasBound = true;
    } catch (err: unknown) {
      bindingAvailability = bindingAvailabilityFor(err);
      if (bindingAvailability === 'error') {
        bindingProbeError = `Failed to read the SDLC binding: ${errorMessage(err)}`;
      }
    }
    if (bindingAvailability === 'absent') return;
    try {
      availableForges = await sdlc.forges();
    } catch {
      // The module answered the binding probe but not the capability probe.
      // Nothing is disabled in that case: the server still refuses a forge it
      // has no adapter for, and greying out every option because we could not
      // ask would be worse than letting that refusal happen.
      availableForges = [];
    }
    if (bindingAvailability === 'bound') {
      await loadConformance(projectId);
    }
  }

  /**
   * loadConformance reads the project's onboarding conformance. Read-only on
   * purpose: an operator must be able to ask what state a repository is in
   * without changing it, which is why repair is a separate call.
   */
  async function loadConformance(projectId: string): Promise<void> {
    conformanceState = 'loading';
    conformanceError = '';
    try {
      const client = await getSDKClient();
      if (!client.sdlc) {
        conformanceState = 'absent';
        return;
      }
      conformanceReport = await client.sdlc.conformance(orchestratorStore.activeTenantID, projectId);
      conformanceState = 'ready';
    } catch (err: unknown) {
      conformanceState = conformanceAvailabilityFor(err);
      if (conformanceState === 'error') {
        conformanceError = `Failed to read onboarding conformance: ${errorMessage(err)}`;
      }
    }
  }

  /**
   * repairConformance provisions the repository and re-reads the report. It
   * is a WRITE, behind its own button, so nothing about opening this dialog
   * can change a repository's configuration.
   */
  async function repairConformance(projectId: string): Promise<void> {
    conformanceState = 'loading';
    conformanceError = '';
    try {
      const client = await getSDKClient();
      if (!client.sdlc) {
        conformanceState = 'absent';
        return;
      }
      const result = await client.sdlc.onboard(orchestratorStore.activeTenantID, projectId);
      conformanceReport = result.conformance ?? conformanceReport;
      conformanceState = 'ready';
      showToast('success', 'Repository provisioned');
    } catch (err: unknown) {
      conformanceState = 'error';
      conformanceError = `Provisioning failed: ${errorMessage(err)}`;
    }
  }

  /** bindingVisible says the SDLC module is mounted and has something to say. */
  const bindingVisible = $derived(bindingAvailability !== 'absent');

  async function submitEdit(): Promise<void> {
    const project = editTarget;
    if (!project) return;
    const invalid = validateProjectForm(editForm, false);
    if (invalid !== '') {
      editError = invalid;
      return;
    }
    if (bindingVisible && bindingEnabled) {
      const bindingInvalid = validateBindingForm(bindingForm, availableForges);
      if (bindingInvalid !== '') {
        editError = bindingInvalid;
        return;
      }
    }
    editing = true;
    editError = '';
    try {
      const client = await getSDKClient();
      await client.projects.updateProject(
        updateProjectRequestFrom(orchestratorStore.activeTenantID, project, editForm),
      );
      if (bindingVisible && client.sdlc) {
        if (bindingEnabled) {
          await client.sdlc.bind(
            orchestratorStore.activeTenantID,
            project.id,
            bindingFrom(project.id, bindingForm),
          );
        } else if (wasBound) {
          await client.sdlc.unbind(orchestratorStore.activeTenantID, project.id);
        }
      }
      showToast('success', `Project "${project.name}" updated`);
      editTarget = null;
      await fetchAll();
    } catch (err: unknown) {
      editError = `Update failed: ${errorMessage(err)}`;
    } finally {
      editing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Role matrix — grant / revoke
  // ---------------------------------------------------------------------------

  /** Track in-flight role changes per project+agent key. */
  let roleChanging = $state<Set<string>>(new Set());

  function roleKey(projectId: string, agentId: string): string {
    return `${projectId}:${agentId}`;
  }

  async function setAgentRole(project: Project, agentId: string, role: ProjectRole): Promise<void> {
    const key = roleKey(project.id, agentId);
    const next = new Set(roleChanging);
    next.add(key);
    roleChanging = next;
    try {
      const client = await getSDKClient();
      const req: GrantAgentRoleRequest = {
        tenant_id: orchestratorStore.activeTenantID,
        project_id: project.id,
        agent_id: agentId,
        role,
      };
      await client.projects.grantAgentRole(req);
      showToast('success', `Role updated to "${role}"`);
      await fetchAll();
    } catch (err: unknown) {
      showToast('error', `Role change failed: ${errorMessage(err)}`);
    } finally {
      const after = new Set(roleChanging);
      after.delete(key);
      roleChanging = after;
    }
  }

  async function revokeAgentRole(project: Project, agentId: string): Promise<void> {
    const key = roleKey(project.id, agentId);
    const next = new Set(roleChanging);
    next.add(key);
    roleChanging = next;
    try {
      const client = await getSDKClient();
      const req: RevokeAgentRoleRequest = {
        tenant_id: orchestratorStore.activeTenantID,
        project_id: project.id,
        agent_id: agentId,
      };
      await client.projects.revokeAgentRole(req);
      showToast('success', 'Agent access revoked (denied)');
      await fetchAll();
    } catch (err: unknown) {
      showToast('error', `Revoke failed: ${errorMessage(err)}`);
    } finally {
      const after = new Set(roleChanging);
      after.delete(key);
      roleChanging = after;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  const ROLES: ProjectRole[] = ['lead', 'worker', 'observer', 'denied'];

  function roleVariant(role: ProjectRole): string {
    switch (role) {
      case 'lead':     return 'badge-primary';
      case 'worker':   return 'badge-success';
      case 'observer': return 'badge-info';
      case 'denied':   return 'badge-error';
      default:         return 'badge-ghost';
    }
  }

  function agentName(agentId: string): string {
    const a = agents.find((ag) => ag.id === agentId);
    return a?.name ?? agentId.slice(0, 12);
  }

  /** True when no AgentRoles are set — project is in legacy unscoped mode. */
  function isUnscoped(project: Project): boolean {
    const roles = project.agent_roles;
    return !roles || Object.keys(roles).length === 0;
  }
</script>

<!-- Create project dialog -->
{#if showCreateDialog}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Create project"
    onkeydown={(e) => { if (e.key === 'Escape') { showCreateDialog = false; } }}
  >
    <div class="modal-box max-w-md max-h-[85vh] overflow-y-auto">
      <h3 class="font-bold text-base">New Project</h3>

      <ProjectFormFields idPrefix="proj" bind:form={createForm} nameEditable />

      {#if createError}
        <p class="text-xs text-error mt-3">{createError}</p>
      {/if}

      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { showCreateDialog = false; }}
          disabled={creating}
        >
          Cancel
        </button>
        <button
          class="btn btn-sm btn-primary"
          onclick={() => { void submitCreate(); }}
          disabled={creating}
        >
          {#if creating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Create
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { showCreateDialog = false; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Delete confirm dialog -->
{#if deleteTarget}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Confirm delete project"
    onkeydown={(e) => { if (e.key === 'Escape') { deleteTarget = null; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Delete Project?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Project <span class="font-semibold">{deleteTarget.name}</span> and all its
        permission assignments will be permanently removed.
      </p>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { deleteTarget = null; }}
          disabled={deleting}
        >
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
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { deleteTarget = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Edit project dialog -->
{#if editTarget}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Edit project"
    onkeydown={(e) => { if (e.key === 'Escape') { editTarget = null; } }}
  >
    <div class="modal-box max-w-md max-h-[85vh] overflow-y-auto">
      <h3 class="font-bold text-base">Edit Project — {editTarget.name}</h3>

      <ProjectFormFields idPrefix="edit-proj" bind:form={editForm} editing />

      {#if bindingVisible}
        <details class="collapse collapse-arrow bg-base-100 rounded-box mt-3" open={bindingEnabled}>
          <summary class="collapse-title text-xs font-semibold py-2 min-h-0">
            Issue-driven delivery (SDLC)
          </summary>
          <div class="collapse-content flex flex-col gap-3 pt-1">
            {#if bindingProbeError}
              <p class="text-xs text-error">{bindingProbeError}</p>
            {/if}

            <label class="label cursor-pointer justify-start gap-3 py-1" for="edit-proj-sdlc-enabled">
              <input
                id="edit-proj-sdlc-enabled"
                type="checkbox"
                class="toggle toggle-sm toggle-primary"
                bind:checked={bindingEnabled}
              />
              <span class="label-text text-xs">
                Bind this project to a forge repository and an SDLC
              </span>
            </label>
            {#if wasBound && !bindingEnabled}
              <p class="text-xs text-warning">
                Saving now removes this project's binding. Its issues stop being adopted.
              </p>
            {/if}

            {#if bindingEnabled}
              <SdlcBindingFields
                idPrefix="edit-proj"
                bind:binding={bindingForm}
                {availableForges}
              />
            {/if}

            {#if conformanceState !== 'absent' && editTarget}
              {@const target = editTarget}
              <SdlcConformancePanel
                report={conformanceReport}
                busy={conformanceState === 'loading'}
                error={conformanceError}
                onRefresh={() => void loadConformance(target.id)}
                onRepair={() => void repairConformance(target.id)}
              />
            {/if}
          </div>
        </details>
      {/if}

      {#if editError}
        <p class="text-xs text-error mt-3">{editError}</p>
      {/if}

      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { editTarget = null; }}
          disabled={editing}
        >
          Cancel
        </button>
        <button
          class="btn btn-sm btn-primary"
          onclick={() => { void submitEdit(); }}
          disabled={editing}
        >
          {#if editing}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Save
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { editTarget = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Main content -->
<div class="p-6 space-y-5 max-w-5xl mx-auto">

  <!-- Page header — standard pattern: icon tile + title + subtitle + actions. -->
  <PageHeader
    title="Projects"
    subtitle="Project workspaces grouping agents, sessions, and jobs"
    icon={FolderKanban}
  >
    {#snippet children()}
      {#if !loading}
        <span class="badge badge-sm badge-ghost" aria-label="{projects.length} projects">{projects.length}</span>
      {/if}
      <button
        class="btn btn-sm btn-ghost gap-1"
        onclick={() => { void fetchAll(); }}
        disabled={loading}
        aria-label="Refresh projects"
      >
        <RefreshCw class="h-3 w-3 {loading ? 'animate-spin' : ''}" />
        Refresh
      </button>
      <button
        class="btn btn-sm btn-primary gap-1"
        onclick={openCreateDialog}
        aria-label="Create project"
      >
        <Plus class="h-3 w-3" />
        New Project
      </button>
    {/snippet}
  </PageHeader>

  <!-- Error banner -->
  {#if loadError}
    <div class="alert alert-error text-sm py-2">
      <span>{loadError}</span>
    </div>
  {/if}

  <!-- Loading skeleton -->
  {#if loading && projects.length === 0}
    <div class="space-y-3">
      {#each Array(3) as _}
        <div class="skeleton h-16 w-full rounded-box"></div>
      {/each}
    </div>

  <!-- Empty state -->
  {:else if !loading && projects.length === 0}
    <EmptyState
      icon={FolderKanban}
      title="No projects yet"
      description="Create a project to start assigning agents and managing access."
    />

  <!-- Project list -->
  {:else}
    <div class="space-y-2 mb-3">
      <input
        type="search"
        class="input input-bordered input-sm w-full max-w-xs"
        placeholder="Search projects..."
        aria-label="Search projects"
        data-testid="projects-search"
        bind:value={projectSearch}
      />
      <FilterBar
        fields={PROJECT_FILTERS}
        values={projectNarrowing.values}
        facets={projectFacets}
        onchange={(next) => projectNarrowing.apply(next)}
      />
      <div class="text-xs text-base-content/50" data-testid="projects-count">
        {visibleProjects.length} of {projects.length} projects
      </div>
    </div>

    {#if visibleProjects.length === 0}
      <!-- Narrowed to nothing is not "no projects yet" — see AgentFleet. -->
      <div class="text-center py-8 text-sm text-base-content/40" data-testid="projects-no-match">
        {projectsNarrowed ? 'No projects match the current filter.' : 'No projects yet.'}
      </div>
    {/if}

    <div class="space-y-3">
      {#each visibleProjects as project (project.id)}
        {@const isOpen = expanded.has(project.id)}
        {@const agentRoles = project.agent_roles ?? {}}
        {@const scopedAgentIds = Object.keys(agentRoles)}

        <div class="card bg-base-200 rounded-box overflow-hidden">

          <!-- Project row header -->
          <div class="flex items-center gap-3 px-4 py-3">
            <!-- Expand toggle -->
            <button
              class="btn btn-ghost btn-xs btn-square"
              onclick={() => { toggleExpand(project.id); }}
              aria-label={isOpen ? 'Collapse permissions' : 'Expand permissions'}
            >
              {#if isOpen}
                <ChevronDown class="h-3.5 w-3.5" />
              {:else}
                <ChevronRight class="h-3.5 w-3.5" />
              {/if}
            </button>

            <!-- Project name + meta -->
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2">
                <span class="font-semibold text-sm truncate">{project.name}</span>
                {#if project.autonomous}
                  <span
                    class="flex items-center gap-1 badge badge-xs badge-primary"
                    title="Autonomous development is on — issues are adopted and pull requests submitted without a person"
                  >
                    <Bot class="h-2.5 w-2.5" />
                    autonomous
                  </span>
                {/if}
                {#if isUnscoped(project)}
                  <span class="badge badge-xs badge-ghost" title="No agent roles set — all agents have implicit access">unscoped</span>
                {:else}
                  <span class="flex items-center gap-1 badge badge-xs badge-outline">
                    <Users class="h-2.5 w-2.5" />
                    {scopedAgentIds.length}
                  </span>
                {/if}
              </div>
              {#if project.description}
                <div class="text-xs text-base-content/40 truncate mt-0.5">{project.description}</div>
              {/if}
              {#if project.workspace_path}
                <div
                  class="text-xs font-mono text-base-content/40 truncate mt-0.5"
                  title={project.workspace_path}
                >
                  {project.workspace_path}
                </div>
              {/if}
            </div>

            <!-- Tenant + timestamps -->
            <div class="hidden sm:flex items-center gap-4 text-xs text-base-content/40 shrink-0">
              <span class="font-mono">{project.id.slice(0, 8)}&hellip;</span>
            </div>

            <!-- Edit button -->
            <button
              class="btn btn-ghost btn-xs btn-square"
              onclick={() => { openEditDialog(project); }}
              aria-label="Edit project"
            >
              <Pencil class="h-3.5 w-3.5" />
            </button>

            <!-- Delete button -->
            <button
              class="btn btn-ghost btn-xs btn-square text-error hover:bg-error/10"
              onclick={() => { deleteTarget = project; }}
              aria-label="Delete project"
            >
              <Trash2 class="h-3.5 w-3.5" />
            </button>
          </div>

          <!-- Permission matrix (expanded) -->
          {#if isOpen}
            <div class="border-t border-base-300/50 px-4 pb-4 pt-3">
              <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3 flex items-center gap-1">
                <Shield class="h-3 w-3" />
                Agent Permissions
              </div>

              {#if isUnscoped(project)}
                <p class="text-xs text-base-content/50 mb-3">
                  This project has no explicit agent roles — all agents have implicit access (legacy
                  unscoped mode). Assigning any role below activates scoped mode and restricts
                  access to only listed agents.
                </p>
              {/if}

              <!-- Role matrix table -->
              <div class="overflow-x-auto">
                <table class="table table-xs w-full">
                  <thead>
                    <tr class="text-base-content/40">
                      <th class="w-2/5">Agent</th>
                      <th class="w-1/4">Current Role</th>
                      <th class="w-1/4">Change Role</th>
                      <th class="w-[80px]">Revoke</th>
                    </tr>
                  </thead>
                  <tbody>
                    <!-- Agents already in the project's role map -->
                    {#each scopedAgentIds as agentId (agentId)}
                      {@const role = agentRoles[agentId] as ProjectRole}
                      {@const key = roleKey(project.id, agentId)}
                      {@const busy = roleChanging.has(key)}
                      <tr>
                        <td class="font-mono text-xs truncate max-w-[12rem]" title={agentId}>
                          {agentName(agentId)}
                        </td>
                        <td>
                          <span class="badge badge-xs {roleVariant(role)}">{role}</span>
                        </td>
                        <td>
                          <select
                            class="select select-bordered select-xs w-full max-w-[8rem]"
                            value={role}
                            disabled={busy}
                            onchange={(e) => {
                              const newRole = (e.target as HTMLSelectElement).value as ProjectRole;
                              if (newRole !== role) void setAgentRole(project, agentId, newRole);
                            }}
                            aria-label="Change role for agent {agentName(agentId)}"
                          >
                            {#each ROLES as r}
                              <option value={r}>{r}</option>
                            {/each}
                          </select>
                        </td>
                        <td>
                          <button
                            class="btn btn-xs btn-ghost text-error hover:bg-error/10 gap-1"
                            onclick={() => { void revokeAgentRole(project, agentId); }}
                            disabled={busy || role === 'denied'}
                            title={role === 'denied' ? 'Already denied' : 'Revoke access (set to denied)'}
                            aria-label="Revoke access for agent {agentName(agentId)}"
                          >
                            {#if busy}
                              <span class="loading loading-spinner loading-xs"></span>
                            {:else}
                              <ShieldOff class="h-3 w-3" />
                            {/if}
                          </button>
                        </td>
                      </tr>
                    {/each}

                    <!-- Agents known to the system but not yet in this project -->
                    {#each agents.filter((a) => !scopedAgentIds.includes(a.id)) as agent (agent.id)}
                      {@const key = roleKey(project.id, agent.id)}
                      {@const busy = roleChanging.has(key)}
                      <tr class="opacity-50 hover:opacity-100 transition-opacity">
                        <td class="font-mono text-xs truncate max-w-[12rem]" title={agent.id}>
                          {agent.name || agent.id.slice(0, 12)}
                        </td>
                        <td>
                          <span class="badge badge-xs badge-ghost">unassigned</span>
                        </td>
                        <td>
                          <select
                            class="select select-bordered select-xs w-full max-w-[8rem]"
                            disabled={busy}
                            onchange={(e) => {
                              const newRole = (e.target as HTMLSelectElement).value as ProjectRole;
                              if (newRole) void setAgentRole(project, agent.id, newRole);
                            }}
                            aria-label="Assign role for agent {agent.name || agent.id}"
                          >
                            <option value="">-- assign --</option>
                            {#each ROLES as r}
                              <option value={r}>{r}</option>
                            {/each}
                          </select>
                        </td>
                        <td>
                          <!-- No revoke when not yet assigned -->
                          <span class="text-base-content/20 text-xs">—</span>
                        </td>
                      </tr>
                    {/each}

                    {#if agents.length === 0 && scopedAgentIds.length === 0}
                      <tr>
                        <td colspan="4" class="text-center text-xs text-base-content/30 py-4">
                          No agents registered for this tenant.
                        </td>
                      </tr>
                    {/if}
                  </tbody>
                </table>
              </div>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
