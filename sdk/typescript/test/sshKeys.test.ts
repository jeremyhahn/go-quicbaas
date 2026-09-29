// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import {
  SSHNotFoundError,
  SSHDuplicateError,
  SSHInvalidError,
  SSHUnavailableError,
  SSHServerError,
  SSHHostMismatchError,
} from '../src/errors';
import { RestSSHKeysClient, RestKnownHostsClient, RestAuthorizedKeysClient } from '../src/rest';

// Minimal SSHKey fixture matching xkmsssh.Key JSON tags.
const keyFixture = {
  scope: 'user',
  owner: 'alice',
  name: 'laptop',
  algorithm: 'ed25519',
  fingerprint: 'SHA256:abc',
  public_key: 'ssh-ed25519 AAA laptop@quicbaas',
  created_at: '2026-01-01T00:00:00Z',
};

const pinFixture = {
  scope: 'user',
  owner: 'alice',
  host: 'server.example.com',
  fingerprint: 'SHA256:xyz',
  algorithm: 'ssh-ed25519',
  added_at: '2026-01-01T00:00:00Z',
  source: 'manual',
};

// Helper: build a Response with JSON body.
function jsonResp(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// Helper: build an error envelope response.
function errResp(status: number, error: string, extra: Record<string, string> = {}): Response {
  return jsonResp({ error, ...extra }, status);
}

// =============================================================================
// RestSSHKeysClient — legacy flat surface (backward compat)
// =============================================================================

describe('RestSSHKeysClient — legacy flat surface', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('list returns server payload mapping items to both keys and items fields', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [keyFixture] }));
    const client = new RestClient('http://localhost');
    const resp = await client.sshKeys.list();
    expect(resp.items).toHaveLength(1);
    expect(resp.keys).toHaveLength(1);
    expect(resp.keys![0].name).toBe('laptop');
    expect(resp.keys![0].public_key).toContain('ssh-ed25519');
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/keys');
  });

  it('list returns empty on server error (silent fallback)', async () => {
    fetchMock.mockResolvedValue(new Response('', { status: 500 }));
    const client = new RestClient('http://localhost');
    const resp = await client.sshKeys.list();
    expect(resp.keys).toHaveLength(0);
    expect(resp.items).toHaveLength(0);
  });

  it('list returns empty on network error', async () => {
    fetchMock.mockRejectedValue(new Error('network fail'));
    const client = new RestClient('http://localhost');
    const resp = await client.sshKeys.list();
    expect(resp.keys).toHaveLength(0);
  });

  it('create POSTs to /api/v1/ssh/keys with JSON body', async () => {
    fetchMock.mockResolvedValue(jsonResp(keyFixture, 201));
    const client = new RestClient('http://localhost');
    const key = await client.sshKeys.create({ name: 'laptop', algorithm: 'ed25519' });
    expect(key.name).toBe('laptop');
    expect(key.algorithm).toBe('ed25519');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/keys');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toMatchObject({ name: 'laptop', algorithm: 'ed25519' });
  });

  it('create throws SSHDuplicateError on 409 without mismatch fields', async () => {
    fetchMock.mockResolvedValue(errResp(409, 'duplicate key'));
    const client = new RestClient('http://localhost');
    await expect(client.sshKeys.create({ name: 'dup', algorithm: 'ed25519' })).rejects.toBeInstanceOf(SSHDuplicateError);
  });

  it('create throws SSHInvalidError on 400', async () => {
    fetchMock.mockResolvedValue(errResp(400, 'bad request'));
    const client = new RestClient('http://localhost');
    await expect(client.sshKeys.create({ name: '', algorithm: '' })).rejects.toBeInstanceOf(SSHInvalidError);
  });

  it('create throws SSHUnavailableError on 503', async () => {
    fetchMock.mockResolvedValue(errResp(503, 'xkms not configured'));
    const client = new RestClient('http://localhost');
    await expect(client.sshKeys.create({ name: 'x', algorithm: 'ed25519' })).rejects.toBeInstanceOf(SSHUnavailableError);
  });

  it('create throws SSHServerError on 500', async () => {
    fetchMock.mockResolvedValue(errResp(500, 'internal error'));
    const client = new RestClient('http://localhost');
    await expect(client.sshKeys.create({ name: 'x', algorithm: 'ed25519' })).rejects.toBeInstanceOf(SSHServerError);
  });

  it('get finds key by name via list then returns it', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [keyFixture] }));
    const client = new RestClient('http://localhost');
    const key = await client.sshKeys.get('laptop');
    expect(key.name).toBe('laptop');
  });

  it('get throws SSHNotFoundError when name not in list', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [] }));
    const client = new RestClient('http://localhost');
    await expect(client.sshKeys.get('missing')).rejects.toBeInstanceOf(SSHNotFoundError);
  });

  it('remove issues DELETE to scoped path using key triple from list', async () => {
    // First call: list (get); second call: DELETE
    fetchMock
      .mockResolvedValueOnce(jsonResp({ items: [keyFixture] }))
      .mockResolvedValueOnce(jsonResp({}, 200));
    const client = new RestClient('http://localhost');
    await client.sshKeys.remove('laptop');
    const [deleteUrl, deleteInit] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(deleteUrl).toBe('http://localhost/api/v1/ssh/keys/user/alice/laptop');
    expect(deleteInit.method).toBe('DELETE');
  });

  it('client.sshKeys and client.ssh.keys are the same instance', () => {
    const client = new RestClient('http://localhost');
    expect(client.sshKeys).toBe(client.ssh.keys);
  });
});

// =============================================================================
// RestSSHKeysClient — scoped methods
// =============================================================================

describe('RestSSHKeysClient — scoped methods', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('listScoped appends scope and owner query params', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [keyFixture] }));
    const client = new RestSSHKeysClient('http://localhost');
    const resp = await client.listScoped({ scope: 'user', owner: 'alice' });
    expect(resp.items).toHaveLength(1);
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toContain('scope=user');
    expect(url).toContain('owner=alice');
  });

  it('listScoped with no query hits /api/v1/ssh/keys without params', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [] }));
    const client = new RestSSHKeysClient('http://localhost');
    const resp = await client.listScoped();
    expect(resp.items).toHaveLength(0);
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toBe('http://localhost/api/v1/ssh/keys');
  });

  it('getScoped hits /api/v1/ssh/keys/{scope}/{owner}/{name}', async () => {
    fetchMock.mockResolvedValue(jsonResp(keyFixture));
    const client = new RestSSHKeysClient('http://localhost');
    const key = await client.getScoped('user', 'alice', 'laptop');
    expect(key.name).toBe('laptop');
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toBe('http://localhost/api/v1/ssh/keys/user/alice/laptop');
  });

  it('getScoped throws SSHNotFoundError on 404', async () => {
    fetchMock.mockResolvedValue(errResp(404, 'not found'));
    const client = new RestSSHKeysClient('http://localhost');
    await expect(client.getScoped('user', 'alice', 'missing')).rejects.toBeInstanceOf(SSHNotFoundError);
  });

  it('deleteScoped issues DELETE to triple path', async () => {
    fetchMock.mockResolvedValue(jsonResp({}, 200));
    const client = new RestSSHKeysClient('http://localhost');
    await client.deleteScoped('user', 'alice', 'laptop');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/keys/user/alice/laptop');
    expect(init.method).toBe('DELETE');
  });

  it('exportPublic hits the /public sub-path', async () => {
    fetchMock.mockResolvedValue(jsonResp({
      algorithm: 'ed25519',
      line: 'ssh-ed25519 AAA',
      fingerprint: 'SHA256:abc',
    }));
    const client = new RestSSHKeysClient('http://localhost');
    const pub = await client.exportPublic('user', 'alice', 'laptop');
    expect(pub.algorithm).toBe('ed25519');
    expect(pub.line).toBe('ssh-ed25519 AAA');
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toBe('http://localhost/api/v1/ssh/keys/user/alice/laptop/public');
  });

  it('sign POSTs digest to /sign and returns format+blob', async () => {
    const blobBytes = [1, 2, 3];
    fetchMock.mockResolvedValue(jsonResp({ format: 'ssh-ed25519', blob: blobBytes }));
    const client = new RestSSHKeysClient('http://localhost');
    const sig = await client.sign('user', 'alice', 'laptop', { digest: [0xde, 0xad] });
    expect(sig.format).toBe('ssh-ed25519');
    expect(sig.blob).toEqual(blobBytes);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/keys/user/alice/laptop/sign');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toMatchObject({ digest: [0xde, 0xad] });
  });

  it('import_ POSTs to /import with passphrase_b64 in body', async () => {
    fetchMock.mockResolvedValue(jsonResp(keyFixture, 201));
    const client = new RestSSHKeysClient('http://localhost');
    const key = await client.import_({
      name: 'imported',
      pem: '-----BEGIN PRIVATE KEY-----\nAAA\n-----END PRIVATE KEY-----',
      passphrase_b64: btoa('secret'),
      scope: 'user',
      owner: 'alice',
    });
    expect(key.name).toBe('laptop');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/keys/import');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body.passphrase_b64).toBe(btoa('secret'));
    expect(body.pem).toContain('BEGIN PRIVATE KEY');
  });

  it('import_ throws SSHDuplicateError on 409', async () => {
    fetchMock.mockResolvedValue(errResp(409, 'key already exists'));
    const client = new RestSSHKeysClient('http://localhost');
    await expect(client.import_({ name: 'dup', pem: 'pem' })).rejects.toBeInstanceOf(SSHDuplicateError);
  });
});

// =============================================================================
// RestKnownHostsClient
// =============================================================================

describe('RestKnownHostsClient', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('add POSTs to /api/v1/ssh/known-hosts', async () => {
    fetchMock.mockResolvedValue(jsonResp({}, 200));
    const client = new RestKnownHostsClient('http://localhost');
    await client.add({
      host: 'server.example.com',
      public_key: 'ssh-ed25519 AAAAxyz',
      scope: 'user',
      owner: 'alice',
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/known-hosts');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body.host).toBe('server.example.com');
    expect(body.public_key).toBe('ssh-ed25519 AAAAxyz');
  });

  it('add throws SSHDuplicateError on 409', async () => {
    fetchMock.mockResolvedValue(errResp(409, 'pin already exists'));
    const client = new RestKnownHostsClient('http://localhost');
    await expect(client.add({ host: 'x', public_key: 'ssh-ed25519 AAAAabc' })).rejects.toBeInstanceOf(SSHDuplicateError);
  });

  it('list GETs /api/v1/ssh/known-hosts with optional query params', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [pinFixture] }));
    const client = new RestKnownHostsClient('http://localhost');
    const resp = await client.list({ scope: 'user', owner: 'alice' });
    expect(resp.items).toHaveLength(1);
    expect(resp.items[0].host).toBe('server.example.com');
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toContain('scope=user');
    expect(url).toContain('owner=alice');
  });

  it('list with no query omits params', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [] }));
    const client = new RestKnownHostsClient('http://localhost');
    const resp = await client.list();
    expect(resp.items).toHaveLength(0);
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toBe('http://localhost/api/v1/ssh/known-hosts');
  });

  it('remove issues DELETE to /api/v1/ssh/known-hosts/{host}', async () => {
    fetchMock.mockResolvedValue(jsonResp({}, 200));
    const client = new RestKnownHostsClient('http://localhost');
    await client.remove('server.example.com', { scope: 'user', owner: 'alice' });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/ssh/known-hosts/server.example.com');
    expect(url).toContain('scope=user');
    expect(init.method).toBe('DELETE');
  });

  it('remove throws SSHNotFoundError on 404', async () => {
    fetchMock.mockResolvedValue(errResp(404, 'not pinned'));
    const client = new RestKnownHostsClient('http://localhost');
    await expect(client.remove('ghost.example.com')).rejects.toBeInstanceOf(SSHNotFoundError);
  });

  it('verify POSTs to /api/v1/ssh/known-hosts/{host}/verify', async () => {
    fetchMock.mockResolvedValue(jsonResp({}, 200));
    const client = new RestKnownHostsClient('http://localhost');
    await client.verify('server.example.com', {
      fingerprint: 'SHA256:xyz',
      scope: 'user',
      owner: 'alice',
    });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/known-hosts/server.example.com/verify');
    expect(init.method).toBe('POST');
  });

  it('verify throws SSHHostMismatchError on 409 with host/expected/received', async () => {
    fetchMock.mockResolvedValue(
      errResp(409, 'ssh/known_hosts: host key mismatch', {
        host: 'evil.example.com',
        expected: 'SHA256:good',
        received: 'SHA256:evil',
      }),
    );
    const client = new RestKnownHostsClient('http://localhost');
    let caughtErr: SSHHostMismatchError | undefined;
    try {
      await client.verify('evil.example.com', { fingerprint: 'SHA256:evil' });
    } catch (e) {
      caughtErr = e as SSHHostMismatchError;
    }
    expect(caughtErr).toBeInstanceOf(SSHHostMismatchError);
    expect(caughtErr!.host).toBe('evil.example.com');
    expect(caughtErr!.expected).toBe('SHA256:good');
    expect(caughtErr!.received).toBe('SHA256:evil');
  });

  it('verify throws SSHUnavailableError on 503', async () => {
    fetchMock.mockResolvedValue(errResp(503, 'xkms not configured'));
    const client = new RestKnownHostsClient('http://localhost');
    await expect(
      client.verify('server.example.com', { fingerprint: 'SHA256:xyz' }),
    ).rejects.toBeInstanceOf(SSHUnavailableError);
  });

  // Surfaces the client via the top-level RestClient.ssh.knownHosts accessor.
  it('client.ssh.knownHosts is accessible from RestClient', async () => {
    fetchMock.mockResolvedValue(jsonResp({ items: [] }));
    const client = new RestClient('http://localhost');
    const resp = await client.ssh.knownHosts.list();
    expect(resp.items).toHaveLength(0);
  });
});

// =============================================================================
// RestAuthorizedKeysClient
// =============================================================================

describe('RestAuthorizedKeysClient', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('emit POSTs to /api/v1/ssh/authorized and returns line', async () => {
    fetchMock.mockResolvedValue(jsonResp({ line: 'ssh-ed25519 AAA alice@quicbaas' }));
    const client = new RestAuthorizedKeysClient('http://localhost');
    const resp = await client.emit({
      public_key: 'ssh-ed25519 AAA',
      options: ['restrict'],
      comment: 'alice@quicbaas',
    });
    expect(resp.line).toBe('ssh-ed25519 AAA alice@quicbaas');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/authorized');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body.public_key).toBe('ssh-ed25519 AAA');
    expect(body.options).toEqual(['restrict']);
  });

  it('emit throws SSHInvalidError on 400', async () => {
    fetchMock.mockResolvedValue(errResp(400, 'bad request'));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await expect(client.emit({ public_key: '' })).rejects.toBeInstanceOf(SSHInvalidError);
  });

  it('verify POSTs to /api/v1/ssh/authorized/verify and wraps entry in response', async () => {
    const entryFixture = {
      scope: 'user',
      owner: 'alice',
      name: 'laptop',
      line: 'ssh-ed25519 AAA laptop@quicbaas',
    };
    fetchMock.mockResolvedValue(jsonResp(entryFixture));
    const client = new RestAuthorizedKeysClient('http://localhost');
    const resp = await client.verify({
      fingerprint: 'SHA256:abc',
      scope: 'user',
      owner: 'alice',
    });
    expect(resp.entry.name).toBe('laptop');
    expect(resp.entry.line).toContain('ssh-ed25519');
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/ssh/authorized/verify');
    expect(init.method).toBe('POST');
  });

  it('verify throws SSHNotFoundError on 404', async () => {
    fetchMock.mockResolvedValue(errResp(404, 'authorized key not found', { resource: 'authorized_key' }));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await expect(
      client.verify({ fingerprint: 'SHA256:nope' }),
    ).rejects.toBeInstanceOf(SSHNotFoundError);
  });

  it('verify throws SSHUnavailableError on 503', async () => {
    fetchMock.mockResolvedValue(errResp(503, 'xkms not configured'));
    const client = new RestAuthorizedKeysClient('http://localhost');
    await expect(
      client.verify({ fingerprint: 'SHA256:x' }),
    ).rejects.toBeInstanceOf(SSHUnavailableError);
  });

  it('client.ssh.authorized is accessible from RestClient', async () => {
    fetchMock.mockResolvedValue(jsonResp({ line: 'ssh-ed25519 AAA' }));
    const client = new RestClient('http://localhost');
    const resp = await client.ssh.authorized.emit({ public_key: 'ssh-ed25519 AAA' });
    expect(resp.line).toBe('ssh-ed25519 AAA');
  });
});

// =============================================================================
// Error type mapping — exhaustive status-to-class table
// =============================================================================

describe('SSH error type mapping', () => {
  const originalFetch = globalThis.fetch;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  const errorCases: Array<[number, Record<string, string>, unknown]> = [
    [400, { error: 'bad input' }, SSHInvalidError],
    [404, { error: 'not found' }, SSHNotFoundError],
    [409, { error: 'duplicate' }, SSHDuplicateError],
    [503, { error: 'unavailable' }, SSHUnavailableError],
    [500, { error: 'internal' }, SSHServerError],
    [502, { error: 'bad gateway' }, SSHServerError],
  ];

  for (const [status, body, ErrorClass] of errorCases) {
    it(`status ${status} → ${(ErrorClass as { name: string }).name}`, async () => {
      fetchMock.mockResolvedValue(jsonResp(body, status));
      const client = new RestSSHKeysClient('http://localhost');
      await expect(client.getScoped('user', 'alice', 'test')).rejects.toBeInstanceOf(
        ErrorClass as new (...args: unknown[]) => Error,
      );
    });
  }

  it('409 with host+expected+received → SSHHostMismatchError', async () => {
    fetchMock.mockResolvedValue(
      jsonResp(
        {
          error: 'ssh/known_hosts: host key mismatch',
          resource: 'known_hosts',
          host: 'badhost',
          expected: 'SHA256:good',
          received: 'SHA256:bad',
        },
        409,
      ),
    );
    const client = new RestKnownHostsClient('http://localhost');
    let caught: SSHHostMismatchError | undefined;
    try {
      await client.verify('badhost', { fingerprint: 'SHA256:bad' });
    } catch (e) {
      caught = e as SSHHostMismatchError;
    }
    expect(caught).toBeInstanceOf(SSHHostMismatchError);
    expect(caught!.host).toBe('badhost');
    expect(caught!.expected).toBe('SHA256:good');
    expect(caught!.received).toBe('SHA256:bad');
    expect(caught!.status).toBe(409);
    expect(caught!.resource).toBe('known_hosts');
  });
});
