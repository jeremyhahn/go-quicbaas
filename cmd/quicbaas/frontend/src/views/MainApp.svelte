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
    setActiveTenant,
    selectJob,
    refresh,
    startOrchestratorPolling,
    loadJobs,
  } from '../stores/orchestrator.svelte.ts';
  import { untrack } from 'svelte';
  import { showToast } from '../stores/toast.svelte.ts';

  // Views
  import SystemDashboard from './SystemDashboard.svelte';
  import OrchestratorDashboard from './orchestrator/OrchestratorDashboard.svelte';
  import JobsView from './orchestrator/JobsView.svelte';
  import JobDetailView from './orchestrator/JobDetailView.svelte';
  import AgentDetailView from './orchestrator/AgentDetailView.svelte';
  import TasksView from './orchestrator/TasksView.svelte';
  import WorkflowsView from './orchestrator/WorkflowsView.svelte';
  import EventStreamView from './orchestrator/EventStreamView.svelte';
  import CostView from './orchestrator/CostView.svelte';
  import InferenceView from './orchestrator/InferenceView.svelte';
  import ProjectsView from './orchestrator/ProjectsView.svelte';
  import ComingSoon from './ComingSoon.svelte';
  import LoginView from './orchestrator/LoginView.svelte';
  import RegisterView from './orchestrator/RegisterView.svelte';

  // DATA views
  import KVStoreView from './data/KVStoreView.svelte';
  import VectorsView from './data/VectorsView.svelte';
  import TimeSeriesView from './data/TimeSeriesView.svelte';
  import ObjectsView from './data/ObjectsView.svelte';

  // AI/ML views
  import RAGView from './ai/RAGView.svelte';
  import EmbeddingsView from './ai/EmbeddingsView.svelte';

  // ASYNC views
  import MessagingView from './async/MessagingView.svelte';
  import SchedulerView from './async/SchedulerView.svelte';

  // SECURITY views
  import IAMView from './security/IAMView.svelte';
  import InviteActivateView from './security/InviteActivateView.svelte';
  import KMSView from './security/KMSView.svelte';
  import SecretsView from './security/SecretsView.svelte';
  import BarrierView from './security/BarrierView.svelte';
  import TenantBarrierView from './security/TenantBarrierView.svelte';
  import CeremonyView from './security/CeremonyView.svelte';
  import BootstrapView from './security/BootstrapView.svelte';
  import SSHKeysView from './security/SSHKeysView.svelte';

  // CLUSTER views
  import ClusterDashboard from './cluster/ClusterDashboard.svelte';

  // New session management views
  import SessionList from '../components/orchestrator/SessionList.svelte';
  import SessionDetail from '../components/orchestrator/SessionDetail.svelte';
  import AgentFleet from '../components/orchestrator/AgentFleet.svelte';
  import {
    routerStore,
    routeLogin,
    routeJobDetail,
    routeAgentDetail,
  } from '../stores/router.svelte.ts';
  import { sessionStore, refreshSessions } from '../stores/sessionStore.svelte.ts';
  import {
    authStore,
    validateSession,
    setUnauthenticated,
    setAuthStatus,
    readAuthToken,
  } from '../stores/auth.svelte.ts';
  import { agentStore, refreshAgents } from '../stores/agentStore.svelte.ts';
  import { isWailsRuntime } from '@quicbaas/sdk';
  import { getSDKClient } from '../stores/sdkClient.ts';

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
    LockKeyhole,
    Server,
    Network,
    HardDrive,
    Settings,
    Sun,
    Moon,
    Menu,
    ChevronLeft,
    KeyRound,
    FolderKanban,
    Users,
    Compass,
    LogIn,
    LogOut,
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
        // Renamed from "Dashboard" so the sidebar exposes three distinct
        // labels — OVERVIEW > Overview, ORCHESTRATION > Orchestration,
        // CLUSTER > Cluster — and a sighted user can disambiguate the
        // three dashboard entries at a glance.
        { id: 'dashboard', label: 'Overview', icon: LayoutDashboard, implemented: true },
      ],
    },
    {
      title: 'ORCHESTRATION',
      items: [
        { id: 'orchestration', label: 'Orchestration', icon: Workflow,      implemented: true },
        { id: 'jobs',          label: 'Jobs',         icon: Cpu,           implemented: true },
        { id: 'agents',        label: 'Agent Fleet',  icon: Bot,           implemented: true },
        { id: 'sessions',      label: 'Sessions',     icon: Terminal,      implemented: true },
        { id: 'tasks',         label: 'Tasks',        icon: ClipboardList, implemented: true },
        { id: 'workflows',     label: 'Workflows',    icon: Workflow,      implemented: true },
        { id: 'events',        label: 'Live Events',  icon: Radio,         implemented: true },
        { id: 'costs',         label: 'Cost Tracking',icon: DollarSign,    implemented: true },
        { id: 'inference',     label: 'Inference',    icon: Sparkles,      implemented: true },
        { id: 'projects',      label: 'Projects',     icon: FolderKanban,  implemented: true },
      ],
    },
    {
      title: 'DATA',
      items: [
        { id: 'kv',         label: 'KV Store',    icon: Database,   implemented: true },
        { id: 'vectors',    label: 'Vectors',     icon: Sparkles,   implemented: true },
        { id: 'timeseries', label: 'Time-Series', icon: TrendingUp, implemented: true },
        { id: 'objects',    label: 'Objects',     icon: Package,    implemented: true },
      ],
    },
    {
      title: 'AI/ML',
      items: [
        { id: 'rag',        label: 'RAG',        icon: Brain, implemented: true },
        { id: 'embeddings', label: 'Embeddings', icon: Cpu,   implemented: true },
      ],
    },
    {
      title: 'ASYNC',
      items: [
        { id: 'messaging',  label: 'Messaging',  icon: MessageSquare, implemented: true },
        { id: 'scheduler',  label: 'Scheduler',  icon: Clock,         implemented: true },
      ],
    },
    {
      title: 'SECURITY',
      items: [
        { id: 'iam',       label: 'IAM',       icon: Shield,   implemented: true },
        { id: 'kms',       label: 'KMS',       icon: Key,      implemented: true },
        { id: 'secrets',   label: 'Secrets',   icon: EyeOff,   implemented: true },
        { id: 'barrier',   label: 'Barrier',   icon: Lock,     implemented: true },
        { id: 'tenant-barrier', label: 'Tenant Barrier', icon: LockKeyhole, implemented: true },
        { id: 'ceremony',  label: 'Ceremony',  icon: Users,    implemented: true },
        { id: 'bootstrap', label: 'Bootstrap', icon: Compass,  implemented: true },
        { id: 'ssh-keys',  label: 'SSH Keys',  icon: KeyRound, implemented: true },
      ],
    },
    {
      title: 'CLUSTER',
      items: [
        { id: 'cluster', label: 'Cluster',   icon: Server,    implemented: true },
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
  // Orchestrator polling — single poller for all orchestrator resources.
  // startOrchestratorPolling() fires every 30 s with stale-while-revalidate
  // (15 s freshness window) and pauses when the tab is hidden.
  // ---------------------------------------------------------------------------

  $effect(() => {
    const stopPolling = startOrchestratorPolling();
    // The orchestrator poller covers orchestratorStore.{jobs,agents,sessions,tasks}
    // but the standalone agentStore and sessionStore (used by AgentFleet, SpawnDialog,
    // SessionList, etc.) need their own refresh ticks.
    void refreshAgents();
    void refreshSessions();
    const auxInterval = setInterval(() => {
      if (typeof document !== 'undefined' && document.hidden) return;
      void refreshAgents();
      void refreshSessions();
    }, 30_000);
    return () => {
      clearInterval(auxInterval);
      stopPolling();
    };
  });

  // ---------------------------------------------------------------------------
  // Active-tenant switcher options. Fetched from the public tenants endpoint
  // (the same one LoginView uses) so the dropdown reflects every tenant the
  // server knows about, not just the bootstrap "system". Falls back to a
  // single "system" entry if the fetch fails so the control still works.
  // 'system' is qrdb's canonical control-plane tenant; the legacy "default"
  // tenant was permanently retired by qrdb's tenant-isolation refactor.
  // ---------------------------------------------------------------------------
  let tenantOptions = $state<Array<{ id: string; name: string }>>([{ id: 'system', name: 'system' }]);

  $effect(() => {
    void (async () => {
      try {
        const client = await getSDKClient();
        const list = await client.auth.listPublicTenants();
        if (Array.isArray(list) && list.length > 0) {
          tenantOptions = list.map((t) => ({
            id: t.id,
            name: t.display_name || t.name || t.id,
          }));
        }
      } catch {
        // Keep the default fallback already in tenantOptions.
      }
    })();
  });

  // ---------------------------------------------------------------------------
  // Authentication state + route guard.
  //
  // Three-valued model:
  //   - 'pending'        : initial mount; we haven't yet checked the server
  //                        whether the token is valid. Render a splash for
  //                        protected routes so we never flash protected
  //                        content before the check resolves.
  //   - 'authenticated'  : server confirmed the token is valid.
  //   - 'unauthenticated': no token, or server rejected it. Protected routes
  //                        redirect to /login.
  //
  // The header login/logout control reads this same state: 'authenticated'
  // shows Sign out; 'unauthenticated' shows Sign in; 'pending' shows neither.
  // Cross-tab logout flips this via the localStorage 'storage' event.
  // ---------------------------------------------------------------------------
  // authStatus is owned by the shared auth store (stores/auth.svelte.ts) so
  // the login-completion paths (passkey, PIV, self-register) can flip it
  // in-tab the instant the server returns tokens. Reading it through the store
  // getter keeps this template reactive to those transitions.
  const authStatus = $derived(authStore.status);

  // Routes that do NOT require authentication.
  const PUBLIC_ROUTES: ReadonlySet<string> = new Set([
    'login',
    'register',
    'invite-activate',
  ]);

  $effect(() => {
    if (isWailsRuntime()) {
      // Wails desktop mode runs embedded — auth is OS-level, always trusted.
      setAuthStatus('authenticated');
      return;
    }
    void validateSession();
    if (typeof window === 'undefined') return;
    // Cross-tab sync: a logout in another tab clears the token; we re-check
    // on every storage event so this tab also flips to unauthenticated.
    const onStorage = (e: StorageEvent): void => {
      if (e.key === 'quicbaas_token') void validateSession();
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  });

  // Route guard: when an unauthenticated user lands on a protected route
  // (whether via direct URL, hash change, or programmatic navigation), bounce
  // to /login. The check waits for authStatus to leave 'pending' so we don't
  // redirect during the initial validate round-trip.
  $effect(() => {
    if (authStatus !== 'unauthenticated') return;
    if (PUBLIC_ROUTES.has(routerStore.name)) return;
    routeLogin();
  });

  async function handleLogout(): Promise<void> {
    if (typeof window === 'undefined') return;
    const token = readAuthToken();
    try {
      const client = await getSDKClient();
      await client.auth.logout(token);
    } catch {
      // Server-side revoke errors shouldn't block the UI from clearing local
      // state — see SDK doc-comment on AuthClient.logout for rationale.
    }
    setUnauthenticated();
    showToast('success', 'Signed out');
    routeLogin();
  }

  function handleLogin(): void {
    routeLogin();
  }

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

  // ---------------------------------------------------------------------------
  // New session management: hash router + SDK stores lifecycle
  // ---------------------------------------------------------------------------

  // (The token validation + route guard are handled by the AuthStatus
  // effect block above; no duplicate validation here.)

  // Active session for the session-detail route (looked up from store).
  // `$derived.by(fn)` evaluates the function reactively and stores the
  // RESULT — `$derived(fn)` would store the function reference itself,
  // which forces every read site to invoke it and creates a fresh function
  // identity on each parent render (a major source of cascading effect
  // re-runs in this app).
  const activeRouteSession = $derived.by(() => {
    if (routerStore.name !== 'session-detail') return null;
    const id = routerStore.params['id'] ?? '';
    return sessionStore.sessions.find((s) => s.session_id === id) ?? null;
  });

  // When the job-detail route is active, sync the selectedJob in the store.
  // On a fresh page load (or browser refresh) at #/jobs/:id, orchestratorStore.jobs
  // may not be populated yet because the orchestrator poller fetches asynchronously.
  // The effect re-fires once the jobs list arrives, so the find() resolves on the
  // second tick. Until then, render guards in JobDetailView show "No job selected"
  // — kick off an immediate refresh so the wait is bounded by network latency
  // rather than the 30s poll cadence.
  $effect(() => {
    if (routerStore.name === 'job-detail') {
      const id = routerStore.params['id'] ?? '';
      const found = orchestratorStore.jobs.find((j) => j.job_id === id) ?? null;
      if (found !== null) {
        selectJob(found);
      } else {
        // Trigger an out-of-band refresh; the effect re-runs when jobs updates.
        untrack(() => { void loadJobs(); });
      }
    }
  });


</script>

<!-- Invite activation is a standalone public page — render it without the app shell. -->
{#if routerStore.name === 'invite-activate'}
  <InviteActivateView token={routerStore.params['token'] ?? ''} />
{:else if authStatus === 'pending' && !PUBLIC_ROUTES.has(routerStore.name)}
  <!-- Initial validate is in flight; never flash protected content during it. -->
  <div class="flex h-screen items-center justify-center bg-base-100 text-base-content">
    <div class="flex flex-col items-center gap-3">
      <span class="loading loading-spinner loading-md"></span>
      <span class="text-sm text-base-content/60">Checking session…</span>
    </div>
  </div>
{:else}

<div class="flex h-screen flex-col overflow-hidden">

  <!-- =========================================================================
       HEADER
       ========================================================================= -->
  <header class="bg-base-200/80 border-base-300/50 sticky top-0 z-30 border-b backdrop-blur-sm">
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
            {#if routerStore.name === 'dashboard'}
              <li class="text-base-content/40">Overview</li>
              <li class="font-medium">Dashboard</li>
            {:else if routerStore.name === 'orchestration'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Dashboard</li>
            {:else if routerStore.name === 'jobs'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Jobs</li>
            {:else if routerStore.name === 'job-detail'}
              <li class="text-base-content/40">Orchestration</li>
              <li><a href="#/jobs" class="text-base-content/60">Jobs</a></li>
              <li class="font-medium font-mono text-xs">{(routerStore.params['id'] ?? '').slice(0, 12)}…</li>
            {:else if routerStore.name === 'fleet'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Agent Fleet</li>
            {:else if routerStore.name === 'agent-detail'}
              <li class="text-base-content/40">Orchestration</li>
              <li><a href="#/fleet" class="text-base-content/60">Fleet</a></li>
              <li class="font-medium font-mono text-xs">{(routerStore.params['id'] ?? '').slice(0, 12)}…</li>
            {:else if routerStore.name === 'sessions'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Sessions</li>
            {:else if routerStore.name === 'session-detail'}
              <li class="text-base-content/40">Orchestration</li>
              <li><a href="#/sessions" class="text-base-content/60">Sessions</a></li>
              <li class="font-medium font-mono text-xs">{(routerStore.params['id'] ?? '').slice(0, 14)}…</li>
            {:else if routerStore.name === 'tasks'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Tasks</li>
            {:else if routerStore.name === 'workflows'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Workflows</li>
            {:else if routerStore.name === 'events'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Live Events</li>
            {:else if routerStore.name === 'cost'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Cost Tracking</li>
            {:else if routerStore.name === 'inference'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Inference</li>
            {:else if routerStore.name === 'kv'}
              <li class="text-base-content/40">Data</li>
              <li class="font-medium">KV Store</li>
            {:else if routerStore.name === 'vectors'}
              <li class="text-base-content/40">Data</li>
              <li class="font-medium">Vectors</li>
            {:else if routerStore.name === 'timeseries'}
              <li class="text-base-content/40">Data</li>
              <li class="font-medium">Time-Series</li>
            {:else if routerStore.name === 'objects'}
              <li class="text-base-content/40">Data</li>
              <li class="font-medium">Objects</li>
            {:else if routerStore.name === 'rag'}
              <li class="text-base-content/40">AI/ML</li>
              <li class="font-medium">RAG</li>
            {:else if routerStore.name === 'embeddings'}
              <li class="text-base-content/40">AI/ML</li>
              <li class="font-medium">Embeddings</li>
            {:else if routerStore.name === 'messaging'}
              <li class="text-base-content/40">Async</li>
              <li class="font-medium">Messaging</li>
            {:else if routerStore.name === 'scheduler'}
              <li class="text-base-content/40">Async</li>
              <li class="font-medium">Scheduler</li>
            {:else if routerStore.name === 'iam'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">IAM</li>
            {:else if routerStore.name === 'kms'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">KMS</li>
            {:else if routerStore.name === 'secrets'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">Secrets</li>
            {:else if routerStore.name === 'barrier'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">Barrier</li>
            {:else if routerStore.name === 'tenant-barrier'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">Tenant Barrier</li>
            {:else if routerStore.name === 'ceremony'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">Ceremony</li>
            {:else if routerStore.name === 'bootstrap'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">Bootstrap</li>
            {:else if routerStore.name === 'ssh-keys'}
              <li class="text-base-content/40">Security</li>
              <li class="font-medium">SSH Keys</li>
            {:else if routerStore.name === 'projects'}
              <li class="text-base-content/40">Orchestration</li>
              <li class="font-medium">Projects</li>
            {:else if routerStore.name === 'cluster'}
              <li class="text-base-content/40">Cluster</li>
              <li class="font-medium">Dashboard</li>
            {:else if activeViewLabel}
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
        <!-- Tenant selector — anchored top-right; the wrapper carries an
             explicit high z-index so the popover always layers above any
             page CTA (e.g. "Register Agent", "Put Key"). -->
        <div class="relative z-[1000]" data-testid="tenant-selector">
          <select
            class="select select-bordered select-xs font-mono hidden md:block relative z-[1000]"
            value={orchestratorStore.activeTenantID}
            aria-label="Active tenant"
            onchange={(e) => {
              const val = (e.target as HTMLSelectElement).value;
              setActiveTenant(val);
              void refresh();
            }}
          >
            {#each tenantOptions as t (t.id)}
              <option value={t.id}>{t.name}</option>
            {/each}
          </select>
        </div>

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

        <!-- Login / Logout — driven by authStatus so it stays in sync with
             the route guard. 'pending' renders nothing (we don't yet know). -->
        {#if authStatus === 'authenticated'}
          <button
            class="btn btn-ghost btn-square btn-sm"
            onclick={() => { void handleLogout(); }}
            aria-label="Sign out"
            title="Sign out"
          >
            <LogOut class="h-4 w-4" />
          </button>
        {:else if authStatus === 'unauthenticated'}
          <button
            class="btn btn-ghost btn-square btn-sm"
            onclick={handleLogin}
            aria-label="Sign in"
            title="Sign in"
          >
            <LogIn class="h-4 w-4" />
          </button>
        {/if}
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
            <div class="px-3 pt-4 pb-1">
              <span class="text-[10px] font-semibold tracking-[0.12em] text-base-content/30 uppercase">
                {section.title}
              </span>
            </div>
          {:else}
            <div class="my-1 border-t border-base-300/20"></div>
          {/if}

          {#each section.items as item}
            {@const NavIcon = item.icon}
            {@const isActive = navStore.activeView === item.id}
            <button
              class="flex w-full items-center gap-3 px-3 py-1.5 text-sm transition-colors rounded-none hover:bg-base-300/50"
              class:bg-base-300={isActive}
              class:text-base-content={true}
              class:font-medium={isActive}
              class:opacity-40={!item.implemented}
              onclick={() => { if (item.implemented) navigate(item.id as ViewID); }}
              title={navStore.sidebarCollapsed ? item.label : undefined}
              aria-current={isActive ? 'page' : undefined}
            >
              <NavIcon
                class="h-4 w-4 shrink-0 {isActive ? 'text-primary' : ''}"
                strokeWidth={isActive ? 2 : 1.5}
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
    <main class="content-bg flex-1 overflow-hidden flex flex-col">

      <!-- Hash router drives all view rendering. URL is the single source of truth. -->
      {#if routerStore.name === 'login'}
        <div class="flex-1 overflow-y-auto">
          <LoginView />
        </div>

      {:else if routerStore.name === 'register'}
        <div class="flex-1 overflow-y-auto">
          <RegisterView />
        </div>

      {:else if routerStore.name === 'dashboard'}
        <div class="flex-1 overflow-y-auto">
          <SystemDashboard />
        </div>

      {:else if routerStore.name === 'orchestration'}
        <div class="flex-1 overflow-y-auto">
          <OrchestratorDashboard />
        </div>

      {:else if routerStore.name === 'jobs'}
        {#if showJobDetail}
          <div class="flex-1 overflow-y-auto">
            <JobDetailView />
          </div>
        {:else}
          <div class="flex-1 overflow-y-auto">
            <JobsView />
          </div>
        {/if}

      {:else if routerStore.name === 'job-detail'}
        <div class="flex-1 overflow-y-auto">
          <JobDetailView />
        </div>

      {:else if routerStore.name === 'fleet'}
        <div class="flex-1 overflow-y-auto">
          <AgentFleet />
        </div>

      {:else if routerStore.name === 'agent-detail'}
        <div class="flex-1 overflow-y-auto">
          <AgentDetailView />
        </div>

      {:else if routerStore.name === 'sessions'}
        <div class="flex-1 overflow-hidden">
          <SessionList />
        </div>

      {:else if routerStore.name === 'session-detail'}
        {#if activeRouteSession !== null}
          <div class="flex-1 overflow-hidden">
            <SessionDetail session={activeRouteSession} />
          </div>
        {:else}
          <div class="flex flex-1 items-center justify-center text-base-content/30 text-sm">
            Session not found. It may have been closed or the ID is invalid.
          </div>
        {/if}

      {:else if routerStore.name === 'tasks'}
        <div class="flex-1 overflow-y-auto">
          <TasksView />
        </div>

      {:else if routerStore.name === 'workflows'}
        <div class="flex-1 overflow-y-auto">
          <WorkflowsView />
        </div>

      {:else if routerStore.name === 'events'}
        <div class="flex-1 overflow-y-auto flex flex-col">
          <EventStreamView />
        </div>

      {:else if routerStore.name === 'cost'}
        <div class="flex-1 overflow-y-auto">
          <CostView />
        </div>

      {:else if routerStore.name === 'inference'}
        <div class="flex-1 overflow-y-auto">
          <InferenceView />
        </div>

      {:else if routerStore.name === 'rag'}
        <div class="flex-1 overflow-y-auto">
          <RAGView />
        </div>

      {:else if routerStore.name === 'embeddings'}
        <div class="flex-1 overflow-y-auto">
          <EmbeddingsView />
        </div>

      {:else if routerStore.name === 'messaging'}
        <div class="flex-1 overflow-y-auto">
          <MessagingView />
        </div>

      {:else if routerStore.name === 'scheduler'}
        <div class="flex-1 overflow-y-auto">
          <SchedulerView />
        </div>

      {:else if routerStore.name === 'kv'}
        <div class="flex-1 overflow-y-auto">
          <KVStoreView />
        </div>

      {:else if routerStore.name === 'vectors'}
        <div class="flex-1 overflow-y-auto">
          <VectorsView />
        </div>

      {:else if routerStore.name === 'timeseries'}
        <div class="flex-1 overflow-y-auto">
          <TimeSeriesView />
        </div>

      {:else if routerStore.name === 'objects'}
        <div class="flex-1 overflow-y-auto">
          <ObjectsView />
        </div>

      {:else if routerStore.name === 'iam'}
        <div class="flex-1 overflow-y-auto">
          <IAMView />
        </div>

      {:else if routerStore.name === 'kms'}
        <div class="flex-1 overflow-y-auto">
          <KMSView />
        </div>

      {:else if routerStore.name === 'secrets'}
        <div class="flex-1 overflow-y-auto">
          <SecretsView />
        </div>

      {:else if routerStore.name === 'barrier'}
        <div class="flex-1 overflow-y-auto">
          <BarrierView />
        </div>

      {:else if routerStore.name === 'tenant-barrier'}
        <div class="flex-1 overflow-y-auto">
          <TenantBarrierView />
        </div>

      {:else if routerStore.name === 'ceremony'}
        <div class="flex-1 overflow-y-auto">
          <CeremonyView />
        </div>

      {:else if routerStore.name === 'bootstrap'}
        <div class="flex-1 overflow-y-auto">
          <BootstrapView />
        </div>

      {:else if routerStore.name === 'ssh-keys'}
        <div class="flex-1 overflow-y-auto">
          <SSHKeysView />
        </div>

      {:else if routerStore.name === 'projects'}
        <div class="flex-1 overflow-y-auto">
          <ProjectsView />
        </div>

      {:else if routerStore.name === 'cluster'}
        <div class="flex-1 overflow-y-auto">
          <ClusterDashboard />
        </div>

      {:else}
        <div class="flex-1 overflow-y-auto">
          <ComingSoon title={activeViewLabel || 'Coming Soon'} />
        </div>
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

{/if}
