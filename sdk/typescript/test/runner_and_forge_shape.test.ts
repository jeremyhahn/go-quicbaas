// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Shape audit for the two capability endpoints the Projects and Job Detail
// views read, plus the project and binding fields the Projects dialogs gained.
//
// Each assertion is written against the Go JSON tags rather than against what
// the SDK happens to do today:
//
//   pkg/api/rest/handlers_orchestrator.go
//     OrchestratorJobStateSpecResponse   state / label / session_backed
//     OrchestratorJobRunnerResponse      name / states
//     OrchestratorListRunnersResponse    runners / count
//   pkg/api/rest/handlers_sdlc.go
//     SDLCForgesResponse                 forges / count
//   pkg/api/rest/handlers_project.go
//     ProjectRESTResponse                autonomous / context_repos / git_*
//   pkg/sdlc/binding.go
//     ProjectBinding                     forge / strategy / worker /
//                                        verification / review /
//                                        max_review_rounds / ranking

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { JobRunnerInfo, Project, CreateProjectRequest } from '../src/index';
import type { ProjectBinding } from '../src/sdlc';

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// =============================================================================
// GET /api/v1/orchestrator/runners
// =============================================================================

describe('RestOrchestratorClient.listRunners', () => {
  const serverBody = {
    runners: [
      {
        name: 'workflow',
        states: [
          { state: 'initializing', label: 'Initializing', session_backed: false },
          { state: 'executing', label: 'Executing', session_backed: true },
        ],
      },
      {
        name: 'sdlc',
        states: [{ state: 'reviewing', label: 'Reviewing', session_backed: false }],
      },
    ],
    count: 2,
  };

  it('requests the runners endpoint', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    await new RestClient('http://localhost').orchestrator.listRunners();
    const requested = new URL(fetchMock.mock.calls[0][0] as string);
    expect(`${requested.origin}${requested.pathname}`).toBe('http://localhost/api/v1/orchestrator/runners');
    expect(requested.searchParams.get('limit')).toBe('100');
    expect(requested.searchParams.has('offset')).toBe(false);
  });

  it('unwraps the {runners:[...]} envelope into a bare array', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const runners = await new RestClient('http://localhost').orchestrator.listRunners();
    expect(Array.isArray(runners)).toBe(true);
    expect(runners).toHaveLength(2);
    expect(runners.map((r) => r.name)).toEqual(['workflow', 'sdlc']);
  });

  it('carries every declared state field the Go DTO names', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const runners: JobRunnerInfo[] = await new RestClient('http://localhost').orchestrator.listRunners();
    const workflow = runners[0]!;
    expect(workflow.states[0]).toEqual({
      state: 'initializing',
      label: 'Initializing',
      session_backed: false,
    });
    // session_backed is serialized even when false — the UI reads it.
    expect(workflow.states[0]!.session_backed).toBe(false);
    expect(workflow.states[1]!.session_backed).toBe(true);
  });

  it('returns an empty array when the server sends {runners: null}', async () => {
    fetchMock.mockResolvedValue(makeResponse({ runners: null, count: 0 }));
    const runners = await new RestClient('http://localhost').orchestrator.listRunners();
    expect(runners).toEqual([]);
  });

  it('accepts a bare array unchanged', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody.runners));
    const runners = await new RestClient('http://localhost').orchestrator.listRunners();
    expect(runners).toHaveLength(2);
  });

  it('throws an APIError carrying the status when the endpoint refuses', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'nope' }, 503));
    await expect(new RestClient('http://localhost').orchestrator.listRunners()).rejects.toMatchObject(
      { status: 503 },
    );
  });
});

// =============================================================================
// GET /api/v1/sdlc/forges
// =============================================================================

describe('RestSdlcClient.forges', () => {
  it('requests the forges endpoint and unwraps the envelope', async () => {
    fetchMock.mockResolvedValue(makeResponse({ forges: ['gitea'], count: 1 }));
    const forges = await new RestClient('http://localhost').sdlc.forges();
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/sdlc/forges');
    expect(forges).toEqual(['gitea']);
  });

  it('returns an empty list when the build registered no adapter', async () => {
    fetchMock.mockResolvedValue(makeResponse({ forges: null, count: 0 }));
    expect(await new RestClient('http://localhost').sdlc.forges()).toEqual([]);
  });

  it('surfaces the 503 that says the module is not running on this node', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({ error: 'the sdlc module is not running on this node' }, 503),
    );
    await expect(new RestClient('http://localhost').sdlc.forges()).rejects.toMatchObject({
      status: 503,
    });
  });
});

// =============================================================================
// The project fields the dialogs gained
// =============================================================================

describe('Project shape — autonomy, context repos and git identity', () => {
  // The server's own JSON for a project carrying every field the dialogs edit.
  const serverProject = {
    id: 'proj-1',
    tenant_id: 'default',
    name: 'go-truststrap',
    default_workflow: 'delivery',
    autonomous: true,
    context_repos: [{ name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' }],
    git: {
      repo_url: 'https://git.example/org/go-truststrap',
      default_branch: 'main',
      author: { name: 'agent', email: 'agent@example.internal' },
      signing: { enabled: true, algorithm: 'ED25519', key: { name: 'k', scope: 'tenant', owner: 'default' } },
      ssh_key: { name: 'deploy-key', scope: 'service', owner: 'ci' },
    },
    quotas: { max_concurrency: 1 },
    created_at: '2026-09-06T00:00:00Z',
    updated_at: '2026-09-06T00:00:00Z',
  };

  it('maps every nested git identity, autonomy and context-repo field', async () => {
    fetchMock.mockResolvedValue(makeResponse({ projects: [serverProject], total: 1 }));
    const resp = await new RestClient('http://localhost').projects.listProjects('default');
    const project: Project = resp.projects[0]!;
    expect(project.autonomous).toBe(true);
    expect(project.context_repos).toEqual([
      { name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' },
    ]);
    expect(project.default_workflow).toBe('delivery');
    expect(project.git?.author?.email).toBe('agent@example.internal');
    expect(project.git?.signing?.key?.scope).toBe('tenant');
    expect(project.git?.ssh_key?.owner).toBe('ci');
  });

  it('sends the flat create keys the REST request struct names', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: serverProject }));
    const req: CreateProjectRequest = {
      tenant_id: 'default',
      name: 'go-truststrap',
      autonomous: true,
      context_repos: [{ name: 'handbook', repo_url: 'https://git.example/org/handbook', ref: 'main' }],
      git_author_name: 'agent',
      git_author_email: 'agent@example.internal',
      git_signing_enabled: true,
      git_signing_key_name: 'k',
      git_ssh_key_name: 'deploy-key',
      default_workflow: 'delivery',
    };
    await new RestClient('http://localhost').projects.createProject(req);
    const sent = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(sent.autonomous).toBe(true);
    expect(sent.context_repos[0].repo_url).toBe('https://git.example/org/handbook');
    expect(sent.git_author_email).toBe('agent@example.internal');
    expect(sent.git_signing_enabled).toBe(true);
    expect(sent.git_ssh_key_name).toBe('deploy-key');
    expect(sent.default_workflow).toBe('delivery');
  });

  it('sends clear_context_repos and clear_default_workflow on an update', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: serverProject }));
    await new RestClient('http://localhost').projects.updateProject({
      tenant_id: 'default',
      project_id: 'proj-1',
      clear_context_repos: true,
      clear_default_workflow: true,
      autonomous: false,
    });
    const sent = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(sent.clear_context_repos).toBe(true);
    expect(sent.clear_default_workflow).toBe(true);
    expect(sent.autonomous).toBe(false);
  });
});

// =============================================================================
// The SDLC binding the Projects edit dialog writes
// =============================================================================

describe('ProjectBinding shape — strategy, worker, verification, review rounds', () => {
  const serverBinding = {
    project_id: 'proj-1',
    forge: {
      type: 'gitea',
      base_url: 'https://git.example/api/v1',
      owner: 'jeremyhahn',
      repo: 'go-truststrap',
      trigger_label: 'task:ready',
      token_secret_ref: 'forge-token',
      ssh_host_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake',
      labels: { in_progress: 'status/working' },
    },
    strategy: 'github-flow',
    worker: { backend: 'claude', model: 'claude-opus-5' },
    verification: ['make build', 'make test'],
    review: {
      required_approvals: 1,
      require_independent_reviewer: true,
      require_green_checks: true,
      checks_settle_window: 90_000_000_000,
      reviewers: { codex: 'gpt' },
    },
    max_review_rounds: 3,
    ranking: { default_priority: 5 },
    created_at: '2026-09-06T00:00:00Z',
    updated_at: '2026-09-06T00:00:00Z',
  };

  it('reads the binding with the module’s own field names', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBinding));
    const binding: ProjectBinding = await new RestClient('http://localhost').sdlc.binding(
      'default',
      'proj-1',
    );
    expect(binding.strategy).toBe('github-flow');
    expect(binding.worker.backend).toBe('claude');
    expect(binding.verification).toEqual(['make build', 'make test']);
    expect(binding.max_review_rounds).toBe(3);
    expect(binding.review.checks_settle_window).toBe(90_000_000_000);
    expect(binding.forge.trigger_label).toBe('task:ready');
    // The pinned forge host key travels under its own name; a binding that
    // dropped it would send an ssh worker at a forge it cannot verify.
    expect(binding.forge.ssh_host_key).toBe(serverBinding.forge.ssh_host_key);
  });

  it('writes the binding back under the same names', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBinding));
    await new RestClient('http://localhost').sdlc.bind('default', 'proj-1', serverBinding);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(
      'http://localhost/api/v1/sdlc/projects/proj-1/binding?tenant_id=default',
    );
    expect((init as RequestInit).method).toBe('PUT');
    const sent = JSON.parse((init as RequestInit).body as string);
    expect(sent.strategy).toBe('github-flow');
    expect(sent.worker).toEqual({ backend: 'claude', model: 'claude-opus-5' });
    expect(sent.max_review_rounds).toBe(3);
    // The binding no longer carries a publication policy or a split
    // branching/merge strategy pair.
    expect(sent.publication).toBeUndefined();
    expect(sent.forge.branching_strategy).toBeUndefined();
    expect(sent.forge.merge_strategy).toBeUndefined();
    expect(sent.forge.ssh_host_key).toBe(serverBinding.forge.ssh_host_key);
  });

  it('reports the 404 that means the project is not bound', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'not found' }, 404));
    await expect(
      new RestClient('http://localhost').sdlc.binding('default', 'proj-1'),
    ).rejects.toMatchObject({ status: 404 });
  });
});
