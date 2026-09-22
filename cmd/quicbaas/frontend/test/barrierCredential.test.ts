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
 * forgets to encode is not reliably told off. TenantBarrierView had one of
 * each: unseal encoded the typed passphrase, init forwarded the picker's
 * plaintext. A tenant barrier initialized through the UI therefore could not
 * be unsealed through it, and a passphrase that happens to be valid base64
 * initialized silently with bytes nobody typed (go-quicbaas#988).
 *
 * These tests hold the two paths to one encoding.
 */

import { describe, it, expect } from 'vitest';
import {
  encodeBarrierCredential,
  encodeInitParams,
} from '../src/lib/barrierCredential.ts';

describe('encodeBarrierCredential', () => {
  it('renders the typed text as the bytes the wire carries', () => {
    expect(encodeBarrierCredential('hunter2')).toBe('aHVudGVyMg==');
    expect(atob(encodeBarrierCredential('hunter2'))).toBe('hunter2');
  });

  it('encodes a multi-byte passphrase by its UTF-8 bytes, not its characters', () => {
    // btoa() alone throws on a code point above U+00FF; the TextEncoder step
    // is what makes a non-ASCII passphrase usable at all.
    const encoded = encodeBarrierCredential('pässwörd–ü');
    const bytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
    expect(new TextDecoder().decode(bytes)).toBe('pässwörd–ü');
  });

  it('encodes the empty passphrase to the empty credential', () => {
    expect(encodeBarrierCredential('')).toBe('');
  });
});

describe('encodeInitParams', () => {
  it('encodes the passphrase strategy', () => {
    const out = encodeInitParams({ strategy: 'passphrase', passphrase: 'hunter2' });
    expect(out).toEqual({ strategy: 'passphrase', passphrase: 'aHVudGVyMg==' });
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
      const initCredential =
        onInit.strategy === 'passphrase' ? onInit.passphrase : undefined;

      // What handlePassphraseUnseal sends for the same typed text.
      const unsealCredential = encodeBarrierCredential(passphrase);

      expect(initCredential).toBe(unsealCredential);
      expect(initCredential).not.toBe(passphrase === '' ? 'x' : passphrase);
    });
  }
});
