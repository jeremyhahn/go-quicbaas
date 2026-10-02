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

package spiffe_test

import (
	"errors"
	"testing"

	xkmsspiffe "github.com/jeremyhahn/go-xkms/sdk/go/spiffe"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/spiffe"
)

// Compile-time proof the aliases are the go-xkms types.
var (
	_ xkmsspiffe.ID              = spiffe.ID{}
	_ spiffe.ID                  = xkmsspiffe.ID{}
	_ xkmsspiffe.WorkloadID      = spiffe.WorkloadID{}
	_ xkmsspiffe.TrustDomain     = spiffe.TrustDomain{}
	_ *xkmsspiffe.InvalidIDError = (*spiffe.InvalidIDError)(nil)
)

// TestBuildURIParseID_RoundTrip proves the tenant-scoped builder and parser
// agree, which is what lets a CA stamp an SVID and a relying party read the
// tenant back out of it.
func TestBuildURIParseID_RoundTrip(t *testing.T) {
	u, err := spiffe.BuildURI("example.org", "acme", spiffe.KindService, "payments")
	if err != nil {
		t.Fatalf("BuildURI: %v", err)
	}
	id, ok, err := spiffe.ParseID(u)
	if err != nil || !ok {
		t.Fatalf("ParseID(%s) = %v, %v", u, ok, err)
	}
	if id.TenantID != "acme" || id.Name != "payments" {
		t.Fatalf("ParseID round trip = %+v", id)
	}
}

// TestParseTrustDomain_RefusesEmpty matches the re-exported sentinel.
func TestParseTrustDomain_RefusesEmpty(t *testing.T) {
	if _, err := spiffe.ParseTrustDomain(""); !errors.Is(err, spiffe.ErrTrustDomainEmpty) {
		t.Fatalf("ParseTrustDomain(\"\") = %v, want ErrTrustDomainEmpty", err)
	}
}
