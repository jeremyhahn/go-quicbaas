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
    ShieldOff,
    Shield,
    Users,
  } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';

  // ---------------------------------------------------------------------------
  // Project list state
  // ---------------------------------------------------------------------------

  let projects = $state<Project[]>([]);
  let agents = $state<AgentRegistration[]>([]);
  let loading = $state(false);
  let loadError = $state('');

  /** IDs of projects whose permission matrix is currently expanded. */
  let expanded = $state<Set<string>>(new Set());

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

  // ---------------------------------------------------------------------------
  // Create project dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createName = $state('');
  let createDescription = $state('');
  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    showCreateDialog = true;
    createName = '';
    createDescription = '';
    createError = '';
  }

  async function submitCreate(): Promise<void> {
    if (!createName.trim()) {
      createError = 'Project name is required.';
      return;
    }
    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      await client.projects.createProject({
        tenant_id: orchestratorStore.activeTenantID,
        name: createName.trim(),
        description: createDescription.trim() || undefined,
      });
      showToast('success', `Project "${createName.trim()}" created`);
      showCreateDialog = false;
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      createError = `Create failed: ${msg}`;
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
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Delete failed: ${msg}`);
    } finally {
      deleting = false;
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
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Role change failed: ${msg}`);
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
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Revoke failed: ${msg}`);
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

  /** Return the current role for an agent in a project, or null if unscoped. */
  function currentRole(project: Project, agentId: string): ProjectRole | null {
    return (project.permissions?.agent_roles?.[agentId] ?? null) as ProjectRole | null;
  }

  /** True when no AgentRoles are set — project is in legacy unscoped mode. */
  function isUnscoped(project: Project): boolean {
    const roles = project.permissions?.agent_roles;
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
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">New Project</h3>
      <div class="form-control mt-4 gap-3">
        <div>
          <label class="label py-1" for="proj-name">
            <span class="label-text text-xs">Name <span class="text-error">*</span></span>
          </label>
          <input
            id="proj-name"
            type="text"
            class="input input-bordered input-sm w-full"
            placeholder="my-project"
            bind:value={createName}
          />
        </div>
        <div>
          <label class="label py-1" for="proj-desc">
            <span class="label-text text-xs">Description</span>
          </label>
          <input
            id="proj-desc"
            type="text"
            class="input input-bordered input-sm w-full"
            placeholder="Optional description"
            bind:value={createDescription}
          />
        </div>
        {#if createError}
          <p class="text-xs text-error">{createError}</p>
        {/if}
      </div>
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
    <div class="space-y-3">
      {#each projects as project (project.id)}
        {@const isOpen = expanded.has(project.id)}
        {@const agentRoles = project.permissions?.agent_roles ?? {}}
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
            </div>

            <!-- Tenant + timestamps -->
            <div class="hidden sm:flex items-center gap-4 text-xs text-base-content/40 shrink-0">
              <span class="font-mono">{project.id.slice(0, 8)}&hellip;</span>
            </div>

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
