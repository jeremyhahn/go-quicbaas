<!--
  OrchestratorDashboard shows summary stats, how jobs and agents split
  across their states, and the 10 most recent events.
-->
<script lang="ts">
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { countLiveSessions } from '../../lib/sessions.ts';
  import StatCard from '../../components/core/StatCard.svelte';
  import BreakdownBars, { type BreakdownRow } from '../../components/core/BreakdownBars.svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import {
    Workflow,
    ServerCog,
    Terminal,
    ClipboardList,
    LayoutDashboard,
  } from 'lucide-svelte';


  // ---------------------------------------------------------------------------
  // Derived stats
  // ---------------------------------------------------------------------------

  const activeJobs = $derived(
    (Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : [])
      .filter((j) => j.state !== 'completed' && j.state !== 'failed').length
  );

  const pendingTasks = $derived(
    (Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : [])
      .filter((t) => t.status === 'pending').length
  );

  // The session list holds closed sessions as well — the Sessions view shows
  // them — so a card that says "active" counts the live ones and reports the
  // total beside it, the same way Active Jobs and Pending Tasks do.
  //
  // countLiveSessions reads the lifecycle state itself: the REST wire calls it
  // `state` and the Wails binding calls it `status`, so naming one here made
  // the tile read 0 on the other platform (go-quicbaas#864).
  const allSessions = $derived(
    Array.isArray(orchestratorStore.sessions) ? orchestratorStore.sessions : []
  );

  const liveSessionCount = $derived(countLiveSessions(allSessions));

  const recentEvents = $derived(
    (Array.isArray(orchestratorStore.events) ? orchestratorStore.events : []).slice(-10)
  );

  // ---------------------------------------------------------------------------
  // State breakdowns
  // ---------------------------------------------------------------------------

  /** JOB_STATE_ORDER is the job lifecycle, so the breakdown reads in the order a job moves. */
  const JOB_STATE_ORDER: readonly string[] = [
    'initializing', 'decomposing', 'scheduling', 'assigning', 'executing', 'validating',
    'reviewing', 're-delegating', 'completing', 'cleanup', 'escalating', 'completed', 'failed',
  ];

  const FLEET_NODE_STATUSES: readonly string[] = ['running', 'idle', 'registered', 'offline', 'error'];

  /** Only the job states that have jobs in them: thirteen rows of mostly zero is noise. */
  const jobStateRows = $derived.by((): BreakdownRow[] => {
    const counts = new Map<string, number>();
    for (const job of (Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : [])) {
      counts.set(job.state, (counts.get(job.state) ?? 0) + 1);
    }
    const known = JOB_STATE_ORDER.filter((state) => counts.has(state));
    const unknown = [...counts.keys()].filter((state) => !JOB_STATE_ORDER.includes(state)).sort();
    return [...known, ...unknown].map((state) => ({ label: state, count: counts.get(state) ?? 0 }));
  });

  /** Every fleet node status, zeros included: "0 in error" is itself the answer an operator wants. */
  const fleetStatusRows = $derived.by((): BreakdownRow[] => {
    const nodes = Array.isArray(orchestratorStore.fleetNodes) ? orchestratorStore.fleetNodes : [];
    return FLEET_NODE_STATUSES.map((status) => ({
      label: status,
      count: nodes.filter((node) => node.status === status).length,
    }));
  });
</script>

<div class="page">
  <PageHeader title="Dashboard" subtitle="Jobs, fleet nodes, sessions and tasks at a glance" icon={LayoutDashboard} />

  <!-- Stat cards -->
  <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
    <StatCard
      label="Active Jobs"
      value={activeJobs}
      description="{(Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : []).length} total"
      icon={Workflow}
      variant={activeJobs > 0 ? 'primary' : 'default'}
    />
    <StatCard
      label="Fleet nodes"
      value={(Array.isArray(orchestratorStore.fleetNodes) ? orchestratorStore.fleetNodes : []).length}
      description="{(Array.isArray(orchestratorStore.fleetNodes) ? orchestratorStore.fleetNodes : []).filter((a) => a.status === 'running').length} running"
      icon={ServerCog}
    />
    <StatCard
      label="Active Sessions"
      value={liveSessionCount}
      description="{allSessions.length} total"
      icon={Terminal}
      variant={liveSessionCount > 0 ? 'primary' : 'default'}
    />
    <StatCard
      label="Pending Tasks"
      value={pendingTasks}
      description="{(Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : []).length} total"
      icon={ClipboardList}
      variant={pendingTasks > 0 ? 'warning' : 'default'}
    />
  </div>

  <!-- State breakdowns -->
  <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
    <div class="card rounded-box bg-base-200">
      <div class="card-body gap-0 p-5">
        <div class="mb-4 flex items-baseline justify-between gap-3">
          <h3 class="text-[0.9375rem] font-semibold">Job states</h3>
          <a class="link link-hover text-xs text-primary" href="#/jobs">All jobs</a>
        </div>
        {#if jobStateRows.length === 0}
          <div class="flex h-32 items-center justify-center text-sm text-base-content/60">
            No jobs
          </div>
        {:else}
          <BreakdownBars rows={jobStateRows} ariaLabel="Jobs by state" />
        {/if}
      </div>
    </div>

    <div class="card rounded-box bg-base-200">
      <div class="card-body gap-0 p-5">
        <div class="mb-4 flex items-baseline justify-between gap-3">
          <h3 class="text-[0.9375rem] font-semibold">Fleet status</h3>
          <a class="link link-hover text-xs text-primary" href="#/fleet">Fleet</a>
        </div>
        {#if (Array.isArray(orchestratorStore.fleetNodes) ? orchestratorStore.fleetNodes : []).length === 0}
          <div class="flex h-32 items-center justify-center text-sm text-base-content/60">
            No fleet nodes registered
          </div>
        {:else}
          <BreakdownBars rows={fleetStatusRows} ariaLabel="Fleet nodes by status" />
        {/if}
      </div>
    </div>
  </div>

  <!-- Recent events -->
  <div class="card rounded-box bg-base-200">
    <div class="card-body gap-0 p-5">
      <h3 class="mb-3 text-[0.9375rem] font-semibold">Recent events</h3>
      <EventLog events={recentEvents} maxHeight="200px" autoScroll={false} />
    </div>
  </div>
</div>
