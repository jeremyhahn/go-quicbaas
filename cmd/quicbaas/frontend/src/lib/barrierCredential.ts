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
 * which is precisely why the encoding belongs in exactly one place: a caller
 * that forgets to encode, or encodes twice, does not reliably get an error.
 *
 * That one place is the SDK. RestTenantBarrierClient takes the passphrase and
 * the unseal key as `Uint8Array` and base64-encodes them itself, so this
 * module hands it the typed text's UTF-8 BYTES and nothing more. It used to
 * hand it a base64 string instead; the SDK's encoder read each character as a
 * NaN byte, and the server received as many zero bytes as the string had
 * characters — every passphrase of one length keyed the same barrier
 * (go-quicbaas#2372).
 *
 * Init and unseal each used to have their own answer here — unseal encoded,
 * init forwarded the picker's plaintext — so a tenant barrier initialized
 * through the UI could not be unsealed through it (go-quicbaas#988). Both
 * still route through the one encoder in this module, now as bytes, and the
 * tests hold them to the same bytes and the same wire.
 */

import type { TenantBarrierInitRequest } from '@quicbaas/sdk';
import type { TenantBarrierInitParams } from '@quicbaas/svelte-security';

/** encodeBarrierCredential renders a typed passphrase as its UTF-8 bytes, the
 * credential the SDK base64-encodes onto the barrier wire. */
export function encodeBarrierCredential(passphrase: string): Uint8Array {
  return new TextEncoder().encode(passphrase);
}

/**
 * encodeInitParams returns the SDK init request for the picker's params, with
 * the passphrase as its UTF-8 bytes (the SDK does the one base64 encoding),
 * leaving the credential-free strategies untouched and the input unmutated.
 *
 * It exists as a function rather than three lines inside the view's submit
 * handler so the property that matters — init and unseal send the same typed
 * text as the same bytes — is reachable by a test.
 */
export function encodeInitParams(
  params: TenantBarrierInitParams,
): TenantBarrierInitRequest {
  if (params.strategy !== 'passphrase') return params;
  return { ...params, passphrase: encodeBarrierCredential(params.passphrase) };
}
