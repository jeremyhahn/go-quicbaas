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

package mcp

import "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

// secretsGetDataMethod is the internal wire verb behind the secrets client's
// GetWithBytes: a read whose payload must reach the SDK as bytes it can wipe.
const secretsGetDataMethod = "GetData"

// isSecretsGetData reports whether service/method is the protected secrets
// read the MCP transport refuses.
func isSecretsGetData(service, method string) bool {
	return service == "quicbaas.Secrets" && method == secretsGetDataMethod
}

// newSecretsGetDataUnsupportedError refuses Client.GetWithBytes on MCP.
//
// MCP returns every tool result as text content, which the MCP client library
// decodes into a Go string before this transport sees it. The plaintext would
// therefore be an ordinary, unwipeable string inside the SDK — exactly what
// GetWithBytes exists to prevent (security-review.md §6) — so the transport
// fails closed instead of lending material it could not protect. Use the
// gRPC, Unix, REST, QUIC or embedded transport for secret material.
func newSecretsGetDataUnsupportedError() error {
	return errors.New(errors.CodeUnimplemented, "mcp.secrets.GetWithBytes",
		"protected secret reads are not supported by the MCP transport: MCP delivers tool results as text strings, so the secret could not be kept out of unprotected memory")
}
