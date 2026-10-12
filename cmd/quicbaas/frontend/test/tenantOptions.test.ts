/**
 * Tests for the tenant selector option helpers (#309, #312): the session
 * tenant is always offered and a listing only ever adds entries.
 */

import { describe, it, expect } from 'vitest';
import { tenantOptionsOf, withRequiredTenants } from '../src/lib/tenantOptions.ts';

describe('tenantOptionsOf', () => {
  it('labels each tenant by name, falling back to its id', () => {
    expect(tenantOptionsOf([{ id: 'acme', name: 'Acme Corp' }, { id: 'globex' }])).toEqual([
      { id: 'acme', name: 'Acme Corp' },
      { id: 'globex', name: 'globex' },
    ]);
  });

  it('drops repeated ids, keeping the first', () => {
    expect(tenantOptionsOf([{ id: 'acme', name: 'first' }, { id: 'acme', name: 'second' }])).toEqual([
      { id: 'acme', name: 'first' },
    ]);
  });
});

describe('withRequiredTenants', () => {
  it('prepends a required tenant the listing lacks', () => {
    expect(withRequiredTenants([{ id: 'globex', name: 'globex' }], ['acme'])).toEqual([
      { id: 'acme', name: 'acme' },
      { id: 'globex', name: 'globex' },
    ]);
  });

  it('adds nothing for an empty or already-listed id, and never invents system', () => {
    const listed = [{ id: 'acme', name: 'Acme Corp' }];
    const options = withRequiredTenants(listed, ['', 'acme']);
    expect(options).toEqual(listed);
    expect(options).not.toBe(listed);
    expect(options.some((t) => t.id === 'system')).toBe(false);
  });
});
