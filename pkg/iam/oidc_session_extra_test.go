// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

import (
	"testing"
)

// IssueSessionToken fails closed when a DPoP proof is presented but invalid.
func TestIssueSessionToken_InvalidDPoPFailsClosed(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	identity := SessionIdentity{
		Subject:    "user-1",
		TenantID:   "tenant-a",
		AuthMethod: "webauthn",
		// A non-JWT garbage proof must be rejected, not silently ignored.
		DPoPProof:  "not-a-valid-dpop-proof",
		DPoPMethod: "POST",
		DPoPURI:    "https://test.example.com/login",
	}

	if _, _, err := provider.IssueSessionToken(t.Context(), identity); err == nil {
		t.Fatal("IssueSessionToken with an invalid DPoP proof must fail closed")
	}
}

// bindSessionDPoP returns no confirmation when no proof is presented.
func TestBindSessionDPoP_NoProof(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	cnf, err := provider.bindSessionDPoP(SessionIdentity{Subject: "u"})
	if err != nil {
		t.Fatalf("bindSessionDPoP(no proof): %v", err)
	}
	if cnf != nil {
		t.Fatalf("bindSessionDPoP(no proof) = %v, want nil confirmation", cnf)
	}
}
