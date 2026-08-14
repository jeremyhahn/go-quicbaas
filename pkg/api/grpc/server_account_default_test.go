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

package grpc

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestAccountToProto_CarriesDerivedDefault pins the gRPC projection of the
// pooled-account default.
//
// Account.Default is DERIVED from the store's per-scope default pointer rather
// than persisted, so every transport that reprojects an Account must forward the
// value it was handed. A transport that dropped it would report no default at
// all for a tenant that has one, and `account list` would show a pool where
// nothing is selectable — while an unpinned spawn on the server resolved fine.
func TestAccountToProto_CarriesDerivedDefault(t *testing.T) {
	for _, isDefault := range []bool{true, false} {
		got := accountToProto(&orchestration.Account{
			ID:       "acct-1",
			TenantID: "tenant-a",
			Provider: orchestration.AccountProviderClaude,
			Default:  isDefault,
		})
		if got == nil {
			t.Fatalf("accountToProto() = nil")
		}
		if got.IsDefault != isDefault {
			t.Fatalf("IsDefault = %v, want %v", got.IsDefault, isDefault)
		}
	}
}

// TestAccountToProto_NilIsNil is the error-path half: a nil account must not
// panic on a field access.
func TestAccountToProto_NilIsNil(t *testing.T) {
	if got := accountToProto(nil); got != nil {
		t.Fatalf("accountToProto(nil) = %v, want nil", got)
	}
}
