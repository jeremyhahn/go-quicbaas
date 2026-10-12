// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The MCP transport carries projects over the server's project_* tools
// (pkg/api/mcp/project_tools.go) and sdlc over its sdlc_* tools
// (pkg/api/mcp/sdlc_tools.go). The project tools answer untagged Go structs,
// so the client translates their envelope keys; a regression there hands
// callers `undefined` where REST hands them a project. A request argument a
// tool does not read must be refused, not dropped.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { McpClient } from '../src/mcp';
import { McpError, TransportNotSupportedError } from '../src/errors';
import type { ProjectBinding } from '../src/sdlc';
import { jsonReply, scriptServer, toolCalls, toolText } from './mcp_server';

const endpoint = 'http://node.example:8081/mcp';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// answer scripts the server to return `value` for every tool call.
function answer(value: unknown): void {
  scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, value)));
}

function lastCall(): { name?: string; args?: Record<string, unknown> } {
  const call = toolCalls(fetchMock).at(-1);
  return { name: call?.params?.name, args: call?.params?.arguments };
}

const wireProject = {
  id: 'proj-1',
  tenant_id: 'tenant-a',
  name: 'api',
  autonomous: true,
  context_repos: [{ name: 'handbook', repo_url: 'https://forge.example/org/handbook', ref: 'main' }],
  created_at: '2026-09-06T00:00:00Z',
  updated_at: '2026-09-06T00:00:00Z',
};

describe('McpClient.projects — tools and envelope translation', () => {
  it('creates through project_create with flat args and unwraps Project', async () => {
    answer({ Project: wireProject, Timestamp: '2026-10-12T00:00:00Z' });
    const client = new McpClient(endpoint);

    const resp = await client.projects.createProject({
      tenant_id: 'tenant-a',
      name: 'api',
      autonomous: true,
      context_repos: wireProject.context_repos,
    });

    expect(lastCall().name).toBe('project_create');
    expect(lastCall().args?.['autonomous']).toBe(true);
    expect(lastCall().args?.['context_repos']).toEqual(wireProject.context_repos);
    expect(resp.project).toEqual(wireProject);
  });

  it('gets and updates through their tools and unwraps Project', async () => {
    answer({ Project: wireProject });
    const client = new McpClient(endpoint);

    const got = await client.projects.getProject('tenant-a', 'proj-1');
    expect(lastCall()).toEqual({ name: 'project_get', args: { tenant_id: 'tenant-a', project_id: 'proj-1' } });
    expect(got.project.id).toBe('proj-1');

    const updated = await client.projects.updateProject({ tenant_id: 'tenant-a', project_id: 'proj-1', parked: true });
    expect(lastCall()).toEqual({ name: 'project_update', args: { tenant_id: 'tenant-a', project_id: 'proj-1', parked: true } });
    expect(updated.project.name).toBe('api');
  });

  it('translates the delete, grant and revoke keys to snake_case', async () => {
    const client = new McpClient(endpoint);

    answer({ ProjectID: 'proj-1', Deleted: true, Timestamp: 't0' });
    expect(await client.projects.deleteProject('tenant-a', 'proj-1')).toEqual({ deleted: true });
    expect(lastCall().name).toBe('project_delete');

    answer({ ProjectID: 'proj-1', FleetNodeID: 'node-1', Role: 'worker', Timestamp: 't1' });
    const granted = await client.projects.grantFleetNodeRole({
      tenant_id: 'tenant-a', project_id: 'proj-1', node_id: 'node-1', role: 'worker',
    });
    expect(lastCall()).toEqual({
      name: 'project_grant_fleet_node',
      args: { tenant_id: 'tenant-a', project_id: 'proj-1', node_id: 'node-1', role: 'worker' },
    });
    expect(granted).toEqual({ project_id: 'proj-1', node_id: 'node-1', role: 'worker', timestamp: 't1' });

    answer({ ProjectID: 'proj-1', FleetNodeID: 'node-1', Timestamp: 't2' });
    const revoked = await client.projects.revokeFleetNodeRole({ tenant_id: 'tenant-a', project_id: 'proj-1', node_id: 'node-1' });
    expect(lastCall().name).toBe('project_revoke_fleet_node');
    expect(revoked).toEqual({ project_id: 'proj-1', node_id: 'node-1', timestamp: 't2' });
  });

  it('lists and pages through project_list, adopting the envelope', async () => {
    answer({ projects: [wireProject], total: 7, limit: 1, offset: 2, next_offset: 3, timestamp: 't' });
    const client = new McpClient(endpoint);

    const page = await client.projects.listProjectsPage('tenant-a', { limit: 1, offset: 2, sort: 'name', q: 'ap' });
    expect(lastCall()).toEqual({
      name: 'project_list',
      args: { tenant_id: 'tenant-a', limit: 1, offset: 2, sort: 'name', q: 'ap' },
    });
    expect(page.items).toEqual([wireProject]);
    expect(page.total).toBe(7);
    expect(page.next_offset).toBe(3);

    const all = await client.projects.listProjects('tenant-a');
    expect(lastCall().args).toEqual({ tenant_id: 'tenant-a' });
    expect(all).toEqual({ projects: [wireProject], total: 7 });
  });

  it('surfaces a tool error as McpError naming the tool', async () => {
    scriptServer(fetchMock, (msg) => jsonReply({
      jsonrpc: '2.0',
      id: msg.id,
      result: { content: [{ type: 'text', text: 'project not found' }], isError: true },
    }));
    const client = new McpClient(endpoint);

    const err = await client.projects.getProject('tenant-a', 'missing').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).tool).toBe('project_get');
    expect((err as McpError).message).toContain('project not found');
  });
});

describe('McpClient.projects — arguments the tools do not read', () => {
  const refusals: Array<{ method: string; field: string; call: (c: McpClient) => Promise<unknown> }> = [
    {
      method: 'createProject',
      field: 'metadata',
      call: (c) => c.projects.createProject({ tenant_id: 'tenant-a', name: 'api', metadata: { team: 'core' } }),
    },
    {
      method: 'updateProject',
      field: 'metadata',
      call: (c) => c.projects.updateProject({ tenant_id: 'tenant-a', project_id: 'proj-1', metadata: { team: 'core' } }),
    },
    {
      // On an update {} means "clear the metadata"; dropping it would report a
      // success that changed nothing.
      method: 'updateProject',
      field: 'metadata',
      call: (c) => c.projects.updateProject({ tenant_id: 'tenant-a', project_id: 'proj-1', metadata: {} }),
    },
    {
      method: 'updateProject',
      field: 'clear_description',
      call: (c) => c.projects.updateProject({ tenant_id: 'tenant-a', project_id: 'proj-1', clear_description: true }),
    },
    {
      method: 'listProjectsPage',
      field: 'autonomous',
      call: (c) => c.projects.listProjectsPage('tenant-a', { autonomous: true }),
    },
  ];

  it.each(refusals)('$method refuses $field without calling the server', async ({ method, field, call }) => {
    const err = await call(new McpClient(endpoint)).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(TransportNotSupportedError);
    const refusal = err as TransportNotSupportedError;
    expect(refusal.transport).toBe('mcp');
    expect(refusal.family).toBe('projects');
    expect(refusal.method).toBe(method);
    expect(refusal.reason).toContain(field);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('accepts an empty metadata map on create, which asks for nothing', async () => {
    answer({ Project: wireProject });
    const client = new McpClient(endpoint);

    await client.projects.createProject({ tenant_id: 'tenant-a', name: 'api', metadata: {} });
    expect(lastCall().name).toBe('project_create');
  });
});

describe('McpClient.sdlc — tools', () => {
  it('maps each project-scoped read to its tool with the tenant and project', async () => {
    answer({ project_id: 'proj-1' });
    const client = new McpClient(endpoint);
    const reads: Array<[string, () => Promise<unknown>]> = [
      ['sdlc_binding', () => client.sdlc.binding('tenant-a', 'proj-1')],
      ['sdlc_onboard', () => client.sdlc.onboard('tenant-a', 'proj-1')],
      ['sdlc_conformance', () => client.sdlc.conformance('tenant-a', 'proj-1')],
      ['sdlc_open_work', () => client.sdlc.openWork('tenant-a', 'proj-1')],
      ['sdlc_collaborators', () => client.sdlc.collaborators('tenant-a', 'proj-1')],
    ];

    for (const [tool, read] of reads) {
      expect(await read()).toEqual({ project_id: 'proj-1' });
      expect(lastCall()).toEqual({ name: tool, args: { tenant_id: 'tenant-a', project_id: 'proj-1' } });
    }
  });

  it('unwraps the forge list', async () => {
    answer({ forges: ['gitea', 'github'], count: 2 });
    const client = new McpClient(endpoint);

    expect(await client.sdlc.forges()).toEqual(['gitea', 'github']);
    expect(lastCall()).toEqual({ name: 'sdlc_forges', args: {} });
  });

  it('nests the binding and defaults its project to the one named', async () => {
    answer({ project_id: 'proj-1', strategy: 'trunk-based' });
    const client = new McpClient(endpoint);
    // Only the fields the client touches matter here; the server validates the rest.
    const binding = { project_id: '', strategy: 'trunk-based' } as unknown as ProjectBinding;

    await client.sdlc.bind('tenant-a', 'proj-1', binding);
    expect(lastCall()).toEqual({
      name: 'sdlc_bind',
      args: { tenant_id: 'tenant-a', project_id: 'proj-1', binding: { project_id: 'proj-1', strategy: 'trunk-based' } },
    });
  });

  it('unbinds and resolves nothing, as REST does for its 204', async () => {
    answer({ project_id: 'proj-1', unbound: true });
    const client = new McpClient(endpoint);

    expect(await client.sdlc.unbind('tenant-a', 'proj-1')).toBeUndefined();
    expect(lastCall().name).toBe('sdlc_unbind');
  });

  it('carries the task, pull request, profile, filing and adoption arguments', async () => {
    answer({});
    const client = new McpClient(endpoint);

    await client.sdlc.delivery('tenant-a', 'task-1');
    expect(lastCall()).toEqual({ name: 'sdlc_delivery', args: { tenant_id: 'tenant-a', task_id: 'task-1' } });

    await client.sdlc.reviewer('tenant-a', 'proj-1', 42);
    expect(lastCall()).toEqual({ name: 'sdlc_reviewer', args: { tenant_id: 'tenant-a', project_id: 'proj-1', pull_request: 42 } });

    await client.sdlc.setReviewer('tenant-a', 'proj-1', 42, 'strict');
    expect(lastCall()).toEqual({
      name: 'sdlc_reviewer_set',
      args: { tenant_id: 'tenant-a', project_id: 'proj-1', pull_request: 42, profile: 'strict' },
    });

    await client.sdlc.clearReviewer('tenant-a', 'proj-1', 42);
    expect(lastCall().name).toBe('sdlc_reviewer_clear');

    await client.sdlc.fileIssue('tenant-a', 'proj-1', { title: 'split', labels: ['task:ready'], parent: 7 });
    expect(lastCall()).toEqual({
      name: 'sdlc_issue_create',
      args: { tenant_id: 'tenant-a', project_id: 'proj-1', title: 'split', labels: ['task:ready'], parent: 7 },
    });

    await client.sdlc.adoptIssue('tenant-a', 'proj-1', { issue: 9, pull_request: 10, ruling: 'rebase' });
    expect(lastCall()).toEqual({
      name: 'sdlc_adopt_issue',
      args: { tenant_id: 'tenant-a', project_id: 'proj-1', issue: 9, pull_request: 10, ruling: 'rebase' },
    });
  });

  it('shares one session with the other families', async () => {
    answer({ forges: [] });
    const client = new McpClient(endpoint);

    await client.sdlc.forges();
    await client.projects.listProjects('tenant-a').catch(() => undefined);

    const handshakes = fetchMock.mock.calls.filter(
      (call) => (JSON.parse((call[1] as RequestInit).body as string) as { method: string }).method === 'initialize',
    );
    expect(handshakes).toHaveLength(1);
  });
});
