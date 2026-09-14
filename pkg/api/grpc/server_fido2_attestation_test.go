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
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestServer_FIDO2FinishRegistration_AttestationRefusalIsPermissionDenied is
// the surface nobody had to teach.
//
// gRPC registration reaches the same attestation gate as the REST surfaces, and
// its translator was never given a case for the gate's refusal types. It is
// correct anyway, because the refusal carries the repo's canonical
// classification and toProtoError routes by that — which is the whole argument
// for classifying at the error rather than at each transport. This test is what
// keeps that claim honest.
func TestServer_FIDO2FinishRegistration_AttestationRefusalIsPermissionDenied(t *testing.T) {
	// The refusals as the production gate really produces them, not as they
	// are believed to look.
	gate := iam.NewWebAuthnAttestationGate(iam.WebAuthnAssuranceHardware, nil, nil)
	aaguid := []byte{0xcb, 0x69, 0x48, 0x1e, 0x8f, 0xf7, 0x40, 0x39,
		0x93, 0xec, 0x0a, 0x27, 0x29, 0xa1, 0x54, 0xa8}

	for _, tc := range []struct {
		name            string
		attestationType string
	}{
		{"software authenticator", "basic_surrogate"},
		{"unverified attestation chain", "basic_full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, refusal := gate.Evaluate(context.Background(), "tenant-a", "alice@example.com",
				tc.attestationType, aaguid)
			require.Error(t, refusal, "the gate admitted %q where hardware was required", tc.attestationType)

			srv, mockSvc := createTestServerWithMockService(t)
			mockSvc.FIDO2Svc = &stubFIDO2Service{err: refusal}

			_, err := srv.FinishRegistration(context.Background(), &pb.FIDO2FinishRegistrationRequest{
				TenantId:        "tenant-a",
				SessionId:       "sess-1",
				AttestationJson: "{}",
			})
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err),
				"a refused authenticator was reported as a server fault: %v", err)
		})
	}
}

// TestServer_FIDO2FinishRegistration_RealFailureStaysInternal is the control.
// An unclassified failure is still an outage, and must keep saying so.
func TestServer_FIDO2FinishRegistration_RealFailureStaysInternal(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.FIDO2Svc = &stubFIDO2Service{err: stderrors.New("credential store unavailable")}

	_, err := srv.FinishRegistration(context.Background(), &pb.FIDO2FinishRegistrationRequest{
		TenantId:        "tenant-a",
		SessionId:       "sess-1",
		AttestationJson: "{}",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err),
		"a real ceremony failure stopped reading as a server fault")
}
