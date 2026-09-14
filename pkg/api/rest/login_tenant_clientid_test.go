//go:build rest

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

package rest

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestTenantFromOAuthClientID_AgreesWithTheMinter pins the transport-side
// parser to the one pkg/iam mints and stores with. Two copies of the same
// grammar is how go-quicbaas#644 could have half-healed: the token endpoint
// scopes its storage context with this function while the store resolves the
// record with iam.TenantFromClientID, so a disagreement puts the read in a
// namespace the write never touched.
func TestTenantFromOAuthClientID_AgreesWithTheMinter(t *testing.T) {
	for _, clientID := range []string{
		"app_automatethethings_QQnqeujdEo",
		"app_automatethethings_o9ufKgB_s9c",
		"app_with_underscore_ABC234",
		"app_acme_",
		"app_",
		"app__suffix",
		"agent-automatethethings-node-mtls",
		"",
	} {
		t.Run(clientID, func(t *testing.T) {
			want := iam.TenantFromClientID(clientID)
			if got := tenantFromOAuthClientID(clientID); got != want {
				t.Fatalf("tenantFromOAuthClientID(%q) = %q, iam.TenantFromClientID = %q", clientID, got, want)
			}
		})
	}
}
