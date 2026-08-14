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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestAccountToREST_CarriesDerivedDefault pins the REST projection of the
// pooled-account default. See the gRPC twin for why this matters: Default is
// derived from the store's per-scope pointer, so each transport must forward
// what it was handed or the same pool reports a different default per protocol.
func TestAccountToREST_CarriesDerivedDefault(t *testing.T) {
	for _, isDefault := range []bool{true, false} {
		got := accountToREST(&orchestration.Account{
			ID:       "acct-1",
			TenantID: "tenant-a",
			Provider: orchestration.AccountProviderClaude,
			Default:  isDefault,
		})
		if got == nil {
			t.Fatalf("accountToREST() = nil")
		}
		if got.Default != isDefault {
			t.Fatalf("Default = %v, want %v", got.Default, isDefault)
		}
	}
}

// TestAccountToREST_NilIsNil is the error-path half.
func TestAccountToREST_NilIsNil(t *testing.T) {
	if got := accountToREST(nil); got != nil {
		t.Fatalf("accountToREST(nil) = %v, want nil", got)
	}
}
