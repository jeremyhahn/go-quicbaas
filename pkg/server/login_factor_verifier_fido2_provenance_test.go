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

package server

import (
	"errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// TestLoginFactorVerifiers_FIDO2IsTheRelyingPartyVerifier is the bridge
// between the invariant proved in pkg/iam and the verifier the LIVE login
// ceremony actually calls.
//
// pkg/iam/webauthn_login_key_provenance_test.go proves that the xkms relying
// party takes the verification key from the enrolled credential and ignores
// the request's "cose_public_key". That proof is only worth anything if the
// verifier the node registers for fido2 is the one that reaches it — which is
// exactly what go-quicbaas#484 could not tell from the outside, since a
// verifier that DID trust client key material (go-xkms pkg/factor/fido2, the
// custodian-plugin verifier the issue mistakenly traced) implements the same
// interface and would slot in unnoticed.
//
// qrdb keys its verifier registry by FactorType (sdk/go transport/embedded
// factor.go WithFactorVerifier), so exactly one verifier can serve fido2, and
// this test names which one.
func TestLoginFactorVerifiers_FIDO2IsTheRelyingPartyVerifier(t *testing.T) {
	s := &Server{}
	s.buildLoginFactorVerifiers()

	var fido2Verifiers int
	for _, verifier := range s.loginFactorVerifiers {
		if verifier.Type() != qrdbtransport.FactorTypeFIDO2 {
			continue
		}
		fido2Verifiers++
		if _, ok := verifier.(*tenantWebAuthnVerifier); !ok {
			t.Fatalf("the fido2 login verifier is %T; it must be *tenantWebAuthnVerifier, "+
				"which delegates to the xkms relying party and therefore verifies with "+
				"the key recorded at enrollment", verifier)
		}
	}
	if fido2Verifiers != 1 {
		t.Fatalf("registered %d fido2 login verifiers, want exactly 1", fido2Verifiers)
	}
}

// TestTenantWebAuthnVerifier_TenantlessPathRefuses asserts the fallback entry
// point cannot verify at all.
//
// A relying party is scoped to a tenant's RP id and origins. Verifying without
// one would mean verifying against an undefined relying party, so the
// tenant-less method refuses rather than guessing — this is the second half of
// "the key comes from the enrollment": the right enrollment, in the right
// tenant.
func TestTenantWebAuthnVerifier_TenantlessPathRefuses(t *testing.T) {
	verifier := newTenantWebAuthnVerifier()

	err := verifier.Verify(t.Context(), nil, qrdbtransport.VerifyChallenge{
		Type: qrdbtransport.FactorTypeFIDO2,
		MethodMetadata: map[string]string{
			"session_id":      "rp-1",
			"cose_public_key": "any-client-supplied-key",
		},
	})
	if err == nil {
		t.Fatal("the tenant-less verify path must refuse, never verify")
	}
	var refused *NonTenantWebAuthnVerifyError
	if !errors.As(err, &refused) {
		t.Fatalf("error = %T (%v), want *NonTenantWebAuthnVerifyError", err, err)
	}
}
