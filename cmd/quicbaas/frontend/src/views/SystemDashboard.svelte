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
  SystemDashboard is the top-level overview page shown on the root route (#/).
  It presents daemon health, storage backend status, KMS/scheduler status, and
  high-level resource counts (sessions, agents, jobs, workflows). This is the
  "landing page" — all system-wide at a glance.

  Also exposes quick-action buttons for starting a job, spawning a session, and
  creating a new workflow; recent activity lists for jobs and sessions; and a
  cluster-mode status panel.
-->
<script lang="ts">
  import { connectionStore } from '../stores/connection.svelte.ts';
  import { orchestratorStore } from '../stores/orchestrator.svelte.ts';
  import type { JobStatus, SessionInfo } from '../stores/orchestrator.svelte.ts';
  import { sessionStore } from '../stores/sessionStore.svelte.ts';
  import { fleetStore } from '../stores/fleetStore.svelte.ts';
  import { attentionStore } from '../stores/attentionStore.svelte.ts';
  import { parkedCountByProject } from '../lib/attention.ts';
  import { formatRelative } from '../lib/time.ts';
  import { getSDKClient } from '../stores/sdkClient.ts';
  import { navigate } from '../stores/navigation.svelte.ts';
  import StatCard from '../components/core/StatCard.svelte';
  import PageHeader from '../components/core/PageHeader.svelte';
  import { formatDateTime } from '../lib/time.ts';
  import { countLiveSessions } from '../lib/sessions.ts';
  import StatusBadge from '../components/core/StatusBadge.svelte';
  import StartJobDialog from '../components/orchestrator/StartJobDialog.svelte';
  import SpawnDialog from '../components/orchestrator/SpawnDialog.svelte';
  import {
    Server,
    HardDrive,
    Key,
    Clock,
    MessageSquare,
    Briefcase,
    ServerCog,
    Workflow,
    CheckCircle,
    XCircle,
    AlertTriangle,
    ExternalLink,
    LayoutDashboard,
    Play,
    Plus,
    Terminal,
    Eye,
    Inbox,
  } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Health data
  // ---------------------------------------------------------------------------

  interface ServiceHealth {
    name: string;
    /**
     * 'not-permitted' is deliberately distinct from 'down': the daemon
     * answered, and the answer was that this caller may not see the
     * subsystem. Folding the two together renders an authorization result as
     * an outage (#219) — the operator reads a red tile and starts
     * debugging a service that is running.
     */
    status: 'ok' | 'degraded' | 'down' | 'not-permitted' | 'unknown';
    detail?: string;
    /** Navigation ViewID to jump to this subsystem, if one exists. */
    viewID?: string;
  }

  // Raw response shape from /api/v1/health/live or /api/v1/health/ready.
  interface DaemonHealthResponse {
    status: string;
    uptime?: string;
    checks?: Record<string, { status: string; message?: string }>;
  }

  let daemonHealth: ServiceHealth = $state({ name: 'Daemon', status: 'unknown' });
  let orchestratorHealth: ServiceHealth = $state({ name: 'Software Factory', status: 'unknown', viewID: 'orchestration' });
  let storageHealth: ServiceHealth = $state({ name: 'Storage (PebbleDB)', status: 'unknown' });
  let kmsHealth: ServiceHealth = $state({ name: 'KMS', status: 'unknown', viewID: 'kms' });
  let schedulerHealth: ServiceHealth = $state({ name: 'Scheduler', status: 'unknown', viewID: 'scheduler' });

  let uptime: string = $state('—');
  let version: string = $state('—');
  let daemonAddress: string = $state('');

  // ---------------------------------------------------------------------------
  // Data fetch helpers
  // ---------------------------------------------------------------------------

  /**
   * Probe the daemon liveness endpoint directly via fetch so we don't depend on
   * a `connection.healthCheck()` method that does not exist on ConnectionClient.
   * Uses the SDK's effective base URL (from getAddress()) to avoid hardcoding.
   */
  async function probeDaemonLiveness(): Promise<DaemonHealthResponse> {
    const client = await getSDKClient();
    const base = client.connection.getAddress().replace(/\/$/, '');
    const resp = await fetch(`${base}/api/v1/health/live`, {
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) {
      throw new Error(`liveness probe failed: ${resp.status}`);
    }
    return resp.json() as Promise<DaemonHealthResponse>;
  }

  // Convert Go time.Duration string ("2m52.039502392s") to a friendly form
  // ("2m 52s"). Falls back to the original on parse failure.
  function formatUptime(raw: string): string {
    if (!raw) return '—';
    const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)(?:\.\d+)?s)?$/.exec(raw.trim());
    if (!m) return raw;
    const [, h, mm, ss] = m;
    const parts: string[] = [];
    if (h) parts.push(`${h}h`);
    if (mm) parts.push(`${mm}m`);
    if (ss || parts.length === 0) parts.push(`${ss ?? '0'}s`);
    return parts.join(' ');
  }

  /** Probe /api/v1/health/ready which includes uptime. */
  async function probeDaemonReadiness(): Promise<DaemonHealthResponse> {
    const client = await getSDKClient();
    const base = client.connection.getAddress().replace(/\/$/, '');
    const resp = await fetch(`${base}/api/v1/health/ready`, {
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) {
      throw new Error(`readiness probe failed: ${resp.status}`);
    }
    return resp.json() as Promise<DaemonHealthResponse>;
  }

  // ---------------------------------------------------------------------------
  // Probe failure classification
  // ---------------------------------------------------------------------------

  /**
   * httpStatus returns the HTTP status code a failed SDK call carries, or
   * undefined when the failure never reached an HTTP response (DNS failure,
   * TLS failure, connection refused, abort — fetch rejects with a TypeError
   * that has no status).
   *
   * The SDK rejects every non-2xx response with an APIError
   * (sdk/typescript/src/errors.ts) whose `status` field is the response's
   * numeric status. The code is read off the error object, never matched out
   * of its message: the message is the server's prose — here go-qrdb's RBAC
   * denial reason — and is free to change, while the status is the contract.
   * The property is read structurally rather than through `instanceof
   * APIError` so a probe rejected by a non-REST transport (or a test double)
   * that carries the same field classifies identically.
   *
   * Both transports carry it: the Wails desktop transport classifies a
   * binding failure from its cause (cmd/quicbaas/errors_gui.go), its error
   * formatter sends the code and status to JS, and the TypeScript SDK turns
   * that rejection into the same APIError the REST client throws (#236).
   */
  function httpStatus(err: unknown): number | undefined {
    const status = (err as { status?: unknown } | null | undefined)?.status;
    return typeof status === 'number' ? status : undefined;
  }

  /**
   * denialDetail returns a short reason when a probe failed because the caller
   * is not permitted to make it, and undefined for every other failure. 401
   * and 403 both prove the daemon is UP and answering — the subsystem's true
   * state is simply not visible to this account — so they must never be
   * reported as an outage.
   */
  function denialDetail(err: unknown): string | undefined {
    switch (httpStatus(err)) {
      case 401:
        return 'authentication required';
      case 403:
        return 'permission denied';
      default:
        return undefined;
    }
  }

  // ---------------------------------------------------------------------------
  // Aggregated fetch
  // ---------------------------------------------------------------------------

  async function fetchSystemHealth(): Promise<void> {
    const client = await getSDKClient();
    daemonAddress = client.connection.getAddress();

    // --- 1. Daemon liveness ---------------------------------------------------
    try {
      const live = await probeDaemonLiveness();
      const isOk = live.status === 'healthy' || live.status === 'ok';
      daemonHealth = {
        name: 'Daemon',
        status: isOk ? 'ok' : 'degraded',
      };
    } catch {
      daemonHealth = { name: 'Daemon', status: 'down' };
    }

    // --- 2. Uptime from readiness endpoint ------------------------------------
    try {
      const ready = await probeDaemonReadiness();
      if (ready.uptime) uptime = formatUptime(ready.uptime);
    } catch {
      // Non-fatal — uptime stays as '—'
    }

    // --- 3. Orchestrator health -----------------------------------------------
    try {
      const orchData = await client.orchestrator.healthCheck();
      // HealthCheckReport has a timestamp + session counts but no explicit status
      // field. If the call succeeds, the orchestrator is reachable.
      const payload = orchData as unknown as Record<string, unknown>;
      const statusField = payload['status'] as string | undefined;
      const orchOk = !statusField || statusField === 'ok' || statusField === 'healthy';
      orchestratorHealth = {
        name: 'Software Factory',
        status: orchOk ? 'ok' : 'degraded',
        viewID: 'orchestration',
      };

      // Derive storage health from the orchestrator payload when present.
      if (payload['storage']) {
        const s = payload['storage'] as Record<string, unknown>;
        const stStatus = s['status'] === 'ok' || s['status'] === 'healthy' ? 'ok' : 'degraded';
        storageHealth = {
          name: 'Storage (PebbleDB)',
          status: stStatus,
          detail: s['detail'] as string | undefined,
        };
      } else {
        storageHealth = { name: 'Storage (PebbleDB)', status: orchOk ? 'ok' : 'degraded' };
      }
    } catch (err: unknown) {
      // A denial says nothing about the orchestrator's health, and the storage
      // tile is derived from the same payload, so both report the denial
      // rather than inventing a state neither probe observed.
      //
      // The denial that prompted this is go-qrdb's: it tags orchestrator:health
      // ScopeTenantOnly, which its RBAC refuses to the system tenant outright,
      // so the operator persona cannot read the node's own orchestrator state
      // (jeremyhahn/go-qrdb#183). That scope is declared entirely inside the
      // dependency and is not ours to change. Reporting it accurately is.
      const denial = denialDetail(err);
      if (denial) {
        orchestratorHealth = {
          name: 'Software Factory',
          status: 'not-permitted',
          detail: denial,
          viewID: 'orchestration',
        };
        storageHealth = { name: 'Storage (PebbleDB)', status: 'not-permitted', detail: denial };
      } else {
        orchestratorHealth = { name: 'Software Factory', status: 'down', viewID: 'orchestration' };
        storageHealth = { name: 'Storage (PebbleDB)', status: 'unknown' };
      }
    }

    // --- 4. KMS health — probe by listing keys (lightweight, limit=1) ---------
    try {
      await client.kms.listKeys(1);
      kmsHealth = { name: 'KMS', status: 'ok', viewID: 'kms' };
    } catch (err: unknown) {
      // A 401/403 means KMS is up but requires auth — treat as ok.
      const status = httpStatus(err);
      if (status === 401 || status === 403) {
        kmsHealth = { name: 'KMS', status: 'ok', viewID: 'kms' };
      } else {
        kmsHealth = { name: 'KMS', status: 'down', viewID: 'kms' };
      }
    }

    // --- 5. Scheduler health — probe by listing jobs (returns 200 when up) ----
    try {
      await client.scheduler.listJobs({ limit: 1 });
      schedulerHealth = { name: 'Scheduler', status: 'ok', viewID: 'scheduler' };
    } catch (err: unknown) {
      const status = httpStatus(err);
      if (status === 401 || status === 403) {
        schedulerHealth = { name: 'Scheduler', status: 'ok', viewID: 'scheduler' };
      } else {
        schedulerHealth = { name: 'Scheduler', status: 'down', viewID: 'scheduler' };
      }
    }

    // --- 6. Version — open /api/v1/version endpoint --------------------------
    if (version === '—') {
      try {
        const base = client.connection.getAddress().replace(/\/$/, '');
        const resp = await fetch(`${base}/api/v1/version`, {
          headers: { Accept: 'application/json' },
        });
        if (resp.ok) {
          const v = (await resp.json()) as { version?: string };
          if (v.version) version = v.version;
        }
      } catch {
        // Non-fatal — version stays as '—' (e.g., older daemon without this route).
      }
    }
  }

  $effect(() => {
    void fetchSystemHealth();
    const interval = setInterval(() => { void fetchSystemHealth(); }, 30_000);
    return () => { clearInterval(interval); };
  });

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function statusVariant(s: ServiceHealth['status']): 'success' | 'error' | 'warning' | 'ghost' {
    if (s === 'ok') return 'success';
    if (s === 'down') return 'error';
    if (s === 'degraded') return 'warning';
    // 'not-permitted' and 'unknown' are both "no signal" — neutral, never red.
    return 'ghost';
  }

  /** statusLabel renders the internal status value as prose for the badge. */
  function statusLabel(s: ServiceHealth['status']): string {
    return s === 'not-permitted' ? 'not permitted' : s;
  }

  const services = $derived<ServiceHealth[]>([
    daemonHealth,
    orchestratorHealth,
    storageHealth,
    kmsHealth,
    schedulerHealth,
  ]);

  // Services whose status is meaningful for overall health eval. 'unknown'
  // (not probed yet) and 'not-permitted' (probed, refused) both carry no
  // observation of the subsystem's health, so neither may move the banner.
  const knownServices = $derived(
    services.filter((s) => s.status !== 'unknown' && s.status !== 'not-permitted'),
  );
  const allOk = $derived(knownServices.length > 0 && knownServices.every((s) => s.status === 'ok'));
  const anyDown = $derived(services.some((s) => s.status === 'down'));
  const anyDegraded = $derived(services.some((s) => s.status === 'degraded'));

  // Subsystems the account may not see. Reported separately from problemNames:
  // they are not problems with the system, they are limits on this session.
  const notPermitted = $derived(services.filter((s) => s.status === 'not-permitted'));
  const anyNotPermitted = $derived(notPermitted.length > 0);
  const notPermittedNames = $derived(notPermitted.map((s) => s.name).join(', '));

  // "All systems operational" claims every subsystem was observed and was
  // healthy. A subsystem the account may not see was not observed at all, and
  // it could be failing behind the denial — so a denial withholds the green
  // banner rather than being quietly excluded from it. That is the mirror of
  // the bug this fixes: refusing to call a denial an outage must not turn into
  // calling it health.
  const allVisible = $derived(allOk && !anyNotPermitted);

  // Names of problem subsystems for the banner summary.
  const problemNames = $derived(
    services
      .filter((s) => s.status === 'down' || s.status === 'degraded')
      .map((s) => s.name)
      .join(', '),
  );

  // Effective address: prefer the SDK's address, fall back to connectionStore.
  const effectiveAddress = $derived(daemonAddress || connectionStore.address || '—');

  // Project health strip: parked count per project, from the attention
  // inbox's own items — a function rather than a $derived so the template
  // reads a fresh Map each render without a reactive dependency footgun over
  // a Map's internal mutation.
  function parkedByProject() {
    return parkedCountByProject(attentionStore.items);
  }

  // Fleet node counts from fleetStore (has the real SDK-backed data).
  const totalFleetNodes = $derived((Array.isArray(fleetStore.nodes) ? fleetStore.nodes : []).length);
  const onlineFleetNodes = $derived(fleetStore.onlineNodes.length);

  // Session counts. The list holds every session the tenant has, closed ones
  // included, because the Sessions view shows them and a closed session is
  // still openable by id — so the tile that claims live state counts the live
  // records and reports the total beside it rather than as it.
  //
  // This tile read 0 in the browser until go-quicbaas#864: it named `status`,
  // which is what the Wails binding calls the lifecycle field, while the REST
  // wire calls it `state`. countLiveSessions reads the value, not a name.
  const allSessionRecords = $derived(
    Array.isArray(sessionStore.sessions) ? sessionStore.sessions : [],
  );
  const liveSessionCount = $derived(countLiveSessions(allSessionRecords));

  // Job counts from orchestratorStore.
  const allJobs = $derived(Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : []);
  const activeJobCount = $derived(
    allJobs.filter((j) => j.state !== 'completed' && j.state !== 'failed').length,
  );

  // ---------------------------------------------------------------------------
  // Cluster status panel (reuses /api/v1/cluster/status)
  // ---------------------------------------------------------------------------

  interface ClusterStatusResponse {
    node_id: number;
    total_nodes?: number;
    shards?: Array<{ shard_id: number; leader_id: number; state: string }>;
    healthy: boolean;
    timestamp: string;
  }

  let clusterStatus: ClusterStatusResponse | null = $state(null);

  async function fetchClusterStatus(): Promise<void> {
    try {
      // Use the authenticated SDK cluster client so the bearer token is
      // attached (a raw fetch omitted it and 401'd under default-deny authz).
      const client = await getSDKClient();
      clusterStatus = await client.cluster.status();
    } catch {
      // Non-fatal — cluster panel stays hidden.
    }
  }

  $effect(() => {
    void fetchClusterStatus();
    const interval = setInterval(() => { void fetchClusterStatus(); }, 60_000);
    return () => { clearInterval(interval); };
  });

  const clusterMode = $derived.by((): string => {
    if (!clusterStatus) return '—';
    if (!clusterStatus.shards || clusterStatus.shards.length === 0) return 'standalone';
    return 'raft';
  });

  const nodeCount = $derived.by((): number => {
    if (!clusterStatus) return 0;
    return clusterStatus.total_nodes ?? (clusterStatus.node_id !== 0 ? 1 : 0);
  });

  // ---------------------------------------------------------------------------
  // Workflows count
  // ---------------------------------------------------------------------------

  let workflowCount = $state(0);

  async function fetchWorkflowCount(): Promise<void> {
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.listWorkflows(orchestratorStore.activeTenantID);
      workflowCount = (resp.workflows ?? []).length;
    } catch {
      // Non-fatal.
    }
  }

  $effect(() => {
    void fetchWorkflowCount();
    const interval = setInterval(() => { void fetchWorkflowCount(); }, 30_000);
    return () => { clearInterval(interval); };
  });

  // ---------------------------------------------------------------------------
  // Recent activity
  // ---------------------------------------------------------------------------

  // The recent-activity lists are keyed by job_id / session_id, so a malformed
  // record without one would (a) collide on an `undefined` key and (b) blank the
  // whole dashboard. Filter to records that carry their id — the activeJobCount
  // stat above still counts every job, so the count is unaffected.
  const recentJobs = $derived<JobStatus[]>(
    [...allJobs]
      .filter((j) => Boolean(j.job_id))
      .sort((a, b) => b.started_at - a.started_at)
      .slice(0, 5),
  );

  const allSessions = $derived<SessionInfo[]>(
    Array.isArray(orchestratorStore.sessions) ? orchestratorStore.sessions : [],
  );

  const recentSessions = $derived<SessionInfo[]>(
    [...allSessions]
      .filter((s) => Boolean(s.session_id))
      .sort((a, b) => b.created_at - a.created_at)
      .slice(0, 5),
  );

  function jobStateVariant(state: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (state === 'completed') return 'success';
    if (state === 'failed') return 'error';
    if (state === 'executing' || state === 'assigning') return 'info';
    return 'ghost';
  }

  const formatTs = (tsUnix: number): string => formatDateTime(tsUnix);

  // ---------------------------------------------------------------------------
  // Quick-action dialogs
  // ---------------------------------------------------------------------------

  let startJobOpen = $state(false);
  let spawnOpen = $state(false);
</script>

<!-- Quick-action dialogs -->
<StartJobDialog bind:open={startJobOpen} />
<SpawnDialog open={spawnOpen} onClose={() => { spawnOpen = false; }} />

<div class="p-6 space-y-6">
  <PageHeader title="System Status" subtitle="High-level view of daemon health, activity, and resource counts" icon={LayoutDashboard} />

  <!-- Overall health banner -->
  <div
    class="alert flex-wrap"
    class:alert-success={allVisible && connectionStore.connected}
    class:alert-error={anyDown || !connectionStore.connected}
    class:alert-warning={!allOk && !anyDown && anyDegraded && connectionStore.connected}
    role="status"
    aria-live="polite"
  >
    {#if !connectionStore.connected}
      <XCircle class="h-5 w-5 shrink-0" />
      <span class="font-semibold">Daemon unreachable</span>
      <span class="text-sm opacity-75">Check server address and TLS configuration.</span>
    {:else if anyDown}
      <AlertTriangle class="h-5 w-5 shrink-0" />
      <span class="font-semibold">Degraded</span>
      <span class="text-sm opacity-75">{problemNames} {anyDown ? 'down' : 'unhealthy'}</span>
    {:else if anyDegraded}
      <AlertTriangle class="h-5 w-5 shrink-0" />
      <span class="font-semibold">Partially degraded</span>
      <span class="text-sm opacity-75">{problemNames} reporting issues</span>
    {:else if allVisible}
      <CheckCircle class="h-5 w-5 shrink-0" />
      <span class="font-semibold">All systems operational</span>
      {#if version !== '—'}
        <span class="badge badge-sm badge-ghost ml-auto font-mono">v{version}</span>
      {/if}
    {:else if anyNotPermitted}
      <!-- Nothing observed is failing, but not everything was observed.
           Neutral, not green and not red — the honest third answer. -->
      <Eye class="h-5 w-5 shrink-0" />
      <span class="font-semibold">Partial visibility</span>
      <span class="text-sm opacity-75"
        >{notPermittedNames} not visible to this account — state unknown</span
      >
      {#if version !== '—'}
        <span class="badge badge-sm badge-ghost ml-auto font-mono">v{version}</span>
      {/if}
    {:else}
      <!-- Still probing — services still 'unknown' -->
      <span class="loading loading-spinner loading-xs shrink-0"></span>
      <span class="font-semibold">Checking system health…</span>
    {/if}
  </div>

  <!-- Quick stats -->
  <div class="grid grid-cols-2 lg:grid-cols-5 gap-4">

    <!-- Needs you tile -->
    <button
      class="text-left hover:scale-[1.02] transition-transform cursor-pointer"
      onclick={() => navigate('attention')}
      aria-label="Go to the Needs you inbox"
      data-testid="stat-attention"
    >
      <StatCard
        label="Needs you"
        value={attentionStore.total}
        description={attentionStore.total > 0 ? `oldest waiting ${formatRelative(attentionStore.oldestAgeInstant)}` : 'nothing waiting'}
        icon={Inbox}
        variant={attentionStore.total > 0 ? 'warning' : 'default'}
      />
    </button>

    <!-- Fleet node count tile -->
    <button
      class="text-left hover:scale-[1.02] transition-transform cursor-pointer"
      onclick={() => navigate('fleet')}
      aria-label="Go to Fleet view"
      data-testid="stat-agents"
    >
      <StatCard
        label="Fleet nodes"
        value={totalFleetNodes}
        description="{onlineFleetNodes} online"
        icon={ServerCog}
        variant={onlineFleetNodes > 0 ? 'success' : 'default'}
      />
    </button>

    <!-- Sessions tile -->
    <button
      class="text-left hover:scale-[1.02] transition-transform cursor-pointer"
      onclick={() => navigate('sessions')}
      aria-label="Go to Sessions view"
      data-testid="stat-sessions"
    >
      <StatCard
        label="Active Sessions"
        value={liveSessionCount}
        description="{allSessionRecords.length} total"
        icon={MessageSquare}
        variant={liveSessionCount > 0 ? 'primary' : 'default'}
      />
    </button>

    <!-- Workflows tile -->
    <button
      class="text-left hover:scale-[1.02] transition-transform cursor-pointer"
      onclick={() => navigate('workflows')}
      aria-label="Go to Workflows view"
      data-testid="stat-workflows"
    >
      <StatCard
        label="Workflows"
        value={workflowCount}
        description="registered"
        icon={Workflow}
      />
    </button>

    <!-- Jobs tile -->
    <button
      class="text-left hover:scale-[1.02] transition-transform cursor-pointer"
      onclick={() => navigate('jobs')}
      aria-label="Go to Jobs view"
      data-testid="stat-jobs"
    >
      <StatCard
        label="Total Jobs"
        value={allJobs.length}
        description="{activeJobCount} active"
        icon={Briefcase}
        variant={activeJobCount > 0 ? 'primary' : 'default'}
      />
    </button>
  </div>

  <!-- Project health strip: per project, parked count is read from the
       attention inbox's own facets — real, today. Merged PRs (24h) and
       worker success rate need a job-history aggregation this endpoint does
       not yet compute, so they render as a dash rather than a fabricated
       number until that aggregation exists (see go-quicbaas issue tracker,
       "project health strip: merged PRs / success rate"). -->
  {#if parkedByProject().size > 0}
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-5">
        <h3 class="text-sm font-semibold text-base-content mb-3">Project Health</h3>
        <div class="overflow-x-auto">
          <table class="table table-sm">
            <thead>
              <tr>
                <th>Project</th>
                <th>Merged PRs (24h)</th>
                <th>Worker success rate</th>
                <th>Parked</th>
              </tr>
            </thead>
            <tbody>
              {#each [...parkedByProject().entries()] as [projectId, info] (projectId)}
                <tr class="hover cursor-pointer" onclick={() => navigate('attention')}>
                  <td class="font-medium">{info.name}</td>
                  <td class="text-base-content/40">—</td>
                  <td class="text-base-content/40">—</td>
                  <td>
                    <span class="badge badge-warning badge-sm">{info.count}</span>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  {/if}

  <!-- Status panel: cluster mode + node count + version -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-5">
      <h3 class="text-sm font-semibold text-base-content mb-3">Cluster Status</h3>
      <dl class="grid grid-cols-2 sm:grid-cols-4 gap-x-6 gap-y-2 text-sm">
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Mode</dt>
          <dd class="mt-0.5">
            {#if clusterMode === 'standalone'}
              <span class="badge badge-info badge-sm">standalone</span>
            {:else if clusterMode === 'raft'}
              <span class="badge badge-success badge-sm">raft</span>
            {:else}
              <span class="text-base-content/40 font-mono">—</span>
            {/if}
          </dd>
        </div>
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Nodes</dt>
          <dd class="font-mono mt-0.5">{nodeCount > 0 ? nodeCount : '—'}</dd>
        </div>
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Version</dt>
          <dd class="font-mono mt-0.5">{version !== '—' ? `v${version}` : '—'}</dd>
        </div>
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Uptime</dt>
          <dd class="font-mono mt-0.5">{uptime}</dd>
        </div>
      </dl>
    </div>
  </div>

  <!-- Resource counts (legacy row — connection details below) -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-2" style="display:none" aria-hidden="true">
  </div>

  <!-- Service health grid -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-5">
      <h3 class="text-sm font-semibold text-base-content mb-3">Service Health</h3>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
        {#each services as svc (svc.name)}
          <div class="flex items-center justify-between rounded-btn bg-base-300/40 px-3 py-2">
            <div class="flex items-center gap-2 min-w-0">
              <!-- Colored status dot -->
              <span
                class="inline-block h-2 w-2 shrink-0 rounded-full {
                  svc.status === 'ok' ? 'bg-success' :
                  svc.status === 'down' ? 'bg-error' :
                  svc.status === 'degraded' ? 'bg-warning animate-pulse' :
                  'opacity-20 bg-current'
                }"
              ></span>

              {#if svc.name === 'Daemon'}
                <Server class="h-4 w-4 text-base-content/40 shrink-0" strokeWidth={1.5} />
              {:else if svc.name.startsWith('Storage')}
                <HardDrive class="h-4 w-4 text-base-content/40 shrink-0" strokeWidth={1.5} />
              {:else if svc.name === 'KMS'}
                <Key class="h-4 w-4 text-base-content/40 shrink-0" strokeWidth={1.5} />
              {:else if svc.name === 'Scheduler'}
                <Clock class="h-4 w-4 text-base-content/40 shrink-0" strokeWidth={1.5} />
              {:else}
                <Workflow class="h-4 w-4 text-base-content/40 shrink-0" strokeWidth={1.5} />
              {/if}

              <span class="text-sm truncate">{svc.name}</span>
              {#if svc.detail}
                <span class="text-xs text-base-content/30 truncate hidden sm:inline">{svc.detail}</span>
              {/if}
            </div>

            <div class="flex items-center gap-2 shrink-0 ml-2">
              <StatusBadge
                text={statusLabel(svc.status)}
                variant={statusVariant(svc.status)}
                pulse={svc.status === 'degraded'}
              />
              {#if svc.viewID}
                <button
                  class="btn btn-ghost btn-xs text-base-content/40 hover:text-base-content"
                  onclick={() => navigate(svc.viewID as Parameters<typeof navigate>[0])}
                  aria-label="View {svc.name}"
                  title="View {svc.name}"
                >
                  <ExternalLink class="h-3 w-3" />
                </button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </div>
  </div>

  <!-- Connection details -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-5">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-base-content">Connection</h3>
        {#if version !== '—'}
          <span class="badge badge-sm badge-ghost font-mono">v{version}</span>
        {/if}
      </div>
      <dl class="grid grid-cols-2 sm:grid-cols-4 gap-x-6 gap-y-2 text-sm">
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Address</dt>
          <dd class="font-mono mt-0.5 break-all">{effectiveAddress}</dd>
        </div>
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Transport</dt>
          <dd class="font-mono mt-0.5">{connectionStore.transport || 'rest'}</dd>
        </div>
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Version</dt>
          <dd class="font-mono mt-0.5">{version}</dd>
        </div>
        <div>
          <dt class="text-base-content/40 text-xs uppercase tracking-wider">Uptime</dt>
          <dd class="font-mono mt-0.5">{uptime}</dd>
        </div>
      </dl>
    </div>
  </div>

  <!-- Recent activity -->
  <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">

    <!-- Recent jobs -->
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-5">
        <div class="flex items-center justify-between mb-3">
          <h3 class="text-sm font-semibold text-base-content">Recent Jobs</h3>
          <button class="btn btn-ghost btn-xs" onclick={() => navigate('jobs')}>
            View all
          </button>
        </div>
        {#if recentJobs.length === 0}
          <p class="text-sm text-base-content/30 text-center py-4">No jobs yet.</p>
        {:else}
          <ul class="space-y-2" data-testid="recent-jobs-list">
            {#each recentJobs as job (job.job_id)}
              <li class="flex items-center justify-between gap-2 text-sm">
                <span class="truncate font-mono text-xs text-base-content/60" title={job.job_id}>
                  {job.description || (job.job_id ?? '').slice(0, 12)}
                </span>
                <div class="flex items-center gap-2 shrink-0">
                  <StatusBadge text={job.state} variant={jobStateVariant(job.state)} />
                  <span class="text-xs text-base-content/30 hidden sm:inline">
                    {formatTs(job.started_at)}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </div>

    <!-- Recent sessions -->
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-5">
        <div class="flex items-center justify-between mb-3">
          <h3 class="text-sm font-semibold text-base-content">Recent Sessions</h3>
          <button class="btn btn-ghost btn-xs" onclick={() => navigate('sessions')}>
            View all
          </button>
        </div>
        {#if recentSessions.length === 0}
          <p class="text-sm text-base-content/30 text-center py-4">No sessions yet.</p>
        {:else}
          <ul class="space-y-2" data-testid="recent-sessions-list">
            {#each recentSessions as session (session.session_id)}
              <li class="flex items-center justify-between gap-2 text-sm">
                <div class="flex items-center gap-2 min-w-0">
                  <span class="badge badge-xs badge-ghost truncate">{session.backend}</span>
                  <span class="font-mono text-xs text-base-content/40 truncate" title={session.session_id}>
                    {(session.session_id ?? '').slice(0, 10)}
                  </span>
                </div>
                <StatusBadge text={session.state} variant="ghost" />
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </div>
  </div>

  <!-- Quick actions -->
  <div class="card bg-base-200 rounded-box">
    <div class="card-body p-5">
      <h3 class="text-sm font-semibold text-base-content mb-3">Quick Actions</h3>
      <div class="flex flex-wrap gap-3">
        <button
          class="btn btn-primary btn-sm gap-2"
          onclick={() => { startJobOpen = true; }}
          aria-label="Start a new job"
          data-testid="quick-start-job"
        >
          <Play class="h-4 w-4" />
          Start Job
        </button>
        <button
          class="btn btn-secondary btn-sm gap-2"
          onclick={() => { spawnOpen = true; }}
          aria-label="Spawn a new session"
          data-testid="quick-spawn-session"
        >
          <Terminal class="h-4 w-4" />
          Spawn Session
        </button>
        <button
          class="btn btn-ghost btn-sm gap-2"
          onclick={() => navigate('workflows')}
          aria-label="Create a new workflow"
          data-testid="quick-new-workflow"
        >
          <Plus class="h-4 w-4" />
          New Workflow
        </button>
      </div>
    </div>
  </div>
</div>
