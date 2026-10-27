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

import { describe, it, expect } from 'vitest';
import { decodeShareEnvelope, ShareEnvelopeFormatError } from '../src/lib/shareEnvelope.ts';

// envelope is a share envelope in qrdb's canonical format
// (pkg/ceremony/envelope.go ShareEnvelope).
function envelope(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    version: 1,
    kem: 'x25519-hkdf-sha256',
    holder: 'alice',
    share_index: 2,
    ciphertext: 'Y2lwaGVydGV4dA==',
    ...over,
  };
}

// wire encodes an envelope the way the REST transport sends a Go []byte.
function wire(value: unknown): string {
  return btoa(JSON.stringify(value));
}

describe('decodeShareEnvelope', () => {
  it('returns the exact envelope bytes the base64 field carries', () => {
    const json = JSON.stringify(envelope());
    const bytes = decodeShareEnvelope(btoa(json));
    expect(new TextDecoder().decode(bytes)).toBe(json);
  });

  it('keeps bytes outside ASCII intact', () => {
    const raw = new TextEncoder().encode(JSON.stringify(envelope({ holder: 'zoë' })));
    const b64 = btoa(String.fromCharCode(...raw));
    expect(Array.from(decodeShareEnvelope(b64))).toEqual(Array.from(raw));
  });

  it.each([
    ['text that is not base64', '%%%'],
    ['bytes that are not JSON', btoa('env')],
    ['a JSON value that is not an object', wire([1])],
    ['another envelope version', wire(envelope({ version: 2 }))],
    ['an envelope with no ciphertext', wire(envelope({ ciphertext: '' }))],
  ])('refuses %s', (_name, b64) => {
    expect(() => decodeShareEnvelope(b64)).toThrow(ShareEnvelopeFormatError);
  });
});
