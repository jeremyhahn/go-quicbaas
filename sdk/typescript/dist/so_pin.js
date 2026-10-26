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
import { SOPINRequiresTLSError } from './errors.js';
/**
 * requireTLSForSOPIN throws SOPINRequiresTLSError when soPIN is nonempty and
 * endpoint is not https://. Every transport calls it before putting a PIN in
 * a request. The rule is the Go SDK's (sdk/go/transport/mcp): a plain
 * "https://" prefix check with no loopback exception. A call without a PIN
 * is never refused.
 */
export function requireTLSForSOPIN(transport, endpoint, soPIN) {
    requireTLSForSecret(transport, endpoint, Boolean(soPIN));
}
/**
 * requireTLSForSecret throws SOPINRequiresTLSError when hasSecret is true and
 * endpoint is not https://. It is the Go MCP transport's requireTLSForSecret:
 * a verb that carries any barrier secret (an SO or user PIN, an initialize or
 * unseal secret, a passphrase or a decrypted key share) calls it before the
 * request is built, so the secret never crosses a plaintext connection. The
 * error never contains the secret.
 */
export function requireTLSForSecret(transport, endpoint, hasSecret) {
    if (hasSecret && !endpoint.startsWith('https://')) {
        throw new SOPINRequiresTLSError(transport);
    }
}
//# sourceMappingURL=so_pin.js.map