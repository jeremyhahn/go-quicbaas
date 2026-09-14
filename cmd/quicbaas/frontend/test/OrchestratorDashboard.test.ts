/**
 * OrchestratorDashboard.test.ts
 *
 * Regression test for TypeError: jobs.filter is not a function.
 * Verifies that the dashboard renders without error when the store
 * returns undefined/null/non-array values, and shows the correct
 * "No jobs" and "No agents registered" empty states.
 */

import { beforeEach, describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — all values must be hoistable (no top-level const references in factory)
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({}),
}));

// Use vi.hoisted so the store object is available in the vi.mock factory scope.
const { orchestratorStoreMock } = vi.hoisted(() => ({
  orchestratorStoreMock: {
    jobs: [] as unknown,
    tasks: [] as unknown,
    agents: [] as unknown,
    sessions: [] as unknown,
    events: [] as unknown,
    activeTenantID: 'default',
    jobsLoading: false,
    agentsLoading: false,
    sessionsLoading: false,
    tasksLoading: false,
    health: null,
    selectedJob: null,
  },
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: orchestratorStoreMock,
}));

// Stub out ECharts so canvas calls don't throw in jsdom.
vi.mock('echarts/core', () => ({
  use: vi.fn(),
  init: vi.fn(() => ({
    setOption: vi.fn(),
    resize: vi.fn(),
    dispose: vi.fn(),
  })),
}));
vi.mock('echarts/charts', () => ({ PieChart: {}, BarChart: {} }));
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }));
vi.mock('echarts/components', () => ({
  TitleComponent: {},
  TooltipComponent: {},
  LegendComponent: {},
  GridComponent: {},
}));

import OrchestratorDashboard from '../src/views/orchestrator/OrchestratorDashboard.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('OrchestratorDashboard — array guard regression', () => {
  it('renders without throwing when jobs is undefined', () => {
    orchestratorStoreMock.jobs = undefined;
    orchestratorStoreMock.tasks = undefined;
    orchestratorStoreMock.agents = undefined;
    orchestratorStoreMock.events = undefined;
    orchestratorStoreMock.sessions = undefined;

    expect(() => render(OrchestratorDashboard)).not.toThrow();
  });

  it('renders without throwing when jobs is null', () => {
    orchestratorStoreMock.jobs = null;
    orchestratorStoreMock.tasks = null;
    orchestratorStoreMock.agents = null;
    orchestratorStoreMock.events = null;
    orchestratorStoreMock.sessions = null;

    expect(() => render(OrchestratorDashboard)).not.toThrow();
  });

  it('shows "No jobs" empty state when jobs is empty array', () => {
    orchestratorStoreMock.jobs = [];
    orchestratorStoreMock.tasks = [];
    orchestratorStoreMock.agents = [];
    orchestratorStoreMock.events = [];
    orchestratorStoreMock.sessions = [];

    render(OrchestratorDashboard);
    expect(screen.getByText('No jobs')).toBeTruthy();
  });

  it('shows "No agents registered" empty state when agents is empty', () => {
    orchestratorStoreMock.jobs = [];
    orchestratorStoreMock.agents = [];
    orchestratorStoreMock.tasks = [];
    orchestratorStoreMock.events = [];
    orchestratorStoreMock.sessions = [];

    render(OrchestratorDashboard);
    expect(screen.getByText('No agents registered')).toBeTruthy();
  });

  it('shows 0 active jobs stat when jobs is undefined', () => {
    orchestratorStoreMock.jobs = undefined;
    orchestratorStoreMock.tasks = undefined;
    orchestratorStoreMock.agents = undefined;
    orchestratorStoreMock.events = undefined;
    orchestratorStoreMock.sessions = undefined;

    render(OrchestratorDashboard);
    // StatCard renders the numeric value — 0 active jobs.
    const activeJobsValues = screen.getAllByText('0');
    expect(activeJobsValues.length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// Counter honesty (go-quicbaas#850)
// ---------------------------------------------------------------------------

/** The daisyUI stat card whose title is `label`. */
function statCard(label: string): HTMLElement {
  const card = screen.getByText(label).closest('.stat');
  if (card === null) throw new Error(`no stat card titled ${label}`);
  return card as HTMLElement;
}

describe('OrchestratorDashboard — a card that claims live state counts live state', () => {
  beforeEach(() => {
    orchestratorStoreMock.jobs = [];
    orchestratorStoreMock.tasks = [];
    orchestratorStoreMock.agents = [];
    orchestratorStoreMock.events = [];
    orchestratorStoreMock.sessions = [];
  });

  it('counts live sessions, not every session record', () => {
    // The shape the owner saw: one live session among many closed ones, and a
    // card that reported the length of the list.
    orchestratorStoreMock.sessions = [
      { session_id: 's1', state: 'active' },
      { session_id: 's2', state: 'closed' },
      { session_id: 's3', state: 'closed' },
      { session_id: 's4', state: 'closed' },
      { session_id: 's5', state: 'error' },
    ];

    render(OrchestratorDashboard);

    const card = statCard('Active Sessions');
    expect(within(card).getByText('1')).toBeTruthy();
    // The total is still reported — as a total, in the qualifying line.
    expect(within(card).getByText('5 total')).toBeTruthy();
    // And never as the headline value.
    expect(within(card).queryByText('5')).toBeNull();
  });

  it('counts every state a session can still be prompted in', () => {
    orchestratorStoreMock.sessions = [
      { session_id: 's1', state: 'active' },
      { session_id: 's2', state: 'idle' },
      { session_id: 's3', state: 'resumable' },
      { session_id: 's4', state: 'closed' },
    ];

    render(OrchestratorDashboard);

    const card = statCard('Active Sessions');
    expect(within(card).getByText('3')).toBeTruthy();
    expect(within(card).getByText('4 total')).toBeTruthy();
  });

  it('reports zero live sessions when every record is closed', () => {
    orchestratorStoreMock.sessions = [
      { session_id: 's1', state: 'closed' },
      { session_id: 's2', state: 'closed' },
    ];

    render(OrchestratorDashboard);

    const card = statCard('Active Sessions');
    expect(within(card).getByText('0')).toBeTruthy();
    expect(within(card).getByText('2 total')).toBeTruthy();
  });

  it('counts live sessions on the desktop\'s record shape too', () => {
    // The Wails binding calls the lifecycle field `status`; the REST wire calls
    // it `state`. One tile, both platforms (go-quicbaas#864).
    orchestratorStoreMock.sessions = [
      { session_id: 's1', status: 'active' },
      { session_id: 's2', status: 'closed' },
      { session_id: 's3', status: 'idle' },
    ];

    render(OrchestratorDashboard);

    const card = statCard('Active Sessions');
    expect(within(card).getByText('2')).toBeTruthy();
    expect(within(card).getByText('3 total')).toBeTruthy();
  });

  it('survives session records that carry no lifecycle field at all', () => {
    orchestratorStoreMock.sessions = [
      { session_id: 's1' },
      { session_id: 's2', state: 'active' },
    ];

    expect(() => render(OrchestratorDashboard)).not.toThrow();
    const card = statCard('Active Sessions');
    expect(within(card).getByText('1')).toBeTruthy();
  });
});

describe('OrchestratorDashboard — Recent Events renders the history the jobs carry', () => {
  beforeEach(() => {
    orchestratorStoreMock.jobs = [];
    orchestratorStoreMock.tasks = [];
    orchestratorStoreMock.agents = [];
    orchestratorStoreMock.events = [];
    orchestratorStoreMock.sessions = [];
  });

  it('renders the events in the log instead of the empty state', () => {
    // What the store holds after a job poll seeds it from the job records.
    orchestratorStoreMock.events = [
      {
        job_id: 'job-1',
        type: 'sdlc:agent_turn',
        state: '',
        message: 'attempt=1 round=0 elapsed=6m3s',
        iteration: 0,
        timestamp: 1_700_000_000_000_000_000,
      },
    ];

    render(OrchestratorDashboard);

    expect(screen.getByText('sdlc:agent_turn')).toBeTruthy();
    expect(screen.getByText('attempt=1 round=0 elapsed=6m3s')).toBeTruthy();
    expect(screen.queryByText('No events yet')).toBeNull();
  });
});
