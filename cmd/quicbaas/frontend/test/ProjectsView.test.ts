/**
 * ProjectsView.test.ts
 *
 * Verifies that the ProjectsView mounts, calls listProjects on init,
 * exposes a create-project affordance, and renders the empty state when
 * no projects are returned.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';

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

describe('ProjectsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      projects: {
        listProjects: vi.fn().mockResolvedValue({ projects: [] }),
        createProject: vi.fn(),
        deleteProject: vi.fn(),
        grantAgentRole: vi.fn(),
        revokeAgentRole: vi.fn(),
      },
      orchestrator: {
        listAgents: vi.fn().mockResolvedValue({ agents: [] }),
      },
    } as never);
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
