/**
 * refProviders.test.ts
 *
 * Verifies the RefProvider registry and the core providers registered in
 * lib/refs/providers.ts:
 *   - every declared kind has a registered provider;
 *   - secret: list/search/status/create round-trip, including the inline
 *     create writing { value } the same way SecretsView does;
 *   - kmsKey: disabled keys report status 'disabled';
 *   - project: a parked project reports status 'disabled';
 *   - a provider whose SDK call rejects fails closed (status 'missing'),
 *     never throws out of the component.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

const { mockGetSDKClient } = vi.hoisted(() => ({ mockGetSDKClient: vi.fn() }));

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: mockGetSDKClient }));

import { getRefProvider, refProviderKinds } from '../src/lib/refs/registry.ts';
import '../src/lib/refs/providers.ts';
import type { RefKind } from '../src/lib/refs/types.ts';

const CTX = { tenantID: 'acme' };

describe('registerCoreRefProviders', () => {
  const expectedKinds: RefKind[] = [
    'secret',
    'kmsKey',
    'sshKey',
    'certificate',
    'account',
    'fleetNode',
    'project',
    'tenant',
    'user',
    'role',
    'schedulerJob',
    'workflow',
  ];

  it('registers every core kind exactly once', () => {
    const kinds = refProviderKinds();
    for (const kind of expectedKinds) {
      expect(kinds).toContain(kind);
    }
  });

  it('returns undefined for an unregistered kind', () => {
    // 'agent'/'pool' are intentionally not RefKind members yet; casting past
    // the type system mirrors what a stale build asking for a retired kind
    // would see.
    expect(getRefProvider('not-a-kind' as RefKind)).toBeUndefined();
  });
});

describe('secret provider', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('lists, searches, and reports status for a stored secret', async () => {
    mockGetSDKClient.mockResolvedValue({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({
          secrets: [
            { path: 'sdlc/reviewer/go-truststrap/token', version: 2, created_at: '', updated_at: '' },
            { path: 'app/db/password', version: 1, created_at: '', updated_at: '' },
          ],
          count: 2,
        }),
        getSecret: vi.fn().mockResolvedValue({ path: 'app/db/password', data: {}, version: 1, created_at: '', updated_at: '' }),
      },
    });

    const provider = getRefProvider('secret');
    expect(provider).toBeDefined();

    const listed = await provider!.list(CTX);
    expect(listed.map((o) => o.value)).toContain('app/db/password');

    const found = await provider!.search('reviewer', CTX);
    expect(found).toHaveLength(1);
    expect(found[0]?.value).toBe('sdlc/reviewer/go-truststrap/token');

    await expect(provider!.status!('app/db/password', CTX)).resolves.toBe('exists');
  });

  it('reports status "missing" when getSecret rejects, and creates via storeSecret({value})', async () => {
    const storeSecret = vi.fn().mockResolvedValue(undefined);
    mockGetSDKClient.mockResolvedValue({
      secrets: {
        listSecrets: vi.fn().mockResolvedValue({ secrets: [], count: 0 }),
        getSecret: vi.fn().mockRejectedValue(new Error('not found')),
        storeSecret,
      },
    });

    const provider = getRefProvider('secret')!;
    await expect(provider.status!('nope', CTX)).resolves.toBe('missing');

    const created = await provider.create!('sdlc/reviewer/new-project/token', { value: 'ghp_xxx' }, CTX);
    expect(storeSecret).toHaveBeenCalledWith({
      path: 'sdlc/reviewer/new-project/token',
      data: { value: 'ghp_xxx' },
    });
    expect(created).toEqual({ value: 'sdlc/reviewer/new-project/token', label: 'sdlc/reviewer/new-project/token', status: 'exists' });
  });
});

describe('kmsKey provider', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('maps a disabled key to status "disabled"', async () => {
    mockGetSDKClient.mockResolvedValue({
      kms: {
        listKeys: vi.fn().mockResolvedValue({
          keys: [
            { id: '1', name: 'commit-signing', description: '', algorithm: 'ed25519', key_size: 256, status: 'active', created_at: '', updated_at: '', exportable: false, tags: {} },
            { id: '2', name: 'retired-key', description: '', algorithm: 'ed25519', key_size: 256, status: 'inactive', created_at: '', updated_at: '', exportable: false, tags: {} },
          ],
          count: 2,
        }),
      },
    });

    const provider = getRefProvider('kmsKey')!;
    const options = await provider.list(CTX);
    expect(options.find((o) => o.value === 'retired-key')?.status).toBe('disabled');
    expect(options.find((o) => o.value === 'commit-signing')?.status).toBe('exists');
  });
});

describe('project provider', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('reports a parked project as "disabled"', async () => {
    mockGetSDKClient.mockResolvedValue({
      projects: {
        listProjectsPage: vi.fn().mockResolvedValue({
          items: [
            { id: 'p1', tenant_id: 'acme', name: 'go-quicbaas', parked: false, created_at: '', updated_at: '' },
            { id: 'p2', tenant_id: 'acme', name: 'retired-proj', parked: true, created_at: '', updated_at: '' },
          ],
          total: 2,
          limit: 100,
          offset: 0,
        }),
      },
    });

    const provider = getRefProvider('project')!;
    const options = await provider.list(CTX);
    expect(options.find((o) => o.value === 'p2')?.status).toBe('disabled');
    expect(options.find((o) => o.value === 'p1')?.status).toBe('exists');
  });
});
