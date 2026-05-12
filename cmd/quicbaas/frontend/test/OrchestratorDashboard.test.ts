/**
 * OrchestratorDashboard.test.ts
 *
 * Regression test for TypeError: jobs.filter is not a function.
 * Verifies that the dashboard renders without error when the store
 * returns undefined/null/non-array values, and shows the correct
 * "No jobs" and "No agents registered" empty states.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';

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
