// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

/**
 * A barrier credential is base64 BYTES on the wire (docs/barrier/api.md), and
 * the server rejects malformed base64 and nothing else — so a call site that
 * forgets to encode, or encodes twice, is not reliably told off.
 * TenantBarrierView had one of each in turn: first init forwarded the
 * picker's plaintext while unseal encoded (go-quicbaas#988); then both handed
 * the SDK an already-base64 string, which the SDK's own encoder turned into
 * all-zero bytes (go-quicbaas#2372).
 *
 * These tests hold the two paths to one set of bytes, and the wire tests hold
 * the SDK to the one base64 encoding of them.
 */

import { describe, it, expect, vi, afterEach } from 'vitest';
import { RestTenantBarrierClient } from '@quicbaas/sdk';
import {
  encodeBarrierCredential,
  encodeInitParams,
} from '../src/lib/barrierCredential.ts';

const utf8 = (s: string): Uint8Array => new TextEncoder().encode(s);

describe('encodeBarrierCredential', () => {
  it('renders the typed text as its bytes', () => {
    expect(encodeBarrierCredential('hunter2')).toEqual(utf8('hunter2'));
    expect(new TextDecoder().decode(encodeBarrierCredential('hunter2'))).toBe('hunter2');
  });

  it('encodes a multi-byte passphrase by its UTF-8 bytes, not its characters', () => {
    const encoded = encodeBarrierCredential('pässwörd–ü');
    // Compare byte values: the test DOM's TextEncoder returns a Uint8Array
    // from another realm, so an instanceof or cross-realm toEqual misleads.
    expect(Array.from(encoded)).toEqual(Array.from(Buffer.from('pässwörd–ü', 'utf8')));
    expect(encoded.length).toBe(Buffer.byteLength('pässwörd–ü', 'utf8'));
    expect(new TextDecoder().decode(encoded)).toBe('pässwörd–ü');
  });

  it('encodes the empty passphrase to the empty credential', () => {
    expect(encodeBarrierCredential('').length).toBe(0);
  });
});

describe('encodeInitParams', () => {
  it('encodes the passphrase strategy', () => {
    const out = encodeInitParams({ strategy: 'passphrase', passphrase: 'hunter2' });
    expect(out).toEqual({ strategy: 'passphrase', passphrase: utf8('hunter2') });
  });

  it('leaves the credential-free strategies untouched', () => {
    expect(encodeInitParams({ strategy: 'none' })).toEqual({ strategy: 'none' });
    expect(encodeInitParams({ strategy: 'shamir', threshold: 2, shares: 3 })).toEqual({
      strategy: 'shamir',
      threshold: 2,
      shares: 3,
    });
  });

  it('does not mutate the params the picker emitted', () => {
    const params = { strategy: 'passphrase', passphrase: 'hunter2' } as const;
    encodeInitParams(params);
    expect(params.passphrase).toBe('hunter2');
  });
});

describe('init and unseal agree on the credential', () => {
  // The regression itself: whatever the operator types, the bytes init sends
  // must be the bytes unseal sends, or the barrier cannot be reopened through
  // the UI that created it.
  const typed = [
    'hunter2',
    '',
    'pässwörd–ü',
    'a passphrase with spaces and $ymbo!s',
    // The hazard case. "password" is eight characters of the base64 alphabet,
    // so it is well-formed base64: sent raw it is ACCEPTED and decodes to six
    // bytes nobody typed. Nothing rejects it — only encoding both sides does.
    'password',
  ];

  for (const passphrase of typed) {
    it(`encodesTheSameForInitAndUnseal: ${JSON.stringify(passphrase)}`, () => {
      const onInit = encodeInitParams({ strategy: 'passphrase', passphrase });
      expect(onInit.strategy).toBe('passphrase');
      const initCredential = onInit.passphrase;

      // What handlePassphraseUnseal sends for the same typed text.
      const unsealCredential = encodeBarrierCredential(passphrase);

      expect(initCredential).toEqual(unsealCredential);
      expect(initCredential).toEqual(utf8(passphrase));
    });
  }
});

describe('the tenant barrier wire carries the typed bytes', () => {
  // The SDK base64-encodes the bytes it is given. Handing it an
  // already-base64 string sent all-zero bytes ("cGFzcw==" went out as
  // "AAAAAAAAAAA="), so every passphrase of one length keyed the same barrier
  // (go-quicbaas#2372). These drive the real RestTenantBarrierClient and read
  // the request body off a stubbed fetch.
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function captureBodies(): Map<string, Record<string, unknown>> {
    const bodies = new Map<string, Record<string, unknown>>();
    const fetchStub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (typeof init?.body === 'string') {
        const path = url.endsWith('/init') ? 'init' : url.endsWith('/unseal') ? 'unseal' : url;
        bodies.set(path, JSON.parse(init.body) as Record<string, unknown>);
      }
      return new Response(null, { status: 204 });
    });
    vi.stubGlobal('fetch', fetchStub);
    return bodies;
  }

  async function wireFor(passphrase: string): Promise<{ init: unknown; unseal: unknown }> {
    const bodies = captureBodies();
    const tb = new RestTenantBarrierClient('http://x');
    await tb.tenantBarrierInit('t1', encodeInitParams({ strategy: 'passphrase', passphrase }));
    await tb.tenantBarrierUnseal('t1', encodeBarrierCredential(passphrase));
    return { init: bodies.get('init')?.passphrase, unseal: bodies.get('unseal')?.key };
  }

  it('wireCarriesTheTypedBytes: hunter2 goes out as its own base64', async () => {
    const wire = await wireFor('hunter2');
    expect(wire.init).toBe('aHVudGVyMg==');
    expect(wire.unseal).toBe('aHVudGVyMg==');
  });

  it('wireCarriesTheTypedBytes: a multi-byte passphrase goes out as its UTF-8 base64', async () => {
    const want = Buffer.from('pässwörd', 'utf8').toString('base64');
    const wire = await wireFor('pässwörd');
    expect(wire.init).toBe(want);
    expect(wire.unseal).toBe(want);
    expect(wire.init).not.toMatch(/^A+=*$/);
    expect(wire.unseal).not.toMatch(/^A+=*$/);
  });
});
