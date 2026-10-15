/**
 * SystemDashboard.test.ts
 *
 * Verifies that the System Status dashboard:
 *  - Probes /api/v1/health/live for daemon liveness (not connection.healthCheck)
 *  - Derives uptime from /api/v1/health/ready response
 *  - Marks KMS as 'ok' when client.kms.listKeys() succeeds
 *  - Marks Scheduler as 'ok' when client.scheduler.listJobs() succeeds
 *  - Marks Daemon as 'down' when the liveness probe fails
 *  - Marks KMS as 'ok' when listKeys returns 401 (up but auth required)
 *  - Populates Agent Daemons count from fleetStore
 *  - Counts only LIVE sessions on the Active Sessions tile (go-quicbaas#850)
 *  - Renders clickable stat tiles
 *  - Shows "All systems operational" banner when all services are ok
 *  - Shows named problem subsystems in degraded/down banner
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Hoisted mocks — must be defined before any imports that reference the mocks.
// ---------------------------------------------------------------------------

const {
  mockFetch,
  mockListKeys,
  mockListJobs,
  mockHealthCheck,
  mockSealStatus,
  mockGetAddress,
  mockListWorkflows,
} = vi.hoisted(() => ({
  mockFetch: vi.fn(),
  mockListKeys: vi.fn(),
  mockListJobs: vi.fn(),
  mockHealthCheck: vi.fn(),
  mockSealStatus: vi.fn(),
  mockGetAddress: vi.fn().mockReturnValue('https://localhost:18080'),
  mockListWorkflows: vi.fn().mockResolvedValue({ workflows: [] }),
}));

// Mock fetch globally so liveness/readiness probes are captured.
vi.stubGlobal('fetch', mockFetch);

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    connection: {
      getAddress: mockGetAddress,
    },
    orchestrator: {
      healthCheck: mockHealthCheck,
      listWorkflows: mockListWorkflows,
    },
    kms: {
      listKeys: mockListKeys,
    },
    scheduler: {
      listJobs: mockListJobs,
    },
    seal: {
      status: mockSealStatus,
    },
  }),
}));

// Use hoisted references so the mock factory closure can reference them.
const { fleetStoreMock } = vi.hoisted(() => ({
  fleetStoreMock: {
    nodes: [] as Array<{ id: string; status: string }>,
    get onlineNodes() {
      return (Array.isArray(this.nodes) ? this.nodes : []).filter(
        (a) => a.status === 'running' || a.status === 'idle',
      );
    },
    nodesLoading: false,
  },
}));

vi.mock('../src/stores/fleetStore.svelte.ts', () => ({
  fleetStore: fleetStoreMock,
}));

const { sessionStoreMock } = vi.hoisted(() => ({
  sessionStoreMock: {
    sessions: [] as Array<{ session_id: string; status?: string; state?: string }>,
    sessionsLoading: false,
    activeSession: null,
    events: [],
    historyExhausted: false,
  },
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: sessionStoreMock,
}));

const { orchestratorStoreMock } = vi.hoisted(() => ({
  orchestratorStoreMock: {
    jobs: [] as Array<{ state: string }>,
    fleetNodes: [] as unknown[],
    sessions: [] as unknown[],
    activeTenantID: 'default',
    health: null,
    selectedJob: null,
    jobsLoading: false,
    fleetNodesLoading: false,
    sessionsLoading: false,
    tasksLoading: false,
    tasks: [],
    events: [],
    backends: [],
  },
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: orchestratorStoreMock,
}));

const { attentionStoreMock } = vi.hoisted(() => ({
  attentionStoreMock: {
    items: [] as unknown[],
    total: 0,
    facets: {} as Record<string, Record<string, number>>,
    loading: false,
    resolvingID: null,
    oldestAgeInstant: undefined,
    visibleAlerts: [] as unknown[],
  },
}));

vi.mock('../src/stores/attentionStore.svelte.ts', () => ({
  attentionStore: attentionStoreMock,
  refreshAttention: vi.fn(),
}));

const { connectionStoreMock } = vi.hoisted(() => ({
  connectionStoreMock: {
    connected: true,
    address: 'https://localhost:18080',
    transport: 'rest',
    error: '',
  },
}));

vi.mock('../src/stores/connection.svelte.ts', () => ({
  connectionStore: connectionStoreMock,
}));

vi.mock('../src/stores/navigation.svelte.ts', () => ({
  navigate: vi.fn(),
  navStore: { activeView: 'dashboard', sidebarCollapsed: false },
}));

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function livenessOk() {
  return Promise.resolve(
    new Response(JSON.stringify({ status: 'healthy', uptime: '5m30s' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  );
}

function readinessOk() {
  return Promise.resolve(
    new Response(JSON.stringify({ status: 'healthy', uptime: '5m30s' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  );
}

function livenessFail() {
  return Promise.resolve(
    new Response('Service Unavailable', { status: 503 }),
  );
}

// ---------------------------------------------------------------------------
// Import component AFTER all mocks are set up.
// ---------------------------------------------------------------------------

import SystemDashboard from '../src/views/SystemDashboard.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// Typed reference to the mocked getSDKClient so we can reset it in beforeEach.
const mockGetSDKClient = vi.mocked(getSDKClient);

/** Rebuild the SDK client mock object. Call this in every beforeEach after
 * vi.clearAllMocks() to restore the mockResolvedValue. */
function resetSDKMock(): void {
  mockGetSDKClient.mockResolvedValue({
    connection: { getAddress: mockGetAddress },
    orchestrator: { healthCheck: mockHealthCheck, listWorkflows: mockListWorkflows },
    kms: { listKeys: mockListKeys },
    scheduler: { listJobs: mockListJobs },
    seal: { status: mockSealStatus },
  } as never);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SystemDashboard — daemon liveness wiring', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetSDKMock();
    mockGetAddress.mockReturnValue('https://localhost:18080');

    // Default: all systems healthy
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/health/live')) return livenessOk();
      if (url.includes('/health/ready')) return readinessOk();
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    mockHealthCheck.mockResolvedValue({ timestamp: Date.now(), total_sessions: 0, healthy: 0 });
    mockListKeys.mockResolvedValue({ keys: [] });
    mockListJobs.mockResolvedValue({ jobs: [], total_count: 0 });
    mockSealStatus.mockResolvedValue({ sealed: false, version: '1.2.3' });

    fleetStoreMock.nodes = [];
    sessionStoreMock.sessions = [];
    orchestratorStoreMock.jobs = [];
    connectionStoreMock.connected = true;
  });

  it('renders the "System Status" page heading', () => {
    render(SystemDashboard);
    // The PageHeader title is "System Status" (the canonical title in
    // page-header-consistency.test and the dashboard_layout e2e specs); the
    // "Overview" label is the sidebar section / breadcrumb, not the page title.
    expect(screen.getByText('System Status')).toBeInTheDocument();
  });

  it('probes /api/v1/health/live for daemon status', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/health/live'),
        expect.any(Object),
      );
    });
  });

  it('probes /api/v1/health/ready for uptime', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/health/ready'),
        expect.any(Object),
      );
    });
  });

  it('shows "All systems operational" when all probes succeed', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      expect(screen.getByText('All systems operational')).toBeInTheDocument();
    });
  });

  it('shows uptime value from readiness endpoint', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      // The dashboard's formatUptime() expands compact Go duration form
      // ("5m30s") into a friendlier spaced form ("5m 30s") and renders it
      // in both the stat tile and the Connection card.
      const matches = screen.getAllByText('5m 30s');
      expect(matches.length).toBeGreaterThanOrEqual(1);
    });
  });

  it('marks Daemon as down when liveness probe fails', async () => {
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/health/live')) return livenessFail();
      if (url.includes('/health/ready')) return readinessOk();
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });

    render(SystemDashboard);
    await waitFor(() => {
      // The status badge for Daemon should be 'down'
      const downBadges = screen.getAllByText('down');
      expect(downBadges.length).toBeGreaterThan(0);
    });
  });

  it('marks KMS as ok when listKeys returns 401 (auth required)', async () => {
    const authErr = Object.assign(new Error('Unauthorized'), { status: 401 });
    mockListKeys.mockRejectedValue(authErr);

    render(SystemDashboard);
    // KMS should still be 'ok' since 401 means the service is up
    await waitFor(() => {
      expect(mockListKeys).toHaveBeenCalled();
    });
    // After the call resolves, KMS row should show 'ok'
    await waitFor(() => {
      const okBadges = screen.getAllByText('ok');
      expect(okBadges.length).toBeGreaterThan(0);
    });
  });

  it('marks Scheduler as ok when listJobs succeeds', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      expect(mockListJobs).toHaveBeenCalledWith({ limit: 1 });
    });
    await waitFor(() => {
      const okBadges = screen.getAllByText('ok');
      expect(okBadges.length).toBeGreaterThan(0);
    });
  });
});

describe('SystemDashboard — resource counts', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetSDKMock();
    mockGetAddress.mockReturnValue('https://localhost:18080');
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/health/live')) return livenessOk();
      if (url.includes('/health/ready')) return readinessOk();
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    mockHealthCheck.mockResolvedValue({ timestamp: Date.now(), total_sessions: 0, healthy: 0 });
    mockListKeys.mockResolvedValue({ keys: [] });
    mockListJobs.mockResolvedValue({ jobs: [], total_count: 0 });
    mockSealStatus.mockRejectedValue(new Error('not unsealed'));
    connectionStoreMock.connected = true;
  });

  it('shows correct agent count from fleetStore', async () => {
    fleetStoreMock.nodes = [
      { id: 'a1', status: 'running' },
      { id: 'a2', status: 'idle' },
    ];

    render(SystemDashboard);
    await waitFor(() => {
      // Total agents = 2, label shows on the Agent Daemons tile
      expect(screen.getByText('2 online')).toBeInTheDocument();
    });
  });

  it('counts only live sessions on the Active Sessions tile', async () => {
    // Two defects guarded at once. go-quicbaas#850: the tile said "Active
    // Sessions" and rendered the LENGTH of a list that holds closed sessions
    // too, so an operator with one live session and 54 closed ones was told 54.
    // go-quicbaas#864: the replacement counted `status`, which is what the WAILS
    // binding calls the lifecycle field — so in the browser, where the REST wire
    // sends `state`, the tile read 0.
    //
    // These records carry `state` because that is what a browser session really
    // has (pinned by sdk/typescript/test/listSessions.test.ts).
    sessionStoreMock.sessions = [
      { session_id: 's1', state: 'active' },
      { session_id: 's2', state: 'closed' },
      { session_id: 's3', state: 'closed' },
      { session_id: 's4', state: 'idle' },
      { session_id: 's5', state: 'error' },
    ];

    render(SystemDashboard);
    const tile = await screen.findByTestId('stat-sessions');
    await waitFor(() => {
      expect(within(tile).getByText('Active Sessions')).toBeInTheDocument();
      // active + idle. closed and error are terminal.
      expect(within(tile).getByText('2')).toBeInTheDocument();
      // The total is still reported — as a total.
      expect(within(tile).getByText('5 total')).toBeInTheDocument();
    });
    // And never as the headline value.
    expect(within(tile).queryByText('5')).not.toBeInTheDocument();
  });

  it('reports zero live sessions when every session is closed', async () => {
    sessionStoreMock.sessions = [
      { session_id: 's1', state: 'closed' },
      { session_id: 's2', state: 'closed' },
    ];

    render(SystemDashboard);
    const tile = await screen.findByTestId('stat-sessions');
    await waitFor(() => {
      expect(within(tile).getByText('0')).toBeInTheDocument();
      expect(within(tile).getByText('2 total')).toBeInTheDocument();
    });
  });

  it('counts live sessions on the desktop\'s record shape too', async () => {
    // The Wails binding renames the lifecycle field to `status`. The same tile
    // has to be right on both platforms, which is why the counter reads the
    // value rather than naming a field (go-quicbaas#864).
    sessionStoreMock.sessions = [
      { session_id: 's1', status: 'active' },
      { session_id: 's2', status: 'closed' },
      { session_id: 's3', status: 'resumable' },
    ];

    render(SystemDashboard);
    const tile = await screen.findByTestId('stat-sessions');
    await waitFor(() => {
      expect(within(tile).getByText('2')).toBeInTheDocument();
      expect(within(tile).getByText('3 total')).toBeInTheDocument();
    });
  });

  it('shows 0 for all counts when stores are empty', async () => {
    fleetStoreMock.nodes = [];
    sessionStoreMock.sessions = [];
    orchestratorStoreMock.jobs = [];

    render(SystemDashboard);
    await waitFor(() => {
      const zeros = screen.getAllByText('0');
      expect(zeros.length).toBeGreaterThanOrEqual(2);
    });
  });

  it('shows active job count in Jobs description', async () => {
    orchestratorStoreMock.jobs = [
      { state: 'executing' },
      { state: 'completed' },
      { state: 'scheduling' },
    ];

    render(SystemDashboard);
    await waitFor(() => {
      expect(screen.getByText('2 active')).toBeInTheDocument();
    });
  });
});

describe('SystemDashboard — banner states', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetSDKMock();
    mockGetAddress.mockReturnValue('https://localhost:18080');
    mockHealthCheck.mockResolvedValue({ timestamp: Date.now() });
    mockListKeys.mockResolvedValue({ keys: [] });
    mockListJobs.mockResolvedValue({ jobs: [], total_count: 0 });
    mockSealStatus.mockRejectedValue(new Error('sealed'));
    connectionStoreMock.connected = true;
  });

  it('shows specific subsystem name in degraded banner', async () => {
    // Daemon is up but orchestrator is down
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/health/live')) return livenessOk();
      if (url.includes('/health/ready')) return readinessOk();
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    mockHealthCheck.mockRejectedValue(new Error('orchestrator not reachable'));

    render(SystemDashboard);
    await waitFor(() => {
      // Should name "Software Factory" in the banner, not "one or more services"
      const bannerText = screen.getByRole('status').textContent ?? '';
      expect(bannerText).toContain('Software Factory');
    });
  });

  it('shows "Daemon unreachable" when not connected', async () => {
    connectionStoreMock.connected = false;
    mockFetch.mockRejectedValue(new Error('network error'));

    render(SystemDashboard);
    await waitFor(() => {
      expect(screen.getByText('Daemon unreachable')).toBeInTheDocument();
    });
    connectionStoreMock.connected = true;
  });
});

describe('SystemDashboard — connection card', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetSDKMock();
    mockGetAddress.mockReturnValue('https://localhost:18080');
    // Must restore fetch implementation after clearAllMocks wipes it.
    mockFetch.mockImplementation((url: string) => {
      if (typeof url === 'string' && url.includes('/health/live')) return livenessOk();
      if (typeof url === 'string' && url.includes('/health/ready')) return readinessOk();
      if (typeof url === 'string' && url.includes('/api/v1/version')) {
        return Promise.resolve(
          new Response(JSON.stringify({ version: '2.0.0', build_time: '2026-04-26', git_commit: 'abcdef', go_version: 'go1.26.1' }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      return Promise.reject(new Error(`unexpected fetch: ${String(url)}`));
    });
    mockHealthCheck.mockResolvedValue({ timestamp: Date.now() });
    mockListKeys.mockResolvedValue({ keys: [] });
    mockListJobs.mockResolvedValue({ jobs: [], total_count: 0 });
    mockSealStatus.mockResolvedValue({ sealed: false, version: '2.0.0' });
    connectionStoreMock.connected = true;
    connectionStoreMock.address = 'https://localhost:18080';
    connectionStoreMock.transport = 'rest';
  });

  it('displays the transport as "rest"', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      expect(screen.getAllByText('rest').length).toBeGreaterThan(0);
    });
  });

  it('displays the version badge from the /api/v1/version endpoint', async () => {
    render(SystemDashboard);
    await waitFor(() => {
      // Version appears in both the banner badge and the connection card badge.
      const versionTexts = screen.getAllByText(/v2\.0\.0/);
      expect(versionTexts.length).toBeGreaterThanOrEqual(1);
    });
  });
});

// ---------------------------------------------------------------------------
// #85 — Overview dashboard: quick stats, recent activity, quick actions
// ---------------------------------------------------------------------------

describe('SystemDashboard — #85 overview dashboard sections', () => {
  function makeFetch() {
    return (url: string) => {
      if (url.includes('/health/live')) {
        return Promise.resolve(
          new Response(JSON.stringify({ status: 'healthy' }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      if (url.includes('/health/ready')) {
        return Promise.resolve(
          new Response(JSON.stringify({ status: 'healthy', uptime: '1m0s' }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      if (url.includes('/api/v1/cluster/status')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({ node_id: 1, total_nodes: 1, shards: [], healthy: true, timestamp: new Date().toISOString() }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        );
      }
      // Version endpoint — silently return 404 so tests focus on stat values.
      return Promise.resolve(new Response('', { status: 404 }));
    };
  }

  beforeEach(() => {
    vi.clearAllMocks();
    resetSDKMock();
    mockGetAddress.mockReturnValue('https://localhost:18080');
    mockFetch.mockImplementation(makeFetch());
    mockHealthCheck.mockResolvedValue({ timestamp: Date.now() });
    mockListKeys.mockResolvedValue({ keys: [] });
    mockListJobs.mockResolvedValue({ jobs: [], total_count: 0 });
    mockSealStatus.mockRejectedValue(new Error('sealed'));
    connectionStoreMock.connected = true;

    // One agent
    fleetStoreMock.nodes = [{ id: 'a1', status: 'running' }];

    // Two sessions
    orchestratorStoreMock.sessions = [
      {
        session_id: 'sess-aaa',
        project_id: 'proj-1',
        backend: 'ollama',
        state: 'active',
        created_at: 1700000100,
        last_activity: 1700000200,
      },
      {
        session_id: 'sess-bbb',
        project_id: 'proj-1',
        backend: 'claude',
        state: 'idle',
        created_at: 1700000000,
        last_activity: 1700000100,
      },
    ];
    // The "Active Sessions" stat reads its count from sessionStore (the recent
    // list reads orchestratorStore); seed both so the holistic dashboard is
    // internally consistent.
    sessionStoreMock.sessions = [{ session_id: 'sess-aaa' }, { session_id: 'sess-bbb' }];

    // One workflow
    mockListWorkflows.mockResolvedValue({ workflows: [{ name: 'dev-sprint' }] });

    // Three jobs
    orchestratorStoreMock.jobs = [
      { job_id: 'job-1', state: 'completed', description: 'build tests', started_at: 1700000300, updated_at: 1700000400 },
      { job_id: 'job-2', state: 'executing', description: 'lint check', started_at: 1700000200, updated_at: 1700000300 },
      { job_id: 'job-3', state: 'failed', description: 'deploy', started_at: 1700000100, updated_at: 1700000200 },
    ];
  });

  it('renders the Agents stat card with count 1', async () => {
    render(SystemDashboard);
    const tile = await waitFor(() => {
      const el = document.querySelector('[data-testid="stat-agents"]');
      if (!el) throw new Error('stat-agents tile not found');
      return el;
    });
    expect(tile.textContent).toContain('1');
  });

  it('renders the Active Sessions stat card with count 2', async () => {
    render(SystemDashboard);
    const tile = await waitFor(() => {
      const el = document.querySelector('[data-testid="stat-sessions"]');
      if (!el) throw new Error('stat-sessions tile not found');
      return el;
    });
    expect(tile.textContent).toContain('2');
  });

  it('renders the Workflows stat card with count 1', async () => {
    render(SystemDashboard);
    const tile = await waitFor(() => {
      const el = document.querySelector('[data-testid="stat-workflows"]');
      if (!el) throw new Error('stat-workflows tile not found');
      return el;
    });
    // Workflow count is fetched asynchronously — wait for it to populate.
    await waitFor(() => {
      const txt = document.querySelector('[data-testid="stat-workflows"]')?.textContent ?? '';
      if (!txt.includes('1')) throw new Error(`expected 1 workflow, got: ${txt}`);
    }, { timeout: 2000 });
    expect(tile.textContent).toContain('1');
  });

  it('renders the Total Jobs stat card with count 3', async () => {
    render(SystemDashboard);
    const tile = await waitFor(() => {
      const el = document.querySelector('[data-testid="stat-jobs"]');
      if (!el) throw new Error('stat-jobs tile not found');
      return el;
    });
    expect(tile.textContent).toContain('3');
  });

  it('renders recent jobs list with 3 rows', async () => {
    render(SystemDashboard);
    const list = await waitFor(() => {
      const el = document.querySelector('[data-testid="recent-jobs-list"]');
      if (!el) throw new Error('recent-jobs-list not found');
      return el;
    });
    expect(list.querySelectorAll('li').length).toBe(3);
  });

  it('renders recent sessions list with 2 rows', async () => {
    render(SystemDashboard);
    const list = await waitFor(() => {
      const el = document.querySelector('[data-testid="recent-sessions-list"]');
      if (!el) throw new Error('recent-sessions-list not found');
      return el;
    });
    expect(list.querySelectorAll('li').length).toBe(2);
  });

  it('renders Start Job quick-action button', async () => {
    render(SystemDashboard);
    const btn = await waitFor(() => {
      const el = document.querySelector('[data-testid="quick-start-job"]');
      if (!el) throw new Error('quick-start-job not found');
      return el;
    });
    expect(btn.textContent).toContain('Start Job');
  });

  it('renders Spawn Session quick-action button', async () => {
    render(SystemDashboard);
    const btn = await waitFor(() => {
      const el = document.querySelector('[data-testid="quick-spawn-session"]');
      if (!el) throw new Error('quick-spawn-session not found');
      return el;
    });
    expect(btn.textContent).toContain('Spawn Session');
  });

  it('renders New Workflow quick-action button', async () => {
    render(SystemDashboard);
    const btn = await waitFor(() => {
      const el = document.querySelector('[data-testid="quick-new-workflow"]');
      if (!el) throw new Error('quick-new-workflow not found');
      return el;
    });
    expect(btn.textContent).toContain('New Workflow');
  });
});

// ---------------------------------------------------------------------------
// go-quicbaas#219 — an authorization denial is not an outage.
//
// On the burn-in node the operator (tenant `system`) got
//   403 ... tenant-only permission denied to system tenant
// from GET /api/v1/orchestrator/health, because go-qrdb tags
// orchestrator:health ScopeTenantOnly. The dashboard folded every rejection
// into 'down', so the tile was permanently red for a running orchestrator —
// indistinguishable from a real outage.
//
// These cases are asymmetric on purpose: the same catch block is driven with a
// denial, with a 5xx, and with a transport failure, and each must produce a
// DIFFERENT tile. A test that only asserted the 403 case would still pass if
// the code mapped every failure to 'not permitted' — the mirror image of the
// bug.
// ---------------------------------------------------------------------------

/** An SDK APIError-shaped rejection: a numeric `status` off an Error. */
function apiError(status: number, message: string): Error {
  return Object.assign(new Error(message), { status });
}

/** What the burn-in node actually returned to the system-tenant admin. */
function tenantOnlyDenial(): Error {
  return apiError(
    403,
    'qrdb/auth/rbac: permission denied: admin cannot health on orchestrator: ' +
      'tenant-only permission denied to system tenant',
  );
}

describe('SystemDashboard — orchestrator probe failure classification (#219)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    resetSDKMock();
    mockGetAddress.mockReturnValue('https://localhost:18080');

    // Everything except the orchestrator probe is healthy, so the orchestrator
    // tile is the only thing under test.
    mockFetch.mockImplementation((url: string) => {
      if (url.includes('/health/live')) return livenessOk();
      if (url.includes('/health/ready')) return readinessOk();
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    });
    mockListKeys.mockResolvedValue({ keys: [] });
    mockListJobs.mockResolvedValue({ jobs: [], total_count: 0 });
    mockSealStatus.mockResolvedValue({ sealed: false, version: '1.2.3' });

    fleetStoreMock.nodes = [];
    sessionStoreMock.sessions = [];
    orchestratorStoreMock.jobs = [];
    connectionStoreMock.connected = true;
  });

  it('renders a 403 denial as "not permitted", never as "down"', async () => {
    mockHealthCheck.mockRejectedValue(tenantOnlyDenial());

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('not permitted').length).toBeGreaterThan(0);
    });
    // The whole point of the issue: no red "down" tile for a running service.
    expect(screen.queryAllByText('down')).toHaveLength(0);
  });

  it('renders a 401 denial as "not permitted" too', async () => {
    mockHealthCheck.mockRejectedValue(apiError(401, 'Unauthorized'));

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('not permitted').length).toBeGreaterThan(0);
    });
    expect(screen.queryAllByText('down')).toHaveLength(0);
  });

  it('renders a desktop (Wails) 403 denial as "not permitted" (#236)', async () => {
    // The Wails transport rejects with the SDK's own APIError once
    // wails.ts normalizes the Go formatter's payload, so the desktop build
    // classifies a denial exactly like the REST build.
    mockHealthCheck.mockRejectedValue(new APIError(403, 'permission denied'));

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('not permitted').length).toBeGreaterThan(0);
    });
    expect(screen.queryAllByText('down')).toHaveLength(0);
  });

  it('marks BOTH Software Factory and Storage not permitted on a denial', async () => {
    // Storage health is derived from the orchestrator payload, so a denial
    // leaves it equally unobserved — it must not claim a state either.
    mockHealthCheck.mockRejectedValue(tenantOnlyDenial());

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('not permitted')).toHaveLength(2);
    });
    expect(screen.getByText('Software Factory')).toBeInTheDocument();
    expect(screen.getByText('Storage (PebbleDB)')).toBeInTheDocument();
  });

  it('still renders a 500 as "down"', async () => {
    mockHealthCheck.mockRejectedValue(apiError(500, 'Internal Server Error'));

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('down').length).toBeGreaterThan(0);
    });
    expect(screen.queryAllByText('not permitted')).toHaveLength(0);
  });

  it('still renders a transport failure (no HTTP status) as "down"', async () => {
    // fetch rejects with a TypeError carrying no status when the connection is
    // refused or TLS fails. That IS an outage and must stay red.
    mockHealthCheck.mockRejectedValue(new TypeError('Failed to fetch'));

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('down').length).toBeGreaterThan(0);
    });
    expect(screen.queryAllByText('not permitted')).toHaveLength(0);
  });

  it('does not raise the "Degraded" banner for a denial', async () => {
    mockHealthCheck.mockRejectedValue(tenantOnlyDenial());

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('not permitted').length).toBeGreaterThan(0);
    });
    expect(screen.queryByText('Degraded')).not.toBeInTheDocument();
    // ...and the banner is not painted with the error colour.
    const banner = document.querySelector('.alert[role="status"]');
    expect(banner?.classList.contains('alert-error')).toBe(false);
  });

  it('raises the "Degraded" banner naming Software Factory for a 500', async () => {
    // The control for the case above: a genuine outage still escalates.
    mockHealthCheck.mockRejectedValue(apiError(500, 'Internal Server Error'));

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getByText('Degraded')).toBeInTheDocument();
    });
    const banner = document.querySelector('.alert[role="status"]');
    expect(banner?.textContent).toContain('Software Factory');
    expect(banner?.classList.contains('alert-error')).toBe(true);
  });

  it('withholds "All systems operational" and names the invisible subsystems', async () => {
    // Every other probe is healthy, so the old rollup would have gone green.
    // It must not: a subsystem behind a denial was never observed, and it
    // could be failing. Green means "observed and healthy", not "no bad news".
    mockHealthCheck.mockRejectedValue(tenantOnlyDenial());

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getByText('Partial visibility')).toBeInTheDocument();
    });
    expect(screen.queryByText('All systems operational')).not.toBeInTheDocument();

    const banner = document.querySelector('.alert[role="status"]');
    expect(banner?.textContent).toContain('Software Factory');
    expect(banner?.textContent).toContain('Storage (PebbleDB)');
    // Neither green nor red: the state is genuinely unknown.
    expect(banner?.classList.contains('alert-success')).toBe(false);
    expect(banner?.classList.contains('alert-error')).toBe(false);
  });

  it('still goes green when nothing is hidden', async () => {
    // The control for the case above: without a denial the banner is
    // unchanged, so the new gate cannot be satisfied by never going green.
    mockHealthCheck.mockResolvedValue({ timestamp: Date.now(), total_sessions: 0, healthy: 0 });

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getByText('All systems operational')).toBeInTheDocument();
    });
    expect(screen.queryByText('Partial visibility')).not.toBeInTheDocument();
  });

  it('treats a denial-shaped message with no status code as "down"', async () => {
    // The classification contract is the numeric status, never the prose. An
    // error carrying qrdb's exact denial text but no status never reached an
    // HTTP response, so it is an outage and must stay red — a regression that
    // string-matched the message would turn this neutral, which is the
    // dangerous direction.
    mockHealthCheck.mockRejectedValue(
      new Error(
        'qrdb/auth/rbac: permission denied: admin cannot health on orchestrator: ' +
          'tenant-only permission denied to system tenant (403)',
      ),
    );

    render(SystemDashboard);

    await waitFor(() => {
      expect(screen.getAllByText('down').length).toBeGreaterThan(0);
    });
    expect(screen.queryAllByText('not permitted')).toHaveLength(0);
  });
});
