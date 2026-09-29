// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Shape audit for the project storage tier (go-quicbaas#913), written against
// the Go JSON tags of pkg/api/rest/handlers_project.go:
//
//   CreateProjectRESTRequest / UpdateProjectRESTRequest / ProjectRESTResponse
//     workspace_tier / workspace_tmpfs_mb / container_memory_mb   (FLAT)
//
// The #797 failure was a read path hydrating from a key the API never sent, so
// the read assertion is made against a verbatim server body.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { CreateProjectRequest, Project } from '../src/index';

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

// A verbatim GET /api/v1/projects/{id} body for an ephemeral-tier project.
const serverBody = {
  project: {
    id: 'proj-1',
    tenant_id: 'default',
    name: 'api',
    workspace_tier: 'ephemeral',
    workspace_tmpfs_mb: 4096,
    container_memory_mb: 8192,
    created_at: '2026-09-24T00:00:00Z',
    updated_at: '2026-09-24T00:00:00Z',
  },
  timestamp: '2026-09-24T00:00:00Z',
};

describe('Project shape — storage tier', () => {
  it('reads the flat storage keys the server sends', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const resp = await new RestClient('http://localhost').projects.getProject('default', 'proj-1');
    const project: Project = resp.project;
    expect(project.workspace_tier).toBe('ephemeral');
    expect(project.workspace_tmpfs_mb).toBe(4096);
    expect(project.container_memory_mb).toBe(8192);
  });

  it('sends the flat create keys the REST request struct names', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const req: CreateProjectRequest = {
      tenant_id: 'default',
      name: 'api',
      workspace_tier: 'ephemeral',
      workspace_tmpfs_mb: 4096,
      container_memory_mb: 8192,
    };
    await new RestClient('http://localhost').projects.createProject(req);
    const sent = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(sent.workspace_tier).toBe('ephemeral');
    expect(sent.workspace_tmpfs_mb).toBe(4096);
    expect(sent.container_memory_mb).toBe(8192);
    expect(sent.storage).toBeUndefined();
  });

  it('sends an explicit zero on an update and omits what it leaves alone', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    await new RestClient('http://localhost').projects.updateProject({
      tenant_id: 'default',
      project_id: 'proj-1',
      workspace_tmpfs_mb: 0,
      container_memory_mb: 0,
    });
    const sent = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(sent.workspace_tmpfs_mb).toBe(0);
    expect(sent.container_memory_mb).toBe(0);
    expect('workspace_tier' in sent).toBe(false);
  });
});
