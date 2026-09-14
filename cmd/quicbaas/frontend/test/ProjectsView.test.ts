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
const mockBinding = vi.fn();
const mockBind = vi.fn();
const mockUnbind = vi.fn();
const mockForges = vi.fn();

/** apiError is what the SDK's REST client throws: a status plus a message. */
function apiError(status: number, message = 'refused'): Error & { status: number } {
  const err = new Error(message) as Error & { status: number };
  err.status = status;
  return err;
}

/**
 * mockClient wires the SDK the view sees. `sdlc` defaults to a module that
 * answers 503 — "not running on this node" — which is the shape a build
 * without the SDLC module presents.
 */
function mockClient(projects: Project[] = [], sdlc: unknown = undefined): void {
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
    sdlc,
  } as never);
}

/** sdlcModule is a mounted SDLC module with the given binding behaviour. */
function sdlcModule(): unknown {
  return { binding: mockBinding, bind: mockBind, unbind: mockUnbind, forges: mockForges };
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
      workspace_path: '/work/alpha',
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
    workspace_path: '/work/alpha',
    git_repo_url: 'https://github.com/org/alpha.git',
    git_default_branch: 'main',
    token_budget: 1000,
    gpu_minutes: 30,
    max_concurrency: 2,
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

// ===========================================================================
// The fields the dialogs gained: git identity and signing, the default
// workflow, the autonomy switch and its context repositories.
// ===========================================================================

describe('ProjectsView — create dialog, git identity and autonomy', () => {
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

  async function submitCreate(container: HTMLElement): Promise<void> {
    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);
  }

  async function setValue(container: HTMLElement, id: string, value: string): Promise<void> {
    await fireEvent.input(container.querySelector(id) as HTMLInputElement, { target: { value } });
  }

  it('sends the commit author, signing policy, ssh key and default workflow', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);

    await setValue(container, '#proj-name', 'go-truststrap');
    await setValue(container, '#proj-git-author-name', 'agent-go-truststrap');
    await setValue(container, '#proj-git-author-email', 'agent@example.internal');
    await fireEvent.click(container.querySelector('#proj-git-signing-enabled') as HTMLInputElement);
    await setValue(container, '#proj-git-signing-key-name', 'commit-signing');
    await fireEvent.change(container.querySelector('#proj-git-signing-key-scope') as HTMLSelectElement, {
      target: { value: 'tenant' },
    });
    await setValue(container, '#proj-git-signing-key-owner', 'default');
    await setValue(container, '#proj-git-ssh-key-name', 'deploy-key');
    await setValue(container, '#proj-default-workflow', 'delivery');

    await submitCreate(container);

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalled());
    const req = mockCreateProject.mock.calls[0][0];
    expect(req.git_author_name).toBe('agent-go-truststrap');
    expect(req.git_author_email).toBe('agent@example.internal');
    expect(req.git_signing_enabled).toBe(true);
    expect(req.git_signing_key_name).toBe('commit-signing');
    expect(req.git_signing_key_scope).toBe('tenant');
    expect(req.git_signing_key_owner).toBe('default');
    expect(req.git_ssh_key_name).toBe('deploy-key');
    expect(req.default_workflow).toBe('delivery');
  });

  it('sends the autonomy switch and the context repositories it names', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);

    await setValue(container, '#proj-name', 'go-truststrap');
    await fireEvent.click(container.querySelector('#proj-autonomous') as HTMLInputElement);
    await setValue(container, '#proj-max-concurrency', '1');

    const addBtn = container.querySelector(
      'button[aria-label="Add context repository"]',
    ) as HTMLButtonElement;
    await fireEvent.click(addBtn);
    await waitFor(() =>
      expect(container.querySelector('#proj-context-repo-name-0')).not.toBeNull(),
    );
    await setValue(container, '#proj-context-repo-name-0', 'handbook');
    await setValue(container, '#proj-context-repo-url-0', 'https://git.example/org/handbook');
    await setValue(container, '#proj-context-repo-ref-0', 'main');

    await submitCreate(container);

    await waitFor(() => expect(mockCreateProject).toHaveBeenCalled());
    const req = mockCreateProject.mock.calls[0][0];
    expect(req.autonomous).toBe(true);
    expect(req.max_concurrency).toBe(1);
    expect(req.context_repos).toEqual([
      { name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' },
    ]);
  });

  it('removes a context repository row again', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);
    await setValue(container, '#proj-name', 'p');

    const addBtn = container.querySelector(
      'button[aria-label="Add context repository"]',
    ) as HTMLButtonElement;
    await fireEvent.click(addBtn);
    await waitFor(() =>
      expect(container.querySelector('#proj-context-repo-name-0')).not.toBeNull(),
    );
    await fireEvent.click(
      container.querySelector('button[aria-label="Remove context repository 1"]') as HTMLButtonElement,
    );
    await waitFor(() => expect(container.querySelector('#proj-context-repo-name-0')).toBeNull());

    await submitCreate(container);
    await waitFor(() => expect(mockCreateProject).toHaveBeenCalled());
    expect(mockCreateProject.mock.calls[0][0].context_repos).toBeUndefined();
  });

  it('refuses signing with no key before the round trip, and does not call the server', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);

    await setValue(container, '#proj-name', 'p');
    await fireEvent.click(container.querySelector('#proj-git-signing-enabled') as HTMLInputElement);
    await submitCreate(container);

    await waitFor(() =>
      expect(screen.getByText(/never degrade to unsigned/i)).toBeInTheDocument(),
    );
    expect(mockCreateProject).not.toHaveBeenCalled();
  });

  it('refuses a half-filled context repository before the round trip', async () => {
    const { container } = render(ProjectsView);
    await openCreateDialog(container);
    await setValue(container, '#proj-name', 'p');

    await fireEvent.click(
      container.querySelector('button[aria-label="Add context repository"]') as HTMLButtonElement,
    );
    await waitFor(() =>
      expect(container.querySelector('#proj-context-repo-name-0')).not.toBeNull(),
    );
    await setValue(container, '#proj-context-repo-name-0', 'handbook');
    await submitCreate(container);

    await waitFor(() => expect(screen.getByText(/needs a repository URL/i)).toBeInTheDocument());
    expect(mockCreateProject).not.toHaveBeenCalled();
  });
});

describe('ProjectsView — edit dialog, git identity and autonomy', () => {
  const project: Project = {
    id: 'proj-1',
    tenant_id: 'default',
    name: 'go-truststrap',
    autonomous: true,
    default_workflow: 'delivery',
    context_repos: [{ name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' }],
    git_repo_url: 'https://git.example/org/go-truststrap',
    git_default_branch: 'main',
    git_author_name: 'agent-go-truststrap',
    git_author_email: 'agent@example.internal',
    git_signing_enabled: true,
    git_signing_algorithm: 'ED25519',
    git_signing_key_name: 'commit-signing',
    git_signing_key_scope: 'tenant',
    git_ssh_key_name: 'deploy-key',
    max_concurrency: 1,
    created_at: '',
    updated_at: '',
  };

  beforeEach(() => {
    vi.clearAllMocks();
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

  async function save(container: HTMLElement): Promise<void> {
    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Save',
    ) as HTMLButtonElement;
    await fireEvent.click(saveBtn);
  }

  it('pre-fills every new field from the project record', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    const value = (id: string) => (container.querySelector(id) as HTMLInputElement).value;
    expect(value('#edit-proj-git-author-name')).toBe('agent-go-truststrap');
    expect(value('#edit-proj-git-author-email')).toBe('agent@example.internal');
    expect((container.querySelector('#edit-proj-git-signing-enabled') as HTMLInputElement).checked).toBe(true);
    expect(value('#edit-proj-git-signing-key-name')).toBe('commit-signing');
    expect(value('#edit-proj-git-signing-key-scope')).toBe('tenant');
    expect(value('#edit-proj-git-ssh-key-name')).toBe('deploy-key');
    expect(value('#edit-proj-default-workflow')).toBe('delivery');
    expect((container.querySelector('#edit-proj-autonomous') as HTMLInputElement).checked).toBe(true);
    expect(value('#edit-proj-max-concurrency')).toBe('1');
    expect(value('#edit-proj-context-repo-name-0')).toBe('handbook');
    expect(value('#edit-proj-context-repo-url-0')).toBe('https://git.example/org/handbook');
    expect(value('#edit-proj-context-repo-ref-0')).toBe('main');
  });

  it('tells the operator that emptying a box does not clear the stored value', async () => {
    // The dialog used to invite leaving the SSH key empty to fall back to the
    // signing key — true on create, false on edit, because update is
    // presence-based and has no clear for these fields. An operator could
    // clear a stale key, save without error, and keep using the old one.
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    const note = container.querySelector('[role="note"]');
    expect(note).not.toBeNull();
    expect(note!.textContent).toMatch(/leaves the stored value unchanged/i);
    expect(container.textContent).not.toMatch(/Leave empty and\s+the signing key doubles/i);
  });

  it('sends only the git identity field that was edited', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    await fireEvent.input(container.querySelector('#edit-proj-git-author-email') as HTMLInputElement, {
      target: { value: 'other@example.internal' },
    });
    await save(container);

    await waitFor(() => expect(mockUpdateProject).toHaveBeenCalled());
    const req = mockUpdateProject.mock.calls[0][0];
    expect(req.git_author_email).toBe('other@example.internal');
    expect(req.git_author_name).toBeUndefined();
    expect(req.autonomous).toBeUndefined();
    expect(req.context_repos).toBeUndefined();
  });

  it('sends the autonomy switch when it is turned off', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    await fireEvent.click(container.querySelector('#edit-proj-autonomous') as HTMLInputElement);
    await save(container);

    await waitFor(() => expect(mockUpdateProject).toHaveBeenCalled());
    expect(mockUpdateProject.mock.calls[0][0].autonomous).toBe(false);
  });

  it('clears the context repositories when the last row is removed', async () => {
    const { container } = render(ProjectsView);
    await openEditDialog(container);

    await fireEvent.click(
      container.querySelector('button[aria-label="Remove context repository 1"]') as HTMLButtonElement,
    );
    await save(container);

    await waitFor(() => expect(mockUpdateProject).toHaveBeenCalled());
    expect(mockUpdateProject.mock.calls[0][0].clear_context_repos).toBe(true);
  });

  it('shows the autonomous badge on the project row', async () => {
    render(ProjectsView);
    await waitFor(() => expect(screen.getByText('autonomous')).toBeInTheDocument());
  });
});

// ===========================================================================
// The SDLC binding section: shown only where the module answers, and edited
// in place through the module's own endpoints.
// ===========================================================================

describe('ProjectsView — SDLC binding section', () => {
  const project: Project = {
    id: 'proj-1',
    tenant_id: 'default',
    name: 'go-truststrap',
    created_at: '',
    updated_at: '',
  };

  const binding = {
    project_id: 'proj-1',
    forge: {
      type: 'gitea',
      base_url: 'https://git.example/api/v1',
      owner: 'jeremyhahn',
      repo: 'go-truststrap',
      trigger_label: 'task:ready',
      token_secret_ref: 'forge-token',
    },
    strategy: 'github-flow',
    worker: { backend: 'claude', model: 'claude-opus-5' },
    verification: ['make build'],
    review: {
      required_approvals: 1,
      require_independent_reviewer: true,
      require_green_checks: true,
    },
    max_review_rounds: 3,
    ranking: {},
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdateProject.mockResolvedValue({ project: {} });
    mockBind.mockResolvedValue(binding);
    mockUnbind.mockResolvedValue(undefined);
    mockForges.mockResolvedValue(['gitea']);
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

  async function save(container: HTMLElement): Promise<void> {
    const saveBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Save',
    ) as HTMLButtonElement;
    await fireEvent.click(saveBtn);
  }

  it('is absent when the SDLC client is not on this build at all', async () => {
    mockClient([project]);
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    expect(container.querySelector('#edit-proj-sdlc-enabled')).toBeNull();
  });

  it('is absent when the module answers 503 — not running on this node', async () => {
    mockBinding.mockRejectedValue(apiError(503, 'the sdlc module is not running on this node'));
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() => expect(mockBinding).toHaveBeenCalled());
    expect(container.querySelector('#edit-proj-sdlc-enabled')).toBeNull();
    expect(mockForges).not.toHaveBeenCalled();
  });

  it('is present but unbound when the module answers 404 — this project has no binding', async () => {
    mockBinding.mockRejectedValue(apiError(404, 'not found'));
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-enabled')).not.toBeNull(),
    );
    expect((container.querySelector('#edit-proj-sdlc-enabled') as HTMLInputElement).checked).toBe(false);
    // The fields only appear once the operator opts in.
    expect(container.querySelector('#edit-proj-sdlc-forge-type')).toBeNull();
  });

  it('pre-fills the section from an existing binding', async () => {
    mockBinding.mockResolvedValue(binding);
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-forge-type')).not.toBeNull(),
    );

    const value = (id: string) => (container.querySelector(id) as HTMLInputElement).value;
    expect(value('#edit-proj-sdlc-forge-type')).toBe('gitea');
    expect(value('#edit-proj-sdlc-strategy')).toBe('github-flow');
    expect(value('#edit-proj-sdlc-base-url')).toBe('https://git.example/api/v1');
    expect(value('#edit-proj-sdlc-owner')).toBe('jeremyhahn');
    expect(value('#edit-proj-sdlc-repo')).toBe('go-truststrap');
    expect(value('#edit-proj-sdlc-trigger-label')).toBe('task:ready');
    expect(value('#edit-proj-sdlc-token-secret-ref')).toBe('forge-token');
    expect(value('#edit-proj-sdlc-worker-backend')).toBe('claude');
    expect(value('#edit-proj-sdlc-worker-model')).toBe('claude-opus-5');
    expect(value('#edit-proj-sdlc-verification-0')).toBe('make build');
    expect(value('#edit-proj-sdlc-required-approvals')).toBe('1');
    expect(value('#edit-proj-sdlc-max-review-rounds')).toBe('3');
  });

  it('shows an SCM whose adapter this build lacks DISABLED with the reason, not hidden', async () => {
    mockBinding.mockResolvedValue(binding);
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-forge-type')).not.toBeNull(),
    );

    const select = container.querySelector('#edit-proj-sdlc-forge-type') as HTMLSelectElement;
    const options = Array.from(select.querySelectorAll('option'));
    expect(options.map((o) => o.value)).toEqual(['gitea', 'github', 'gitlab']);

    const gitea = options.find((o) => o.value === 'gitea') as HTMLOptionElement;
    const github = options.find((o) => o.value === 'github') as HTMLOptionElement;
    expect(gitea.disabled).toBe(false);
    expect(github.disabled).toBe(true);
    expect(github.textContent).toMatch(/no adapter in this build/i);
    expect(github.title).toMatch(/no github adapter is registered/i);
  });

  it('leaves every SCM selectable when the capability probe could not answer', async () => {
    mockBinding.mockResolvedValue(binding);
    mockForges.mockRejectedValue(apiError(404, 'no such route'));
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-forge-type')).not.toBeNull(),
    );
    const options = Array.from(
      (container.querySelector('#edit-proj-sdlc-forge-type') as HTMLSelectElement).querySelectorAll(
        'option',
      ),
    );
    expect(options.every((o) => !o.disabled)).toBe(true);
  });

  it('saves the binding through the module after the project update', async () => {
    mockBinding.mockResolvedValue(binding);
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-trigger-label')).not.toBeNull(),
    );

    await fireEvent.input(container.querySelector('#edit-proj-sdlc-trigger-label') as HTMLInputElement, {
      target: { value: 'task:go' },
    });
    await save(container);

    await waitFor(() => expect(mockBind).toHaveBeenCalled());
    expect(mockUpdateProject).toHaveBeenCalled();
    const [tenantID, projectID, sent] = mockBind.mock.calls[0];
    expect(tenantID).toBe('default');
    expect(projectID).toBe('proj-1');
    expect(sent.forge.trigger_label).toBe('task:go');
    expect(sent.strategy).toBe('github-flow');
    expect(sent.worker.backend).toBe('claude');
  });

  it('unbinds when a bound project is switched off', async () => {
    mockBinding.mockResolvedValue(binding);
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-enabled')).not.toBeNull(),
    );

    await fireEvent.click(container.querySelector('#edit-proj-sdlc-enabled') as HTMLInputElement);
    await save(container);

    await waitFor(() => expect(mockUnbind).toHaveBeenCalledWith('default', 'proj-1'));
    expect(mockBind).not.toHaveBeenCalled();
  });

  it('refuses an incomplete binding before the round trip', async () => {
    mockBinding.mockResolvedValue(binding);
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(container.querySelector('#edit-proj-sdlc-worker-backend')).not.toBeNull(),
    );

    await fireEvent.input(container.querySelector('#edit-proj-sdlc-worker-backend') as HTMLInputElement, {
      target: { value: '' },
    });
    await save(container);

    await waitFor(() =>
      expect(screen.getByText(/worker backend is required/i)).toBeInTheDocument(),
    );
    expect(mockBind).not.toHaveBeenCalled();
    expect(mockUpdateProject).not.toHaveBeenCalled();
  });

  it('reports a probe failure that is neither 404 nor 503 rather than swallowing it', async () => {
    mockBinding.mockRejectedValue(apiError(500, 'store unavailable'));
    mockClient([project], sdlcModule());
    const { container } = render(ProjectsView);
    await openEditDialog(container);
    await waitFor(() =>
      expect(screen.getByText(/failed to read the sdlc binding/i)).toBeInTheDocument(),
    );
  });
});
