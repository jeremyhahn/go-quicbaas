// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for SDK/API response shape mismatches discovered during the 2026-04-26 audit.
// Each describe block corresponds to one fixed domain; tests verify the SDK maps
// the ACTUAL server shape into the EXPECTED GUI interface.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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
// Orchestrator — listJobs: server {jobs: [...] | null} → SDK JobStatus[]
// =============================================================================

describe('RestOrchestratorClient.listJobs shape fix', () => {
  it('unwraps {jobs:[...]} envelope into bare array', async () => {
    const jobs = [{ job_id: 'j1', state: 'executing', tenant_id: 'default' }];
    fetchMock.mockResolvedValue(makeResponse({ jobs }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listJobs('default');
    expect(Array.isArray(result)).toBe(true);
    expect(result).toHaveLength(1);
    expect(result[0].job_id).toBe('j1');
  });

  it('returns empty array when server sends {jobs: null}', async () => {
    fetchMock.mockResolvedValue(makeResponse({ jobs: null }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listJobs('default');
    expect(result).toEqual([]);
  });

  it('returns bare array unchanged when server already sends array (legacy)', async () => {
    const jobs = [{ job_id: 'j2', state: 'completed' }];
    fetchMock.mockResolvedValue(makeResponse(jobs));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listJobs('default');
    expect(result).toHaveLength(1);
    expect(result[0].job_id).toBe('j2');
  });
});

// =============================================================================
// Orchestrator — listTasks: server {tasks:[...]} → SDK Task[]
// =============================================================================

describe('RestOrchestratorClient.listTasks shape fix', () => {
  it('unwraps {tasks:[...]} envelope into bare array', async () => {
    const tasks = [{ task_id: 't1', subject: 'do something', tenant_id: 'default', status: 'pending' }];
    fetchMock.mockResolvedValue(makeResponse({ tasks }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listTasks('default');
    expect(Array.isArray(result)).toBe(true);
    expect(result).toHaveLength(1);
    expect(result[0].task_id).toBe('t1');
  });

  it('returns empty array when server sends {tasks: []}', async () => {
    fetchMock.mockResolvedValue(makeResponse({ tasks: [] }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listTasks('default');
    expect(result).toEqual([]);
  });

  it('returns bare array unchanged when server already sends array (legacy)', async () => {
    const tasks = [{ task_id: 't3', subject: 'legacy' }];
    fetchMock.mockResolvedValue(makeResponse(tasks));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listTasks('default');
    expect(result).toHaveLength(1);
  });
});

// =============================================================================
// Orchestrator — listSessions: server {sessions:[], timestamp} → SDK ListSessionsResponse
// =============================================================================

describe('RestOrchestratorClient.listSessions shape fix', () => {
  it('provides count derived from sessions array length when absent', async () => {
    const sessions = [{ session_id: 's1', state: 'active' }, { session_id: 's2', state: 'idle' }];
    fetchMock.mockResolvedValue(makeResponse({ sessions, timestamp: 1777190217685280708 }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listSessions({ tenantID: 'default' });
    expect(result.sessions).toHaveLength(2);
    expect(result.count).toBe(2);
  });

  it('uses server count when present and does not override it', async () => {
    fetchMock.mockResolvedValue(makeResponse({ sessions: [], count: 5 }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listSessions({ tenantID: 'default' });
    expect(result.count).toBe(5);
  });

  it('throws when tenantID is missing', async () => {
    const client = new RestClient('http://localhost');
    await expect(client.orchestrator.listSessions({ tenantID: '' })).rejects.toThrow('tenantID is required');
  });
});

// =============================================================================
// Orchestrator — listWorkflows: server {workflows:[]} → SDK ListWorkflowsResponse
// =============================================================================

describe('RestOrchestratorClient.listWorkflows shape fix', () => {
  it('derives count from workflows array when server omits it', async () => {
    const workflows = [{ name: 'wf1' }, { name: 'wf2' }];
    fetchMock.mockResolvedValue(makeResponse({ workflows }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listWorkflows('default');
    expect(result.workflows).toHaveLength(2);
    expect(result.count).toBe(2);
  });

  it('returns empty workflows array on empty server response', async () => {
    fetchMock.mockResolvedValue(makeResponse({ workflows: [] }));
    const client = new RestClient('http://localhost');
    const result = await client.orchestrator.listWorkflows('default');
    expect(result.workflows).toEqual([]);
    expect(result.count).toBe(0);
  });
});

// =============================================================================
// Messaging — listChannels: server {channels:[], total, timestamp} → SDK ListChannelsResponse
// =============================================================================

describe('RestMessagingClient.listChannels shape fix', () => {
  it('maps server total → total_count and provides has_more/next_cursor defaults', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({ channels: [], total: 7, timestamp: 1777190253 }),
    );
    const client = new RestClient('http://localhost');
    const result = await client.messaging.listChannels();
    expect(result.channels).toEqual([]);
    expect(result.total_count).toBe(7);
    expect(result.has_more).toBe(false);
    expect(result.next_cursor).toBe('');
  });

  it('prefers total_count over total when both are present', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({ channels: [], total: 3, total_count: 10 }),
    );
    const client = new RestClient('http://localhost');
    const result = await client.messaging.listChannels();
    expect(result.total_count).toBe(10);
  });

  it('maps next_cursor and has_more when present', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({ channels: [{ name: 'c1' }], total: 1, has_more: true, next_cursor: 'tok' }),
    );
    const client = new RestClient('http://localhost');
    const result = await client.messaging.listChannels();
    expect(result.has_more).toBe(true);
    expect(result.next_cursor).toBe('tok');
  });
});

// =============================================================================
// Scheduler — listJobs: server {jobs:[], has_more, timestamp} → SDK ListJobsResponse
// =============================================================================

describe('RestSchedulerClient.listJobs shape fix', () => {
  it('maps server {jobs:[], has_more} to full ListJobsResponse', async () => {
    fetchMock.mockResolvedValue(
      makeResponse({ jobs: [], has_more: false, timestamp: '2026-04-26T03:57:13Z' }),
    );
    const client = new RestClient('http://localhost');
    const result = await client.scheduler.listJobs();
    expect(result.jobs).toEqual([]);
    expect(result.has_more).toBe(false);
    expect(result.total_count).toBe(0);
    expect(result.next_cursor).toBe('');
    expect(result.timestamp).toBe('2026-04-26T03:57:13Z');
  });

  it('derives total_count from jobs array when absent', async () => {
    const jobs = [{ id: 1, name: 'job1' }, { id: 2, name: 'job2' }];
    fetchMock.mockResolvedValue(makeResponse({ jobs, has_more: false }));
    const client = new RestClient('http://localhost');
    const result = await client.scheduler.listJobs();
    expect(result.total_count).toBe(2);
    expect(result.jobs).toHaveLength(2);
  });

  it('uses provided total_count when server includes it', async () => {
    fetchMock.mockResolvedValue(makeResponse({ jobs: [], has_more: true, total_count: 99 }));
    const client = new RestClient('http://localhost');
    const result = await client.scheduler.listJobs();
    expect(result.total_count).toBe(99);
    expect(result.has_more).toBe(true);
  });
});

// =============================================================================
// IAM — listTenants: server {limit, offset, tenants:[...]} → SDK ListTenantsResponse
// =============================================================================

describe('RestIAMClient.listTenants shape fix', () => {
  it('derives count from tenants array when server omits it', async () => {
    const tenants = [
      { id: 'default', name: 'Default Tenant', created: '2026-04-26T03:55:32Z', updated: '2026-04-26T03:55:32Z' },
    ];
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, tenants }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listTenants();
    expect(result.tenants).toHaveLength(1);
    expect(result.count).toBe(1);
  });

  it('uses server count when present', async () => {
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, tenants: [], count: 42 }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listTenants();
    expect(result.count).toBe(42);
  });
});

// =============================================================================
// IAM — listUsers: server {limit, offset, users:[]} → SDK ListUsersResponse
// =============================================================================

describe('RestIAMClient.listUsers shape fix', () => {
  it('derives count from users array when server omits it', async () => {
    const users = [{ id: 'u1', username: 'alice', email: 'alice@example.com', tenant_id: 'default' }];
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, users }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listUsers('default');
    expect(result.users).toHaveLength(1);
    expect(result.count).toBe(1);
  });

  it('returns empty users and count 0 when users list is empty', async () => {
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, users: [] }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listUsers('default');
    expect(result.users).toEqual([]);
    expect(result.count).toBe(0);
  });
});

// =============================================================================
// IAM — listRoles: server {limit, offset, roles:[]} → SDK ListRolesResponse
// =============================================================================

describe('RestIAMClient.listRoles shape fix', () => {
  it('derives count from roles array when server omits it', async () => {
    const roles = [{ id: 'r1', name: 'admin', tenant_id: 'default' }];
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, roles }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listRoles('default');
    expect(result.roles).toHaveLength(1);
    expect(result.count).toBe(1);
  });

  it('returns empty roles and count 0 when roles list is empty', async () => {
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, roles: [] }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listRoles('default');
    expect(result.roles).toEqual([]);
    expect(result.count).toBe(0);
  });

  it('passes retired_permissions through so the GUI can flag role drift', async () => {
    const roles = [{ id: 'r1', name: 'admin', tenant_id: 'default', permissions: ['kv:write'], retired_permissions: ['kv:put'] }];
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, roles }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listRoles('default');
    expect(result.roles[0].retired_permissions).toEqual(['kv:put']);
  });
});

// =============================================================================
// IAM — listPolicies: server {limit, offset, policies:[]} → SDK ListPoliciesResponse
// =============================================================================

describe('RestIAMClient.listPolicies shape fix', () => {
  it('derives count from policies array when server omits it', async () => {
    const policies = [{ id: 'p1', name: 'read-only', tenant_id: 'default' }];
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, policies }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listPolicies('default');
    expect(result.policies).toHaveLength(1);
    expect(result.count).toBe(1);
  });

  it('returns empty policies and count 0 when policies list is empty', async () => {
    fetchMock.mockResolvedValue(makeResponse({ limit: 100, offset: 0, policies: [] }));
    const client = new RestClient('http://localhost');
    const result = await client.iam.listPolicies('default');
    expect(result.policies).toEqual([]);
    expect(result.count).toBe(0);
  });
});

// =============================================================================
// Secrets — listSecrets: server {secrets:[], has_more, timestamp} → SDK ListSecretsResponse
// =============================================================================

describe('RestSecretsClient.listSecrets shape fix', () => {
  it('derives count from secrets array when server omits it', async () => {
    const secrets = [{ path: 'db/password', version: 1 }, { path: 'api/key', version: 2 }];
    fetchMock.mockResolvedValue(
      makeResponse({ secrets, has_more: false, timestamp: '2026-04-26T03:57:00Z' }),
    );
    const client = new RestClient('http://localhost');
    const result = await client.secrets.listSecrets();
    expect(result.secrets).toHaveLength(2);
    expect(result.count).toBe(2);
  });

  it('returns count 0 for empty secrets list when server omits count', async () => {
    fetchMock.mockResolvedValue(makeResponse({ secrets: [], has_more: false }));
    const client = new RestClient('http://localhost');
    const result = await client.secrets.listSecrets();
    expect(result.secrets).toEqual([]);
    expect(result.count).toBe(0);
  });
});
