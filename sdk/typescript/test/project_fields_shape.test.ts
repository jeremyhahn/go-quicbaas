// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Shape audit for the generic Project autonomy fields: `autonomous` and
// `context_repos` are what decide whether the daemon adopts work for a
// project and what the worker clones beside the checkout. A field the SDK
// dropped would read as "the server did not say", which is not what happened.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { ContextRepo, CreateProjectRequest, Project, UpdateProjectRequest } from '../src/projects';

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

const handbook: ContextRepo = {
  name: 'handbook',
  repo_url: 'https://forge.example/org/handbook',
  ref: 'main',
};

// The wire shape as pkg/api/rest/handlers_project.go writes it — every json
// tag, nothing renamed.
const wireProject = {
  id: 'proj-1',
  tenant_id: 'tenant-a',
  name: 'api',
  autonomous: true,
  context_repos: [handbook],
  created_at: '2026-09-06T00:00:00Z',
  updated_at: '2026-09-06T00:00:00Z',
};

// requestBody reads back the JSON body of the recorded fetch call.
function requestBody(call: number): Record<string, unknown> {
  const init = fetchMock.mock.calls[call]?.[1] as RequestInit;
  return JSON.parse(init.body as string) as Record<string, unknown>;
}

describe('RestProjectsClient — autonomy and context repositories', () => {
  it('sends both fields on create and reads them back', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: wireProject }));
    const client = new RestClient('http://localhost');

    const req: CreateProjectRequest = {
      tenant_id: 'tenant-a',
      name: 'api',
      autonomous: true,
      context_repos: [handbook],
    };
    const resp = await client.projects.createProject(req);

    const body = requestBody(0);
    expect(body['autonomous']).toBe(true);
    expect(body['context_repos']).toEqual([handbook]);

    const project: Project = resp.project;
    expect(project.autonomous).toBe(true);
    expect(project.context_repos).toHaveLength(1);
    expect(project.context_repos?.[0]?.name).toBe('handbook');
    expect(project.context_repos?.[0]?.repo_url).toBe('https://forge.example/org/handbook');
    expect(project.context_repos?.[0]?.ref).toBe('main');
  });

  it('keeps both fields through a get', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: wireProject }));
    const client = new RestClient('http://localhost');

    const resp = await client.projects.getProject('tenant-a', 'proj-1');
    expect(resp.project.autonomous).toBe(true);
    expect(resp.project.context_repos).toEqual([handbook]);
  });

  it('omits an unset autonomous on update so the server leaves it alone', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: wireProject }));
    const client = new RestClient('http://localhost');

    const req: UpdateProjectRequest = { tenant_id: 'tenant-a', project_id: 'proj-1', name: 'renamed' };
    await client.projects.updateProject(req);

    const body = requestBody(0);
    expect('autonomous' in body).toBe(false);
    expect('context_repos' in body).toBe(false);
  });

  it('sends an explicit false and a clear on update', async () => {
    const off = { ...wireProject, autonomous: false, context_repos: [] as ContextRepo[] };
    fetchMock.mockResolvedValue(makeResponse({ project: off }));
    const client = new RestClient('http://localhost');

    const req: UpdateProjectRequest = {
      tenant_id: 'tenant-a',
      project_id: 'proj-1',
      autonomous: false,
      clear_context_repos: true,
    };
    const resp = await client.projects.updateProject(req);

    const body = requestBody(0);
    expect(body['autonomous']).toBe(false);
    expect(body['clear_context_repos']).toBe(true);
    expect(resp.project.autonomous).toBe(false);
    expect(resp.project.context_repos).toEqual([]);
  });

  it('leaves the fields absent — not zeroed — when the server omits them', async () => {
    const { autonomous: _a, context_repos: _c, ...bare } = wireProject;
    fetchMock.mockResolvedValue(makeResponse({ project: bare }));
    const client = new RestClient('http://localhost');

    const resp = await client.projects.getProject('tenant-a', 'proj-1');
    expect(resp.project.autonomous).toBeUndefined();
    expect(resp.project.context_repos).toBeUndefined();
  });

  it('keeps both fields through the listProjects envelope', async () => {
    fetchMock.mockResolvedValue(makeResponse({ projects: [wireProject], total: 1 }));
    const client = new RestClient('http://localhost');

    const resp = await client.projects.listProjects('tenant-a');
    expect(resp.projects).toHaveLength(1);
    expect(resp.projects[0]?.autonomous).toBe(true);
    expect(resp.projects[0]?.context_repos?.[0]?.ref).toBe('main');
  });
});

// requestURL reads back the URL of the recorded fetch call.
function requestURL(call: number): string {
  return String(fetchMock.mock.calls[call]?.[0]);
}

describe('RestProjectsClient — the tenant is on the URL, not only in the body', () => {
  // pkg/api/rest/handlers_project.go derives the tenant from the QUERY and
  // refuses before it reads the body, so a verb that omits it is dead on
  // arrival. updateProject omitted it and every save from the SPA's edit
  // dialog came back 400 — the whole project edit flow was broken.
  it('puts tenant_id on the update URL', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: wireProject }));
    const client = new RestClient('http://localhost');

    const req: UpdateProjectRequest = {
      tenant_id: 'tenant-a',
      project_id: 'proj-1',
      git_repo_url: 'ssh://git@forge.example:2222/org/api.git',
    };
    await client.projects.updateProject(req);

    expect(requestURL(0)).toBe('http://localhost/api/v1/projects/proj-1?tenant_id=tenant-a');
    // The body still carries it too; the handler reads both.
    expect(requestBody(0)['tenant_id']).toBe('tenant-a');
  });

  it('escapes a tenant and project that need it', async () => {
    fetchMock.mockResolvedValue(makeResponse({ project: wireProject }));
    const client = new RestClient('http://localhost');

    await client.projects.updateProject({ tenant_id: 'a b/c', project_id: 'p/1' });

    expect(requestURL(0)).toBe('http://localhost/api/v1/projects/p%2F1?tenant_id=a+b%2Fc');
  });

  it('puts tenant_id on every other project verb that takes one', async () => {
    // The sibling verbs already did this; asserting them together is what
    // makes the odd one out visible.
    fetchMock.mockResolvedValue(makeResponse({ project: wireProject }));
    const client = new RestClient('http://localhost');

    await client.projects.getProject('tenant-a', 'proj-1');
    expect(requestURL(0)).toContain('tenant_id=tenant-a');

    fetchMock.mockResolvedValue(makeResponse({ project_id: 'proj-1', deleted: true }));
    await client.projects.deleteProject('tenant-a', 'proj-1');
    expect(requestURL(1)).toContain('tenant_id=tenant-a');
  });
});
