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
 * A barrier credential is bytes (docs/barrier/api.md). The SDK's
 * TenantBarrierClient takes them as a Uint8Array and base64-encodes them once
 * for the wire, so the UI must hand it raw UTF-8 bytes on both init and unseal.
 * TenantBarrierView once pre-encoded the init passphrase to a base64 string,
 * which the SDK then coerced to different bytes, so a tenant barrier
 * initialized through the UI could not be unsealed through it
 * (go-quicbaas#988).
 *
 * These tests hold the two paths to the same bytes.
 */

import { describe, it, expect } from 'vitest';
import { barrierCredentialBytes, toInitRequest } from '../src/lib/barrierCredential.ts';

describe('barrierCredentialBytes', () => {
  it('returns the UTF-8 bytes of the typed text', () => {
    expect(barrierCredentialBytes('hunter2')).toEqual(new TextEncoder().encode('hunter2'));
  });

  it('encodes a multi-byte passphrase by its UTF-8 bytes, not its characters', () => {
    const bytes = barrierCredentialBytes('pässwörd–ü');
    expect(bytes.length).toBeGreaterThan('pässwörd–ü'.length);
    expect(new TextDecoder().decode(bytes)).toBe('pässwörd–ü');
  });

  it('returns no bytes for the empty passphrase', () => {
    expect(barrierCredentialBytes('')).toHaveLength(0);
  });
});

describe('toInitRequest', () => {
  it('carries the passphrase strategy as bytes, never a pre-encoded string', () => {
    const out = toInitRequest({ strategy: 'passphrase', passphrase: 'hunter2' });
    expect(out.strategy).toBe('passphrase');
    // jsdom's TextEncoder returns another realm's Uint8Array, so check the
    // shape rather than instanceof.
    expect(typeof out.passphrase).not.toBe('string');
    expect(ArrayBuffer.isView(out.passphrase)).toBe(true);
    expect(new TextDecoder().decode(out.passphrase)).toBe('hunter2');
  });

  it('leaves the credential-free strategies untouched', () => {
    expect(toInitRequest({ strategy: 'none' })).toEqual({ strategy: 'none' });
    expect(toInitRequest({ strategy: 'shamir', threshold: 2, shares: 3 })).toEqual({
      strategy: 'shamir',
      threshold: 2,
      shares: 3,
    });
  });

  it('does not mutate the params the picker emitted', () => {
    const params = { strategy: 'passphrase', passphrase: 'hunter2' } as const;
    toInitRequest(params);
    expect(params.passphrase).toBe('hunter2');
  });
});

describe('init and unseal agree on the credential', () => {
  // The regression itself: whatever the operator types, the bytes init hands
  // the SDK must be the bytes unseal hands it, or the barrier cannot be
  // reopened through the UI that created it.
  const typed = [
    'hunter2',
    '',
    'pässwörd–ü',
    'a passphrase with spaces and $ymbo!s',
    // "password" is well-formed base64, so a stray decode or encode step on
    // one side would go unnoticed by the server.
    'password',
  ];

  for (const passphrase of typed) {
    it(`passes the same bytes to init and unseal: ${JSON.stringify(passphrase)}`, () => {
      const initBytes = toInitRequest({ strategy: 'passphrase', passphrase }).passphrase;
      const unsealBytes = barrierCredentialBytes(passphrase);
      expect(initBytes).toEqual(unsealBytes);
    });
  }
});
