// decomposeTask posts the free-text task to the route the server registers:
// POST /api/v1/orchestrator/tasks/decompose?tenant_id=<id>, with every
// request field except tenant_id in the JSON body (#2372).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function makeResp(status: number, payload: unknown) {
  const body = JSON.stringify(payload);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 403 ? 'Forbidden' : 'OK',
    headers: new Headers({ 'Content-Type': 'application/json' }),
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

describe('orchestrator decomposeTask', () => {
  it('posts the task text to /tasks/decompose with the tenant in the query', async () => {
    const reply = {
      work_items: [
        { id: 'w1', project_id: 'p1', title: 'Split', description: 'first part', priority: 1 },
        { id: 'w2', project_id: 'p1', title: 'Join', description: 'second part', priority: 2, dependencies: ['w1'] },
      ],
      timestamp: 1700000000000000000,
    };
    fetchMock.mockResolvedValue(makeResp(200, reply));

    const client = new RestClient('http://localhost');
    const res = await client.orchestrator.decomposeTask({
      tenant_id: 'acme corp/1',
      task: 'Build the login page and its tests',
      backend: 'claude',
      model: 'opus',
      project_ids: ['p1'],
      use_rag: true,
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(
      `http://localhost/api/v1/orchestrator/tasks/decompose?tenant_id=${encodeURIComponent('acme corp/1')}`,
    );
    expect(init.method).toBe('POST');
    const sent = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(sent).toEqual({
      task: 'Build the login page and its tests',
      backend: 'claude',
      model: 'opus',
      project_ids: ['p1'],
      use_rag: true,
    });
    expect(sent).not.toHaveProperty('tenant_id');

    expect(res.work_items).toHaveLength(2);
    expect(res.work_items[1].dependencies).toEqual(['w1']);
    expect(res.timestamp).toBe(reply.timestamp);
  });

  it('sends only the task when no optional field is set', async () => {
    fetchMock.mockResolvedValue(makeResp(200, { work_items: [], timestamp: 1 }));

    const client = new RestClient('http://localhost');
    await client.orchestrator.decomposeTask({ tenant_id: 't1', task: 'do it' });

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({ task: 'do it' });
  });

  it('rejects on a 403', async () => {
    fetchMock.mockResolvedValue(makeResp(403, { error: 'forbidden' }));

    const client = new RestClient('http://localhost');
    await expect(
      client.orchestrator.decomposeTask({ tenant_id: 't1', task: 'do it' }),
    ).rejects.toThrow();
  });
});
