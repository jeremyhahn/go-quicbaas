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
  MainApp is the top-level application shell: header, collapsible sidebar,
  and content area. It owns the auto-refresh polling loop for orchestration
  data and delegates all view rendering to purpose-built child components.
-->
<script lang="ts">
  import {
    navStore,
    navigate,
    toggleSidebar,
    type ViewID,
  } from '../stores/navigation.svelte.ts';
  import { themeStore, toggleTheme } from '../stores/theme.svelte.ts';
  import { connectionStore } from '../stores/connection.svelte.ts';
  import {
    orchestratorStore,
    setJobs,
    setAgents,
    setSessions,
    setTasks,
    setHealth,
    setJobsLoading,
    setAgentsLoading,
    setSessionsLoading,
    setTasksLoading,
    setActiveTenant,
    type JobStatus,
    type AgentRegistration,
    type SessionInfo,
    type Task,
    type HealthCheckReport,
  } from '../stores/orchestrator.svelte.ts';
  import { showToast } from '../stores/toast.svelte.ts';

  // Views
  import OrchestratorDashboard from './orchestrator/OrchestratorDashboard.svelte';
  import JobsView from './orchestrator/JobsView.svelte';
  import JobDetailView from './orchestrator/JobDetailView.svelte';
  import AgentsView from './orchestrator/AgentsView.svelte';
  import SessionsView from './orchestrator/SessionsView.svelte';
  import TasksView from './orchestrator/TasksView.svelte';
  import EventStreamView from './orchestrator/EventStreamView.svelte';
  import CostView from './orchestrator/CostView.svelte';
  import ComingSoon from './ComingSoon.svelte';

  // Components
  import StatusBadge from '../components/core/StatusBadge.svelte';

  // Icons
  import {
    LayoutDashboard,
    Workflow,
    Bot,
    Terminal,
    ClipboardList,
    Radio,
    DollarSign,
    Database,
    Sparkles,
    TrendingUp,
    Package,
    Brain,
    Cpu,
    MessageSquare,
    Clock,
    Shield,
    Key,
    EyeOff,
    Lock,
    Server,
    Network,
    HardDrive,
    Settings,
    Sun,
    Moon,
    Menu,
    ChevronLeft,
  } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Sidebar navigation definition
  // ---------------------------------------------------------------------------

  // Lucide icons are SvelteComponentTyped classes, which are not directly
  // assignable to Svelte 5's Component type. Use a permissive constructible
  // type that satisfies both old and new Svelte component patterns.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  type NavIcon = new (...args: any[]) => any;

  interface NavItem {
    id: ViewID;
    label: string;
    icon: NavIcon;
    implemented: boolean;
  }

  interface NavSection {
    title: string;
    items: NavItem[];
  }

  const navSections: NavSection[] = [
    {
      title: 'OVERVIEW',
      items: [
        { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard, implemented: true },
      ],
    },
    {
      title: 'ORCHESTRATION',
      items: [
        { id: 'jobs',     label: 'Jobs',        icon: Workflow,       implemented: true },
        { id: 'agents',   label: 'Agent Fleet', icon: Bot,            implemented: true },
        { id: 'sessions', label: 'Sessions',    icon: Terminal,       implemented: true },
        { id: 'tasks',    label: 'Tasks',       icon: ClipboardList,  implemented: true },
        { id: 'events',   label: 'Live Events', icon: Radio,          implemented: true },
        { id: 'costs',    label: 'Cost Tracking',icon: DollarSign,    implemented: true },
      ],
    },
    {
      title: 'DATA',
      items: [
        { id: 'kv',         label: 'KV Store',    icon: Database,   implemented: false },
        { id: 'vectors',    label: 'Vectors',     icon: Sparkles,   implemented: false },
        { id: 'timeseries', label: 'Time-Series', icon: TrendingUp, implemented: false },
        { id: 'objects',    label: 'Objects',     icon: Package,    implemented: false },
      ],
    },
    {
      title: 'AI/ML',
      items: [
        { id: 'rag',        label: 'RAG',        icon: Brain, implemented: false },
        { id: 'embeddings', label: 'Embeddings', icon: Cpu,   implemented: false },
      ],
    },
    {
      title: 'ASYNC',
      items: [
        { id: 'messaging',  label: 'Messaging',  icon: MessageSquare, implemented: false },
        { id: 'scheduler',  label: 'Scheduler',  icon: Clock,         implemented: false },
      ],
    },
    {
      title: 'SECURITY',
      items: [
        { id: 'iam',     label: 'IAM',     icon: Shield, implemented: false },
        { id: 'kms',     label: 'KMS',     icon: Key,    implemented: false },
        { id: 'secrets', label: 'Secrets', icon: EyeOff, implemented: false },
        { id: 'seal',    label: 'Seal',    icon: Lock,   implemented: false },
      ],
    },
    {
      title: 'CLUSTER',
      items: [
        { id: 'cluster', label: 'Dashboard', icon: Server,    implemented: false },
        { id: 'nodes',   label: 'Nodes',     icon: Network,   implemented: false },
        { id: 'backup',  label: 'Backup',    icon: HardDrive, implemented: false },
      ],
    },
    {
      title: 'SETTINGS',
      items: [
        { id: 'connection', label: 'Connection', icon: Settings, implemented: false },
      ],
    },
  ];

  // ---------------------------------------------------------------------------
  // Data fetching
  // ---------------------------------------------------------------------------

  async function fetchJobs(): Promise<void> {
    setJobsLoading(true);
    try {
      const resp = await fetch(`/v1/tenants/${orchestratorStore.activeTenantID}/jobs`);
      if (!resp.ok) return;
      const data = await resp.json() as { jobs?: JobStatus[] };
      setJobs(data.jobs ?? []);
    } catch {
      // Silently ignore fetch errors during polling.
    } finally {
      setJobsLoading(false);
    }
  }

  async function fetchAgents(): Promise<void> {
    setAgentsLoading(true);
    try {
      const resp = await fetch(`/v1/tenants/${orchestratorStore.activeTenantID}/agents`);
      if (!resp.ok) return;
      const data = await resp.json() as { agents?: AgentRegistration[] };
      setAgents(data.agents ?? []);
    } catch {
      // Silently ignore.
    } finally {
      setAgentsLoading(false);
    }
  }

  async function fetchSessions(): Promise<void> {
    setSessionsLoading(true);
    try {
      const resp = await fetch(`/v1/tenants/${orchestratorStore.activeTenantID}/sessions`);
      if (!resp.ok) return;
      const data = await resp.json() as { sessions?: SessionInfo[] };
      setSessions(data.sessions ?? []);
    } catch {
      // Silently ignore.
    } finally {
      setSessionsLoading(false);
    }
  }

  async function fetchTasks(): Promise<void> {
    setTasksLoading(true);
    try {
      const resp = await fetch(`/v1/tenants/${orchestratorStore.activeTenantID}/tasks`);
      if (!resp.ok) return;
      const data = await resp.json() as { tasks?: Task[] };
      setTasks(data.tasks ?? []);
    } catch {
      // Silently ignore.
    } finally {
      setTasksLoading(false);
    }
  }

  async function fetchHealth(): Promise<void> {
    try {
      const resp = await fetch('/health');
      if (!resp.ok) return;
      const data = await resp.json() as HealthCheckReport;
      setHealth(data);
    } catch {
      // Silently ignore.
    }
  }

  async function refreshAll(): Promise<void> {
    await Promise.allSettled([fetchJobs(), fetchAgents(), fetchSessions(), fetchTasks()]);
  }

  // ---------------------------------------------------------------------------
  // Auto-refresh with setInterval (Svelte 5 compatible)
  // ---------------------------------------------------------------------------

  $effect(() => {
    void refreshAll();

    const quickInterval = setInterval(() => { void refreshAll(); }, 5000);
    const healthInterval = setInterval(() => { void fetchHealth(); }, 30000);

    void fetchHealth();

    return () => {
      clearInterval(quickInterval);
      clearInterval(healthInterval);
    };
  });

  // ---------------------------------------------------------------------------
  // Derived: current view label for breadcrumb
  // ---------------------------------------------------------------------------

  const activeViewLabel = $derived.by((): string => {
    for (const section of navSections) {
      const item = section.items.find((i) => i.id === navStore.activeView);
      if (item) return item.label;
    }
    return '';
  });

  // ---------------------------------------------------------------------------
  // Whether we're showing the job detail sub-view
  // ---------------------------------------------------------------------------

  const showJobDetail = $derived(
    navStore.activeView === 'jobs' && orchestratorStore.selectedJob !== null
  );
</script>

<div class="flex h-screen flex-col overflow-hidden">

  <!-- =========================================================================
       HEADER
       ========================================================================= -->
  <header class="bg-base-200/80 border-base-300/50 z-20 border-b backdrop-blur-sm">
    <div class="flex h-12 items-center px-3 gap-3">

      <!-- Sidebar toggle -->
      <button
        class="btn btn-ghost btn-square btn-sm"
        onclick={toggleSidebar}
        aria-label="Toggle sidebar"
      >
        {#if navStore.sidebarCollapsed}
          <Menu class="h-5 w-5" />
        {:else}
          <ChevronLeft class="h-5 w-5" />
        {/if}
      </button>

      <!-- Brand -->
      <span class="gradient-text text-lg font-bold tracking-tight select-none">QuicBaaS</span>

      <!-- Connection badge -->
      <StatusBadge
        text={connectionStore.connected ? 'Connected' : 'Disconnected'}
        variant={connectionStore.connected ? 'success' : 'error'}
        pulse={!connectionStore.connected}
      />

      <!-- Breadcrumb -->
      <div class="flex flex-1 items-center justify-center">
        <div class="breadcrumbs text-sm hidden sm:block">
          <ul>
            <li class="text-base-content/40">QuicBaaS</li>
            {#if activeViewLabel}
              <li class="font-medium">{activeViewLabel}</li>
            {/if}
            {#if showJobDetail && orchestratorStore.selectedJob}
              <li class="text-base-content/60">
                {orchestratorStore.selectedJob.description || orchestratorStore.selectedJob.job_id.slice(0, 8)}
              </li>
            {/if}
          </ul>
        </div>
      </div>

      <!-- Right controls -->
      <div class="flex items-center gap-1">
        <!-- Tenant selector -->
        <select
          class="select select-bordered select-xs font-mono hidden md:block"
          value={orchestratorStore.activeTenantID}
          onchange={(e) => {
            const val = (e.target as HTMLSelectElement).value;
            setActiveTenant(val);
            void refreshAll();
          }}
        >
          <option value="default">default</option>
        </select>

        <!-- Theme toggle -->
        <button
          class="btn btn-ghost btn-square btn-sm"
          onclick={toggleTheme}
          aria-label="Toggle theme"
        >
          {#if themeStore.theme === 'dark'}
            <Sun class="h-4 w-4" />
          {:else}
            <Moon class="h-4 w-4" />
          {/if}
        </button>
      </div>

    </div>
  </header>

  <!-- =========================================================================
       BODY: sidebar + content
       ========================================================================= -->
  <div class="flex flex-1 overflow-hidden">

    <!-- -----------------------------------------------------------------------
         SIDEBAR
         ----------------------------------------------------------------------- -->
    <aside
      class="bg-base-200 border-base-300/50 flex flex-col border-r transition-all duration-200 overflow-hidden shrink-0"
      style:width={navStore.sidebarCollapsed ? '56px' : '220px'}
    >
      <nav class="flex-1 overflow-y-auto overflow-x-hidden py-2">
        {#each navSections as section}
          <!-- Section header — hidden when collapsed -->
          {#if !navStore.sidebarCollapsed}
            <div class="px-3 pt-3 pb-1">
              <span class="text-[10px] font-semibold tracking-widest text-base-content/30">
                {section.title}
              </span>
            </div>
          {:else}
            <div class="my-1 border-t border-base-300/30"></div>
          {/if}

          {#each section.items as item}
            {@const NavIcon = item.icon}
            <button
              class="flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors rounded-none hover:bg-base-300/50"
              class:bg-primary={navStore.activeView === item.id}
              class:text-primary-content={navStore.activeView === item.id}
              class:text-base-content={navStore.activeView !== item.id}
              class:opacity-50={!item.implemented}
              onclick={() => { if (item.implemented) navigate(item.id as ViewID); }}
              title={navStore.sidebarCollapsed ? item.label : undefined}
            >
              <NavIcon
                class="h-4 w-4 shrink-0"
                strokeWidth={navStore.activeView === item.id ? 2.5 : 1.5}
              />
              {#if !navStore.sidebarCollapsed}
                <span class="truncate">{item.label}</span>
                {#if !item.implemented}
                  <span class="badge badge-xs badge-ghost ml-auto">soon</span>
                {/if}
              {/if}
            </button>
          {/each}
        {/each}
      </nav>
    </aside>

    <!-- -----------------------------------------------------------------------
         CONTENT AREA
         ----------------------------------------------------------------------- -->
    <main class="content-bg flex-1 overflow-y-auto">

      {#if showJobDetail}
        <JobDetailView />

      {:else if navStore.activeView === 'dashboard'}
        <OrchestratorDashboard />

      {:else if navStore.activeView === 'jobs'}
        <JobsView />

      {:else if navStore.activeView === 'agents'}
        <AgentsView />

      {:else if navStore.activeView === 'sessions'}
        <SessionsView />

      {:else if navStore.activeView === 'tasks'}
        <TasksView />

      {:else if navStore.activeView === 'events'}
        <EventStreamView />

      {:else if navStore.activeView === 'costs'}
        <CostView />

      {:else}
        <ComingSoon title={activeViewLabel || 'Coming Soon'} />
      {/if}

    </main>
  </div>

  <!-- =========================================================================
       STATUS BAR
       ========================================================================= -->
  <footer class="bg-base-300/80 border-base-300/50 flex items-center justify-between border-t px-4 py-1 text-xs backdrop-blur-sm">
    <div class="flex items-center gap-3 text-base-content/40">
      <span>{connectionStore.address || 'localhost:9443'}</span>
      {#if connectionStore.transport}
        <span class="badge badge-xs badge-ghost">{connectionStore.transport}</span>
      {/if}
    </div>
    <div class="flex items-center gap-3">
      {#if orchestratorStore.health}
        <StatusBadge
          text={orchestratorStore.health.status}
          variant={orchestratorStore.health.status === 'ok' || orchestratorStore.health.status === 'healthy' ? 'success' : 'warning'}
        />
      {/if}
      <span class="text-base-content/25">QuicBaaS v0.1.0</span>
    </div>
  </footer>

</div>
