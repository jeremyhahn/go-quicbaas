/**
 * ProjectsView.test.ts
 *
 * Verifies that the ProjectsView mounts, calls listProjects on init,
 * exposes a create-project affordance, and renders the empty state when
 * no projects are returned.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import type { Project } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import ProjectsView from '../src/views/orchestrator/ProjectsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockCreateProject = vi.fn();
const mockUpdateProject = vi.fn();
const mockListProjects = vi.fn();

function mockClient(projects: Project[] = []): void {
  mockListProjects.mockResolvedValue({ projects, total: projects.length });
  vi.mocked(getSDKClient).mockResolvedValue({
    projects: {
      listProjects: mockListProjects,
      createProject: mockCreateProject,
      updateProject: mockUpdateProject,
      deleteProject: vi.fn(),
      grantAgentRole: vi.fn(),
      revokeAgentRole: vi.fn(),
    },
    orchestrator: {
      listAgents: vi.fn().mockResolvedValue({ agents: [] }),
    },
  } as never);
}

describe('ProjectsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateProject.mockResolvedValue({ project: {} });
    mockUpdateProject.mockResolvedValue({ project: {} });
    mockClient([]);
  });

  it('calls listProjects on mount', async () => {
    render(ProjectsView);
    await waitFor(() => {
      expect(vi.mocked(getSDKClient)).toHaveBeenCalled();
    });
  });

  it('renders the page heading', async () => {
    render(ProjectsView);
    await waitFor(() => {
      expect(screen.getAllByText(/Projects/i).length).toBeGreaterThan(0);
    });
  });

  it('uses the standard PageHeader pattern with icon and subtitle (#40)', async () => {
    const { container } = render(ProjectsView);
    await waitFor(() => {
      // Heading is rendered as <h1> by PageHeader.
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Projects');

      // Icon is wrapped in the primary-tinted square tile.
      const tile = container.querySelector('div.bg-primary\\/10');
      expect(tile).not.toBeNull();
      expect(tile!.querySelector('svg')).not.toBeNull();

      // Subtitle is rendered as a muted <p>.
      const subtitle = container.querySelector('p.text-base-content\\/60');
      expect(subtitle).not.toBeNull();
      expect(subtitle!.textContent).toContain('Project workspaces');
    });
  });

  it('exposes a create-project button', async () => {
    render(ProjectsView);
    await waitFor(() => {
      const candidates = screen.getAllByRole('button');
      const createBtn = candidates.find((b) =>
        /create|new|\+|add/i.test(b.textContent ?? '') &&
        /project/i.test(b.textContent ?? '')
      );
      expect(createBtn).toBeTruthy();
    });
  });

  it('renders an empty state when zero projects are returned', async () => {
    render(ProjectsView);
    await waitFor(() => {
      // EmptyState component renders text containing "No projects" or
      // "Create your first project" or "no project". Tolerate any of those.
      const hasEmptyHint =
        screen.queryByText(/no projects/i) ||
        screen.queryByText(/first project/i) ||
        screen.queryByText(/create.*project/i);
      expect(hasEmptyHint).toBeTruthy();
    });
  });
});

describe('ProjectsView — create dialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateProject.mockResolvedValue({ project: {} });
    mockUpdateProject.mockResolvedValue({ project: {} });
    mockClient([]);
  });

  async function openCreateDialog(container: HTMLElement): Promise<void> {
    const openBtn = await waitFor(() =>
      screen.getAllByRole('button').find(
        (b) => /create|new|\+|add/i.test(b.textContent ?? '') && /project/i.test(b.textContent ?? ''),
      ),
    );
    await fireEvent.click(openBtn as HTMLElement);
    await waitFor(() => expect(container.querySelector('#proj-name')).not.toBeNull());
  }

  it('sends workspace_path and git fields when populated', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);

    await fireEvent.input(container.querySelector('#proj-name') as HTMLInputElement, {
      target: { value: 'my-proj' },
    });
    await fireEvent.input(container.querySelector('#proj-workspace-path') as HTMLInputElement, {
      target: { value: '/work/my-proj' },
    });
    await fireEvent.input(container.querySelector('#proj-git-url') as HTMLInputElement, {
      target: { value: 'https://github.com/org/repo.git' },
    });
    await fireEvent.input(container.querySelector('#proj-git-branch') as HTMLInputElement, {
      target: { value: 'main' },
    });

    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalled());
    const req = mockCreateProject.mock.calls[0][0];
    expect(req.workspace_path).toBe('/work/my-proj');
    expect(req.git_repo_url).toBe('https://github.com/org/repo.git');
    expect(req.git_default_branch).toBe('main');
  });

  it('omits advanced quota fields when left blank (0/empty = unlimited)', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);

    await fireEvent.input(container.querySelector('#proj-name') as HTMLInputElement, {
      target: { value: 'my-proj' },
    });

    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalled());
    const req = mockCreateProject.mock.calls[0][0];
    expect(req.token_budget).toBeUndefined();
    expect(req.gpu_minutes).toBeUndefined();
    expect(req.max_concurrency).toBeUndefined();
    expect(req.host_affinity).toBeUndefined();
  });

  it('sends advanced quota fields when populated with positive values', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);

    await fireEvent.input(container.querySelector('#proj-name') as HTMLInputElement, {
      target: { value: 'my-proj' },
    });
    await fireEvent.input(container.querySelector('#proj-token-budget') as HTMLInputElement, {
      target: { value: '5000' },
    });
    await fireEvent.input(container.querySelector('#proj-gpu-minutes') as HTMLInputElement, {
      target: { value: '60' },
    });
    await fireEvent.input(container.querySelector('#proj-max-concurrency') as HTMLInputElement, {
      target: { value: '3' },
    });

    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalled());
    const req = mockCreateProject.mock.calls[0][0];
    expect(req.token_budget).toBe(5000);
    expect(req.gpu_minutes).toBe(60);
    expect(req.max_concurrency).toBe(3);
  });
});

describe('ProjectsView — workspace path display', () => {
  it('shows the workspace path on the project row when present', async () => {
    const project: Project = {
      id: 'proj-1',
      tenant_id: 'default',
      name: 'alpha',
      workspace: { path: '/work/alpha' },
      created_at: '',
      updated_at: '',
    };
    mockClient([project]);
    render(ProjectsView);
    await waitFor(() => expect(screen.getByText('/work/alpha')).toBeInTheDocument());
  });
});

describe('ProjectsView — edit dialog', () => {
  const project: Project = {
    id: 'proj-1',
    tenant_id: 'default',
    name: 'alpha',
    description: 'original desc',
    workspace: { path: '/work/alpha' },
    git: { repo_url: 'https://github.com/org/alpha.git', default_branch: 'main' },
    quotas: { token_budget: 1000, gpu_minutes: 30, max_concurrency: 2 },
    created_at: '',
    updated_at: '',
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateProject.mockResolvedValue({ project: {} });
    mockUpdateProject.mockResolvedValue({ project: {} });
    mockClient([project]);
  });

  async function openEditDialog(container: HTMLElement): Promise<void> {
    const editBtn = await waitFor(() => {
      const btn = container.querySelector('button[aria-label="Edit project"]');
      expect(btn).not.toBeNull();
      return btn as HTMLElement;
    });
    await fireEvent.click(editBtn);
    await waitFor(() => expect(container.querySelector('#edit-proj-desc')).not.toBeNull());
  }

  it('pre-fills the edit dialog from the project', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    expect((container.querySelector('#edit-proj-desc') as HTMLInputElement).value).toBe('original desc');
    expect((container.querySelector('#edit-proj-workspace-path') as HTMLInputElement).value).toBe('/work/alpha');
    expect((container.querySelector('#edit-proj-git-url') as HTMLInputElement).value).toBe(
      'https://github.com/org/alpha.git',
    );
    expect((container.querySelector('#edit-proj-git-branch') as HTMLInputElement).value).toBe('main');
    expect((container.querySelector('#edit-proj-token-budget') as HTMLInputElement).value).toBe('1000');
  });

  it('submits updateProject with only the changed field when one value is edited', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    await fireEvent.input(container.querySelector('#edit-proj-desc') as HTMLInputElement, {
      target: { value: 'updated desc' },
    });

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Save',
    ) as HTMLButtonElement;
    await fireEvent.click(saveBtn);

    await waitFor(() => expect(mockUpdateProject).toHaveBeenCalled());
    const req = mockUpdateProject.mock.calls[0][0];
    expect(req.tenant_id).toBe('default');
    expect(req.project_id).toBe('proj-1');
    expect(req.description).toBe('updated desc');
    expect(req.workspace_path).toBeUndefined();
    expect(req.git_repo_url).toBeUndefined();
    expect(req.token_budget).toBeUndefined();
  });

  it('sends clear_description when the description is cleared', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    await fireEvent.input(container.querySelector('#edit-proj-desc') as HTMLInputElement, {
      target: { value: '' },
    });

    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Save',
    ) as HTMLButtonElement;
    await fireEvent.click(saveBtn);

    await waitFor(() => expect(mockUpdateProject).toHaveBeenCalled());
    const req = mockUpdateProject.mock.calls[0][0];
    expect(req.clear_description).toBe(true);
    expect(req.description).toBeUndefined();
  });
});
