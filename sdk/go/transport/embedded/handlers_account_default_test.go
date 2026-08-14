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

package embedded

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestAccountInfoFromOrchestration_CarriesDerivedDefault pins the embedded
// (in-process) projection of the pooled-account default. Default is derived
// from the store's per-scope pointer, so each transport must forward what it was
// handed or the same pool reports a different default per protocol.
func TestAccountInfoFromOrchestration_CarriesDerivedDefault(t *testing.T) {
	for _, isDefault := range []bool{true, false} {
		got := accountInfoFromOrchestration(&orchestration.Account{
			ID:       "acct-1",
			TenantID: "tenant-a",
			Provider: orchestration.AccountProviderClaude,
			Default:  isDefault,
		})
		if got == nil {
			t.Fatalf("accountInfoFromOrchestration() = nil")
		}
		if got.Default != isDefault {
			t.Fatalf("Default = %v, want %v", got.Default, isDefault)
		}
	}
}

// TestAccountInfoFromOrchestration_NilIsNil is the error-path half.
func TestAccountInfoFromOrchestration_NilIsNil(t *testing.T) {
	if got := accountInfoFromOrchestration(nil); got != nil {
		t.Fatalf("accountInfoFromOrchestration(nil) = %v, want nil", got)
	}
}
