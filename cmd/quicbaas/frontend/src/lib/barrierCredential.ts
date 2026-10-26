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
 * keys on.
 *
 * A barrier credential is a byte string, not text: go-qrdb types every one of
 * them `[]byte` so it can be wiped. The SDK's TenantBarrierClient takes those
 * bytes as a Uint8Array and base64-encodes them for the wire itself (see
 * docs/barrier/api.md), so the UI hands it the passphrase's UTF-8 bytes and
 * never encodes a second time.
 *
 * Init and unseal must derive the same bytes from the same typed text, or a
 * tenant barrier initialized through the UI cannot be unsealed through it
 * (go-quicbaas#988). Both route through this module, and the tests hold them
 * to the same bytes.
 */

import type { TenantBarrierInitRequest } from '@quicbaas/sdk';
import type { TenantBarrierInitParams } from '@quicbaas/svelte-security';

/** barrierCredentialBytes returns the UTF-8 bytes of a typed passphrase. */
export function barrierCredentialBytes(passphrase: string): Uint8Array {
  return new TextEncoder().encode(passphrase);
}

/**
 * toInitRequest maps the picker's init params onto the SDK init request,
 * carrying a passphrase as its UTF-8 bytes and leaving the credential-free
 * strategies untouched.
 */
export function toInitRequest(params: TenantBarrierInitParams): TenantBarrierInitRequest {
  if (params.strategy !== 'passphrase') return params;
  return { strategy: 'passphrase', passphrase: barrierCredentialBytes(params.passphrase) };
}
