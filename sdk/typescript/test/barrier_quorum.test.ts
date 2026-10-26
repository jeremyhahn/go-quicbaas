// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The barrier key holders work through the quorum surface the server serves
// (pkg/api/rest/routes.go, routes_iam.go and pkg/api/mcp): each holder is
// registered as an officer, claims its sealed share, then answers a quorum
// challenge with that share. These tests pin, for each verb, the route or
// tool it hits, the body it sends and how it maps the reply.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient, RestTenantBarrierClient } from '../src/rest';
import { McpClient } from '../src/mcp';
import { APIError, McpError, SOPINRequiresTLSError } from '../src/errors';
import { jsonReply, scriptServer, toolCalls, toolText } from './mcp_server';

const base = 'http://localhost';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

// sent returns the URL, method and decoded JSON body of the nth fetch.
function sent(n = 0): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[n];
  const raw = (init as RequestInit | undefined)?.body;
  return {
    url: url as string,
    method: (init as RequestInit | undefined)?.method ?? 'GET',
    body: typeof raw === 'string' ? JSON.parse(raw) : undefined,
  };
}

const challenge = {
  challenge_id: 'ch-1',
  domain: 'unseal',
  op: 'unseal',
  username: 'officer-1',
  factor: 'mtls',
  nonce: 'bm9uY2U=',
  expires_at: '2026-10-25T00:05:00Z',
};

const outcome = { domain: 'unseal', op: 'unseal', required: 2, submitted: 1, complete: false };

const submit = {
  domain: 'unseal' as const,
  op: 'unseal' as const,
  username: 'officer-1',
  challenge_id: 'ch-1',
  factor: 'mtls',
  response: 'c2ln',
  share: 'c2hhcmU=',
};

describe('RestBarrierClient quorum verbs', () => {
  it('lists the officers from /barrier/officers', async () => {
    const officer = { username: 'officer-1', role: 'officer', share_index: 1, enrolled_at: '2026-10-25T00:00:00Z', share_pending: true };
    fetchMock.mockResolvedValue(jsonResponse({ officers: [officer] }));

    const got = await new RestClient(base).barrier.officers();

    expect(got).toEqual([officer]);
    expect(sent()).toMatchObject({ url: `${base}/api/v1/barrier/officers`, method: 'GET' });
  });

  it('registers an unseal officer at /barrier/officers', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ cert_pem: 'CERT', role: 'officer' }));

    const got = await new RestClient(base).barrier.registerOfficer({ username: 'officer-1', csr_pem: 'CSR' });

    expect(got).toEqual({ cert_pem: 'CERT', role: 'officer' });
    expect(sent()).toEqual({ url: `${base}/api/v1/barrier/officers`, method: 'POST', body: { username: 'officer-1', csr_pem: 'CSR' } });
  });

  it('registers a recovery officer at /barrier/recovery/officers', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ cert_pem: 'CERT', role: 'officer' }));

    await new RestClient(base).barrier.registerOfficer({ username: 'officer-1', csr_pem: 'CSR' }, 'recovery');

    expect(sent().url).toBe(`${base}/api/v1/barrier/recovery/officers`);
  });

  it('begins and completes a share claim under the officer path', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ nonce_hex: 'beef', expires_at: '2026-10-25T00:05:00Z' }))
      .mockResolvedValueOnce(jsonResponse({ share_index: 1, envelope: 'ZW52' }));
    const client = new RestClient(base);

    const begun = await client.barrier.shareClaimBegin('officer 1');
    const claimed = await client.barrier.shareClaimComplete({ username: 'officer 1', nonce_hex: 'beef', signature: 'c2ln' });

    expect(begun).toEqual({ nonce_hex: 'beef', expires_at: '2026-10-25T00:05:00Z' });
    expect(claimed).toEqual({ share_index: 1, envelope: 'ZW52' });
    expect(sent(0)).toEqual({
      url: `${base}/api/v1/barrier/officers/officer%201/claim/begin`,
      method: 'POST',
      body: { username: 'officer 1' },
    });
    expect(sent(1)).toEqual({
      url: `${base}/api/v1/barrier/officers/officer%201/claim/complete`,
      method: 'POST',
      body: { username: 'officer 1', nonce_hex: 'beef', signature: 'c2ln' },
    });
  });

  it('claims a recovery share under /barrier/recovery/officers', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ nonce_hex: 'beef', expires_at: '2026-10-25T00:05:00Z' }));

    await new RestClient(base).barrier.shareClaimBegin('officer-1', 'recovery');

    expect(sent().url).toBe(`${base}/api/v1/barrier/recovery/officers/officer-1/claim/begin`);
  });

  it('begins a quorum round at /barrier/quorum/begin', async () => {
    fetchMock.mockResolvedValue(jsonResponse(challenge));

    const got = await new RestClient(base).barrier.quorumBegin({ domain: 'unseal', op: 'unseal', username: 'officer-1' });

    expect(got).toEqual(challenge);
    expect(sent()).toEqual({
      url: `${base}/api/v1/barrier/quorum/begin`,
      method: 'POST',
      body: { domain: 'unseal', op: 'unseal', username: 'officer-1' },
    });
  });

  it('submits a share at /barrier/quorum/submit', async () => {
    fetchMock.mockResolvedValue(jsonResponse(outcome));

    const got = await new RestClient(base).barrier.quorumSubmit(submit);

    expect(got).toEqual(outcome);
    expect(sent()).toEqual({ url: `${base}/api/v1/barrier/quorum/submit`, method: 'POST', body: submit });
  });

  it('opens a rekey round at /barrier/rekey/start', async () => {
    const round = { domain: 'unseal', op: 'rekey', required: 2, submitted: 0, expires_at: '2026-10-25T01:00:00Z', new_threshold: 3 };
    fetchMock.mockResolvedValue(jsonResponse(round));

    const got = await new RestClient(base).barrier.rekeyStart({ domain: 'unseal', threshold: 3 });

    expect(got).toEqual(round);
    expect(sent()).toEqual({ url: `${base}/api/v1/barrier/rekey/start`, method: 'POST', body: { domain: 'unseal', threshold: 3 } });
  });

  it('surfaces a refused submission as APIError', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: 'challenge expired' }, 409));

    await expect(new RestClient(base).barrier.quorumSubmit(submit)).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestTenantBarrierClient quorum verbs', () => {
  it('begins a tenant quorum round under the tenant path', async () => {
    fetchMock.mockResolvedValue(jsonResponse(challenge));

    const got = await new RestTenantBarrierClient(base).tenantBarrierQuorumBegin('tenant a', { op: 'unseal', username: 'officer-1' });

    expect(got).toEqual(challenge);
    expect(sent()).toEqual({
      url: `${base}/api/v1/tenants/tenant%20a/barrier/quorum/begin`,
      method: 'POST',
      body: { op: 'unseal', username: 'officer-1' },
    });
  });

  it('submits a tenant share under the tenant path', async () => {
    fetchMock.mockResolvedValue(jsonResponse(outcome));
    const { domain: _domain, ...tenantSubmit } = submit;

    const got = await new RestTenantBarrierClient(base).tenantBarrierQuorumSubmit('tenant-a', tenantSubmit);

    expect(got).toEqual(outcome);
    expect(sent()).toEqual({ url: `${base}/api/v1/tenants/tenant-a/barrier/quorum/submit`, method: 'POST', body: tenantSubmit });
  });

  it('surfaces a refused tenant submission as APIError', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: 'not an officer' }, 403));
    const { domain: _domain, ...tenantSubmit } = submit;

    await expect(new RestTenantBarrierClient(base).tenantBarrierQuorumSubmit('tenant-a', tenantSubmit)).rejects.toBeInstanceOf(APIError);
  });
});

describe('McpBarrierClient quorum tools', () => {
  const endpoint = 'https://node.example:8081/mcp';

  const cases: { name: string; call: (c: McpClient) => Promise<unknown>; tool: string; args: Record<string, unknown>; reply: unknown; want: unknown }[] = [
    {
      name: 'officers',
      call: (c) => c.barrier.officers(),
      tool: 'qrdb_barrier_officer_list',
      args: {},
      reply: { officers: [{ username: 'officer-1', share_pending: false }] },
      want: [{ username: 'officer-1', share_pending: false }],
    },
    {
      name: 'registerOfficer',
      call: (c) => c.barrier.registerOfficer({ username: 'officer-1', csr_pem: 'CSR' }),
      tool: 'qrdb_barrier_officer_register',
      args: { username: 'officer-1', csr_pem: 'CSR' },
      reply: { cert_pem: 'CERT', role: 'officer' },
      want: { cert_pem: 'CERT', role: 'officer' },
    },
    {
      name: 'registerOfficer for recovery',
      call: (c) => c.barrier.registerOfficer({ username: 'officer-1', csr_pem: 'CSR' }, 'recovery'),
      tool: 'qrdb_barrier_recovery_officer_register',
      args: { username: 'officer-1', csr_pem: 'CSR' },
      reply: { cert_pem: 'CERT', role: 'officer' },
      want: { cert_pem: 'CERT', role: 'officer' },
    },
    {
      name: 'shareClaimBegin',
      call: (c) => c.barrier.shareClaimBegin('officer-1'),
      tool: 'qrdb_barrier_share_claim_begin',
      args: { username: 'officer-1' },
      reply: { nonce_hex: 'beef', expires_at: '2026-10-25T00:05:00Z' },
      want: { nonce_hex: 'beef', expires_at: '2026-10-25T00:05:00Z' },
    },
    {
      name: 'shareClaimComplete for recovery',
      call: (c) => c.barrier.shareClaimComplete({ username: 'officer-1', nonce_hex: 'beef', signature: 'c2ln' }, 'recovery'),
      tool: 'qrdb_barrier_recovery_share_claim_complete',
      args: { username: 'officer-1', nonce_hex: 'beef', signature: 'c2ln' },
      reply: { share_index: 2, envelope: 'ZW52' },
      want: { share_index: 2, envelope: 'ZW52' },
    },
    {
      name: 'quorumBegin',
      call: (c) => c.barrier.quorumBegin({ domain: 'unseal', op: 'unseal', username: 'officer-1' }),
      tool: 'qrdb_barrier_quorum_begin',
      args: { domain: 'unseal', op: 'unseal', username: 'officer-1' },
      reply: challenge,
      want: challenge,
    },
    {
      name: 'quorumSubmit',
      call: (c) => c.barrier.quorumSubmit(submit),
      tool: 'qrdb_barrier_quorum_submit',
      args: submit,
      reply: outcome,
      want: outcome,
    },
    {
      name: 'rekeyStart',
      call: (c) => c.barrier.rekeyStart({ domain: 'recovery', threshold: 2 }),
      tool: 'qrdb_barrier_rekey_start',
      args: { domain: 'recovery', threshold: 2 },
      reply: { domain: 'recovery', op: 'rekey', required: 2, submitted: 0, expires_at: '2026-10-25T01:00:00Z' },
      want: { domain: 'recovery', op: 'rekey', required: 2, submitted: 0, expires_at: '2026-10-25T01:00:00Z' },
    },
  ];

  it.each(cases)('$name calls $tool', async (c) => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, c.reply)));

    const got = await c.call(new McpClient(endpoint));

    const calls = toolCalls(fetchMock);
    expect(calls).toHaveLength(1);
    expect(calls[0].params?.name).toBe(c.tool);
    expect(calls[0].params?.arguments).toEqual(c.args);
    expect(got).toEqual(c.want);
  });

  it('refuses to send a rekey SO PIN over plaintext http', async () => {
    const err = await new McpClient('http://node.example:8081/mcp')
      .barrier.rekeyStart({ domain: 'unseal', threshold: 2, so_pin: 'so-pin-1234' })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(SOPINRequiresTLSError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('refuses to send a share over plaintext http', async () => {
    const err = await new McpClient('http://node.example:8081/mcp')
      .barrier.quorumSubmit(submit)
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(SOPINRequiresTLSError);
    expect((err as SOPINRequiresTLSError).transport).toBe('mcp');
    expect(String((err as Error).message)).not.toContain(submit.share);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('sends a share over https', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, outcome)));

    await new McpClient(endpoint).barrier.quorumSubmit(submit);

    const calls = toolCalls(fetchMock);
    expect(calls).toHaveLength(1);
    expect(calls[0].params?.arguments?.share).toBe(submit.share);
  });

  it('sends a factor-only submission over plaintext http', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, outcome)));

    await new McpClient('http://node.example:8081/mcp').barrier.quorumSubmit({ ...submit, share: '' });

    const calls = toolCalls(fetchMock);
    expect(calls).toHaveLength(1);
    expect(calls[0].params?.arguments?.share).toBe('');
  });

  it('surfaces a refused submission as McpError', async () => {
    scriptServer(fetchMock, (msg) =>
      jsonReply({ jsonrpc: '2.0', id: msg.id, result: { isError: true, content: [{ type: 'text', text: 'challenge expired' }] } }),
    );

    await expect(new McpClient(endpoint).barrier.quorumSubmit(submit)).rejects.toBeInstanceOf(McpError);
  });
});
