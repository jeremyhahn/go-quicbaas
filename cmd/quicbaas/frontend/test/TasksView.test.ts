/**
 * TasksView.test.ts
 *
 * Regression guard for the TasksView component.
 *
 * Asserts:
 *   - The "Spawned Job" column header is rendered in table layout.
 *   - The Auto-spawned / Queued / All filter buttons are present.
 *   - The empty-state renders when there are no tasks.
 *   - Rows render when tasks are present, including a spawned-job link.
 *
 * These tests fail if someone removes the Spawned Job column or the filter
 * controls without updating the empty-state or the assertions here.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks — declared before any src/ imports.
// ---------------------------------------------------------------------------

const { mockDecomposeTask, mockClaimTask, mockCompleteTask, mockShowToast } = vi.hoisted(() => ({
  mockDecomposeTask: vi.fn(),
  mockClaimTask: vi.fn(),
  mockCompleteTask: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      decomposeTask: mockDecomposeTask,
      claimTask: mockClaimTask,
      completeTask: mockCompleteTask,
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

// Provide a controllable orchestratorStore so tests can inject tasks.
let storeTasks: unknown[] = [];
let storeLoading = false;

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    get tasks() { return storeTasks; },
    get tasksLoading() { return storeLoading; },
    activeTenantID: 'default',
  },
}));

// CreateTaskDialog imports from the SDK — stub it so the module graph stays
// simple. PageHeader, StatusBadge, and EmptyState have no SDK dependencies
// and are rendered normally so snippet content (filter buttons, empty state)
// appears in the DOM.
vi.mock('../src/components/orchestrator/CreateTaskDialog.svelte', () => ({
  default: vi.fn(),
}));

// Stub lucide icon components. jsdom has no SVG layout engine; rendering real
// icons causes no errors but we silence them to keep test output clean.
vi.mock('lucide-svelte', () => ({
  LayoutList: vi.fn(),
  Columns3: vi.fn(),
  Plus: vi.fn(),
  ClipboardList: vi.fn(),
  UserCheck: vi.fn(),
  CheckCircle: vi.fn(),
  Zap: vi.fn(),
}));

import TasksView from '../src/views/orchestrator/TasksView.svelte';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeTask(overrides: Record<string, unknown> = {}) {
  return {
    id: 'task-001',
    subject: 'Test task',
    description: '',
    team_name: 'eng',
    status: 'pending',
    assigned_to: '',
    spawned_job_id: '',
    auto_spawn: false,
    created_at: 0,
    updated_at: 0,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TasksView — Spawned Job column', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    storeTasks = [];
    storeLoading = false;
  });

  it('renders the "Spawned Job" column header in table layout', () => {
    render(TasksView);
    expect(screen.getByText('Spawned Job')).toBeInTheDocument();
  });

  it('keeps "Spawned Job" header when the task list is empty', () => {
    storeTasks = [];
    render(TasksView);
    expect(screen.getByText('Spawned Job')).toBeInTheDocument();
  });

  it('keeps "Spawned Job" header when tasks are present', () => {
    storeTasks = [makeTask()];
    render(TasksView);
    expect(screen.getByText('Spawned Job')).toBeInTheDocument();
  });

  it('renders a link to the spawned job when spawned_job_id is set', () => {
    storeTasks = [makeTask({ spawned_job_id: 'job-abcdef1234567890' })];
    render(TasksView);
    const link = screen.getByTitle(/View spawned job job-abcdef1234567890/i);
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '#/orchestrator/jobs/job-abcdef1234567890');
  });

  it('shows "no agent matched" for an auto-spawn task without a spawned job', () => {
    storeTasks = [makeTask({ auto_spawn: true, spawned_job_id: '' })];
    render(TasksView);
    expect(screen.getByText(/no agent matched/i)).toBeInTheDocument();
  });
});

describe('TasksView — spawn filter controls', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    storeTasks = [];
    storeLoading = false;
  });

  it('renders the Auto-spawned filter button', () => {
    render(TasksView);
    expect(screen.getByRole('button', { name: /Show auto-spawned tasks/i })).toBeInTheDocument();
  });

  it('renders the Queued filter button', () => {
    render(TasksView);
    expect(screen.getByRole('button', { name: /Show queued tasks/i })).toBeInTheDocument();
  });

  it('renders the All filter button', () => {
    render(TasksView);
    expect(screen.getByRole('button', { name: /Show all tasks/i })).toBeInTheDocument();
  });
});

describe('TasksView — empty state', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    storeTasks = [];
    storeLoading = false;
  });

  it('renders the "No tasks tracked" empty-state when there are no tasks', () => {
    render(TasksView);
    expect(screen.getByText(/No tasks tracked/i)).toBeInTheDocument();
  });
});
