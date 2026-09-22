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
 * The one place the UI turns a typed passphrase into the bytes the barrier
 * wire carries.
 *
 * A barrier credential is a byte string, not text: go-qrdb types every one of
 * them `[]byte` so it can be wiped, and `encoding/json` renders a `[]byte` as
 * base64 — so `secret` / `passphrase` / `key` are base64 on the wire (see
 * docs/barrier/api.md). The server rejects MALFORMED base64 and nothing else,
 * which is precisely why this belongs in one function: a caller that forgets
 * to encode does not reliably get an error. `password` is valid base64, so it
 * is accepted and the barrier is keyed on six bytes nobody typed.
 *
 * Init and unseal each used to have their own answer here — unseal encoded,
 * init forwarded the picker's plaintext — so a tenant barrier initialized
 * through the UI could not be unsealed through it (go-quicbaas#988). Both now
 * route through this module, and the tests hold them to the same encoding.
 */

import type { TenantBarrierInitParams } from '@quicbaas/svelte-security';

/** encodeBarrierCredential renders a typed passphrase as the base64 bytes the
 * barrier wire carries. */
export function encodeBarrierCredential(passphrase: string): string {
  const bytes = new TextEncoder().encode(passphrase);
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

/**
 * encodeInitParams returns the init payload with its credential encoded,
 * leaving the credential-free strategies untouched.
 *
 * It exists as a function rather than three lines inside the view's submit
 * handler so the property that matters — init and unseal encode the same typed
 * text identically — is reachable by a test.
 */
export function encodeInitParams(
  params: TenantBarrierInitParams,
): TenantBarrierInitParams {
  if (params.strategy !== 'passphrase') return params;
  return { ...params, passphrase: encodeBarrierCredential(params.passphrase) };
}
