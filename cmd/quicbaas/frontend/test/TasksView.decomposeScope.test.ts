/**
 * TasksView.decomposeScope — a decomposition is bound to the tenant and login
 * session it was made in. Adoption writes into that tenant, never the one the
 * header shows now, and a scope change before or during adoption stops it, so
 * tenant A's task text is never created under tenant B.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { mockDecomposeTask, mockCreateTask, mockShowToast, scope } = vi.hoisted(() => ({
  mockDecomposeTask: vi.fn(),
  mockCreateTask: vi.fn(),
  mockShowToast: vi.fn(),
  scope: { tenant: 'tenant-a', generation: 1, listeners: [] as Array<() => void> },
}));

const task = {
  id: 'task-a1',
  subject: 'Split the importer',
  description: 'Split the importer into stages',
  team_name: 'eng',
  status: 'pending',
  assigned_to: '',
  spawned_job_id: '',
  auto_spawn: false,
  created_at: 0,
  updated_at: 0,
};

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    orchestrator: {
      listTasksPage: async () => ({ items: [task], total: 1, limit: 25, offset: 0 }),
      listJobsPage: async () => ({ items: [], total: 0, limit: 0, offset: 0 }),
      decomposeTask: mockDecomposeTask,
      createTask: mockCreateTask,
      claimTask: vi.fn(),
      completeTask: vi.fn(),
      listFleetNodesPage: async () => ({ items: [], total: 0, limit: 25, offset: 0 }),
      listBackends: async () => [],
    },
    projects: {
      listProjectsPage: async () => ({ items: [], total: 0, limit: 25, offset: 0 }),
    },
  })),
}));

vi.mock('../src/stores/auth.svelte.ts', () => ({
  sessionGeneration: () => scope.generation,
  onSessionScopeChange: (fn: () => void) => {
    scope.listeners.push(fn);
    return () => { scope.listeners = scope.listeners.filter((l) => l !== fn); };
  },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: mockShowToast }));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routerStore: { name: 'tasks', params: {}, hash: '#/tasks' },
  routeTaskDetail: vi.fn(),
  routeTasks: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    get tasks() { return [task]; },
    get jobs() { return []; },
    get tasksLoading() { return false; },
    get tasksError() { return null; },
    get activeTenantID() { return scope.tenant; },
  },
  loadJobs: vi.fn().mockResolvedValue(undefined),
  refresh: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/components/orchestrator/CreateTaskDialog.svelte', () => ({ default: vi.fn() }));

import TasksView from '../src/views/orchestrator/TasksView.svelte';
import { forgetReferenceOptions } from '../src/stores/referenceOptions.svelte.ts';

async function decompose(): Promise<void> {
  render(TasksView);
  await fireEvent.click(
    await waitFor(() => screen.getByRole('button', { name: 'Decompose task Split the importer' })),
  );
}

describe('TasksView — a decomposition keeps the scope it was made in', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    scope.tenant = 'tenant-a';
    scope.generation = 1;
    scope.listeners = [];
    window.location.hash = '#/tasks';
    forgetReferenceOptions();
    mockDecomposeTask.mockResolvedValue({
      work_items: [{ title: 'Stage one' }, { title: 'Stage two' }, { title: 'Stage three' }],
      timestamp: 1,
    });
    mockCreateTask.mockResolvedValue({});
  });

  it('creates the work items in the tenant it decomposed in', async () => {
    await decompose();
    await waitFor(() => screen.getByTestId('decomposition'));
    await fireEvent.click(screen.getByRole('button', { name: 'Create 3 tasks' }));
    await waitFor(() => expect(mockCreateTask).toHaveBeenCalledTimes(3));
    for (const [req] of mockCreateTask.mock.calls) expect(req.tenant_id).toBe('tenant-a');
  });

  it('creates nothing when the tenant changes before adoption', async () => {
    await decompose();
    await waitFor(() => screen.getByTestId('decomposition'));
    scope.tenant = 'tenant-b';
    await fireEvent.click(screen.getByRole('button', { name: 'Create 3 tasks' }));
    await waitFor(() => expect(screen.queryByTestId('decomposition')).toBeNull());
    expect(mockCreateTask).not.toHaveBeenCalled();
  });

  it('stops a batch when the tenant changes during adoption', async () => {
    mockCreateTask.mockImplementationOnce(async () => {
      scope.tenant = 'tenant-b';
      return {};
    });
    await decompose();
    await waitFor(() => screen.getByTestId('decomposition'));
    await fireEvent.click(screen.getByRole('button', { name: 'Create 3 tasks' }));
    await waitFor(() => expect(mockShowToast).toHaveBeenCalledWith(
      'error', 'Stopped after 1 task: the tenant or session changed',
    ));
    expect(mockCreateTask).toHaveBeenCalledTimes(1);
    expect(mockCreateTask.mock.calls[0][0].tenant_id).toBe('tenant-a');
    expect(screen.queryByTestId('decomposition')).toBeNull();
  });

  it('drops a decompose answer that arrives after the tenant changed', async () => {
    let answer: (v: unknown) => void = () => {};
    mockDecomposeTask.mockImplementationOnce(() => new Promise((resolve) => { answer = resolve; }));
    await decompose();
    await waitFor(() => expect(mockDecomposeTask).toHaveBeenCalled());
    expect(mockDecomposeTask.mock.calls[0][0].tenant_id).toBe('tenant-a');
    scope.tenant = 'tenant-b';
    answer({ work_items: [{ title: 'Stage one' }], timestamp: 1 });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Decompose task Split the importer' })).not.toBeDisabled());
    expect(screen.queryByTestId('decomposition')).toBeNull();
    expect(mockShowToast).not.toHaveBeenCalledWith('success', expect.anything());
  });

  it('discards the panel when the login session changes', async () => {
    await decompose();
    await waitFor(() => screen.getByTestId('decomposition'));
    scope.generation = 2;
    for (const fn of scope.listeners) fn();
    await waitFor(() => expect(screen.queryByTestId('decomposition')).toBeNull());
    expect(mockCreateTask).not.toHaveBeenCalled();
  });
});
