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

package iam

import "strings"

// OAuth2 client identifier grammar.
const (
	// ClientIDPrefix opens every generated client identifier.
	ClientIDPrefix = "app_"

	// ClientIDSeparator divides the tenant segment from the random suffix.
	ClientIDSeparator = "_"
)

// TenantFromClientID extracts the tenant from an OAuth2 client identifier
// minted in the ClientIDPrefix + <tenant> + ClientIDSeparator + <suffix> shape.
// It returns "" when the identifier does not carry that shape, so callers can
// fall back to the tenant on the context.
func TenantFromClientID(clientID string) string {
	if !strings.HasPrefix(clientID, ClientIDPrefix) {
		return ""
	}
	rest := clientID[len(ClientIDPrefix):]
	idx := strings.LastIndex(rest, ClientIDSeparator)
	if idx <= 0 {
		return ""
	}
	return rest[:idx]
}
