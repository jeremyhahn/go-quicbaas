//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Ceremony handler request-validation branches.
//
// These handlers validate required fields (and, for external-officer enrolment,
// the cert/pubkey mutual exclusion) before delegating to the ceremony facade. A
// valid ceremony facade is wired so failures are attributable to validation.
// =============================================================================

func TestCeremonyValidation_RequiredFields(t *testing.T) {
	handler := newCeremonyHandler(t, &mockCeremonyServicer{})
	ctx := context.Background()

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"claim-cert-begin/no-username", SubOpCeremonyClaimCertBegin, encMust(t, &ceremonyClaimCertBeginWireRequest{})},
		{"begin-claim-share/no-username", SubOpCeremonyBeginClaimShare, encMust(t, &ceremonyBeginClaimShareWireRequest{})},
		{"complete-claim-share/no-username", SubOpCeremonyCompleteClaimShare, encMust(t, &ceremonyCompleteClaimShareWireRequest{NonceHex: "ab", Signature: "c2ln"})},
		{"complete-claim-share/no-nonce", SubOpCeremonyCompleteClaimShare, encMust(t, &ceremonyCompleteClaimShareWireRequest{Username: "u", Signature: "c2ln"})},
		{"complete-claim-share/no-signature", SubOpCeremonyCompleteClaimShare, encMust(t, &ceremonyCompleteClaimShareWireRequest{Username: "u", NonceHex: "ab"})},
		{"complete-claim-share/bad-signature-encoding", SubOpCeremonyCompleteClaimShare, encMust(t, &ceremonyCompleteClaimShareWireRequest{Username: "u", NonceHex: "ab", Signature: "%%%"})},
		{"claim-cert-complete/no-username", SubOpCeremonyClaimCertComplete, encMust(t, &ceremonyClaimCertCompleteWireRequest{NonceHex: "ab", Signature: "s"})},
		{"claim-cert-complete/no-nonce", SubOpCeremonyClaimCertComplete, encMust(t, &ceremonyClaimCertCompleteWireRequest{Username: "u", Signature: "s"})},
		{"claim-cert-complete/no-signature", SubOpCeremonyClaimCertComplete, encMust(t, &ceremonyClaimCertCompleteWireRequest{Username: "u", NonceHex: "ab"})},
		{"sign-csr/no-username", SubOpCeremonySignCSR, encMust(t, &ceremonySignCSRWireRequest{CSRPEM: "csr"})},
		{"sign-csr/no-csr", SubOpCeremonySignCSR, encMust(t, &ceremonySignCSRWireRequest{Username: "u"})},
		{"enroll/no-username", SubOpCeremonyEnrollExternalOfficer, encMust(t, &ceremonyEnrollExternalOfficerWireRequest{CertPEM: "c", AuthFactor: "piv"})},
		{"enroll/no-cert-or-pubkey", SubOpCeremonyEnrollExternalOfficer, encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "u", AuthFactor: "piv"})},
		{"enroll/both-cert-and-pubkey", SubOpCeremonyEnrollExternalOfficer, encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "u", CertPEM: "c", PubKeyPEM: "p", AuthFactor: "piv"})},
		{"enroll/no-auth-factor", SubOpCeremonyEnrollExternalOfficer, encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "u", CertPEM: "c"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, ceremonyRequest(tc.subOp, tc.body))
			assert.NotEqual(t, StatusOK, resp.Status, "%s must be rejected by validation", tc.name)
		})
	}
}
