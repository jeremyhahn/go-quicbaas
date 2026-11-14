// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for the cluster leader, rebalance and migration methods of
// RestClusterClient: each asserts the URL, HTTP method and body sent.

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

function jsonResponse(status: number, body: unknown) {
  const text = JSON.stringify(body);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 403 ? 'Forbidden' : 'OK',
    json: async () => JSON.parse(text),
    text: async () => text,
  };
}

function lastCall(): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [
    string,
    { method?: string; body?: string },
  ];
  return { url, method: init.method ?? 'GET', body: init.body };
}

const base = 'http://localhost';
const now = '2026-11-12T00:00:00Z';

describe('RestClusterClient cluster operations', () => {
  it('leader GETs /api/v1/cluster/leader', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        leader: {
          node_id: 1,
          address: 'a:1',
          raft_address: 'a:2',
          is_leader: true,
          health_status: 'healthy',
          version: 'v1',
          uptime_seconds: 5,
        },
        timestamp: now,
      }),
    );
    const res = await new RestClient(base).cluster.leader();
    expect(lastCall()).toEqual({ url: `${base}/api/v1/cluster/leader`, method: 'GET', body: undefined });
    expect(res.leader.node_id).toBe(1);
  });

  it('shardLeader GETs /api/v1/cluster/leader/{shardID}', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { shard_id: 7, leader_id: 2, valid: true }));
    const res = await new RestClient(base).cluster.shardLeader(7);
    expect(lastCall()).toEqual({ url: `${base}/api/v1/cluster/leader/7`, method: 'GET', body: undefined });
    expect(res.leader_id).toBe(2);
  });

  it('startRebalance POSTs the request to /api/v1/cluster/rebalance/', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(202, { operation_id: 'op1', started: true, status: 'running', timestamp: now }),
    );
    const res = await new RestClient(base).cluster.startRebalance({ strategy: 'even', target_shard_count: 4 });
    const call = lastCall();
    expect(call.url).toBe(`${base}/api/v1/cluster/rebalance/`);
    expect(call.method).toBe('POST');
    expect(JSON.parse(call.body as string)).toEqual({ strategy: 'even', target_shard_count: 4 });
    expect(res.operation_id).toBe('op1');
  });

  it('startRebalance with no request sends {}', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(202, { operation_id: 'op1', started: true, status: 'running', timestamp: now }),
    );
    await new RestClient(base).cluster.startRebalance();
    const call = lastCall();
    expect(call.url).toBe(`${base}/api/v1/cluster/rebalance/`);
    expect(call.method).toBe('POST');
    expect(call.body).toBe('{}');
  });

  it('rebalanceStatus GETs the status, with and without an encoded operation_id', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { status: 'idle', percent_complete: 0, elapsed_time: 0, timestamp: now }),
    );
    const client = new RestClient(base);
    await client.cluster.rebalanceStatus();
    expect(lastCall()).toEqual({ url: `${base}/api/v1/cluster/rebalance/status`, method: 'GET', body: undefined });
    await client.cluster.rebalanceStatus('op 1/&x');
    expect(lastCall().url).toBe(`${base}/api/v1/cluster/rebalance/status?operation_id=op%201%2F%26x`);
  });

  it('rebalancePlan GETs /api/v1/cluster/rebalance/plan', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        current_shards: 2,
        target_shards: 4,
        strategy: 'even',
        estimated_time: 1000,
        timestamp: now,
      }),
    );
    const res = await new RestClient(base).cluster.rebalancePlan();
    expect(lastCall()).toEqual({ url: `${base}/api/v1/cluster/rebalance/plan`, method: 'GET', body: undefined });
    expect(res.target_shards).toBe(4);
  });

  it('startMigration POSTs the request to /api/v1/cluster/migrate/', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(202, {
        operation_id: 'm1',
        status: 'running',
        source_shard_id: 1,
        target_shard_id: 2,
        estimated_time: 0,
        timestamp: now,
      }),
    );
    const req = { source_shard_id: 1, target_shard_id: 2, entity_types: ['users'] };
    const res = await new RestClient(base).cluster.startMigration(req);
    const call = lastCall();
    expect(call.url).toBe(`${base}/api/v1/cluster/migrate/`);
    expect(call.method).toBe('POST');
    expect(JSON.parse(call.body as string)).toEqual(req);
    expect(res.operation_id).toBe('m1');
  });

  it('migrationStatus GETs the status with an encoded operation_id', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, {
        operation_id: 'm 1',
        status: 'running',
        keys_processed: 3,
        bytes_processed: 9,
        percent_complete: 50,
        elapsed_time: 10,
        timestamp: now,
      }),
    );
    const res = await new RestClient(base).cluster.migrationStatus('m 1/&x');
    expect(lastCall()).toEqual({
      url: `${base}/api/v1/cluster/migrate/status?operation_id=m%201%2F%26x`,
      method: 'GET',
      body: undefined,
    });
    expect(res.keys_processed).toBe(3);
  });

  it('cancelMigration DELETEs /api/v1/cluster/migrate/ with an encoded operation_id', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { operation_id: 'm 1', cancelled: true, message: 'cancelled', timestamp: now }),
    );
    const res = await new RestClient(base).cluster.cancelMigration('m 1/&x');
    expect(lastCall()).toEqual({
      url: `${base}/api/v1/cluster/migrate/?operation_id=m%201%2F%26x`,
      method: 'DELETE',
      body: undefined,
    });
    expect(res.cancelled).toBe(true);
  });

  it('rejects with the status on a 403', async () => {
    fetchMock.mockResolvedValue(jsonResponse(403, { error: 'permission denied' }));
    await expect(new RestClient(base).cluster.cancelMigration('m1')).rejects.toMatchObject({
      status: 403,
    });
  });
});
