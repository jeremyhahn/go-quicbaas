//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"testing"
)

// =============================================================================
// Additional barrier / ceremony client nil-request guards.
//
// These cover the nil-request branches of the Shamir, recovery, and ceremony
// claim/enroll client methods that previously lacked direct coverage. The guard
// must reject a nil request before any transport call is attempted.
// =============================================================================

func TestClient_BarrierShamirUnsealShare_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.BarrierShamirUnsealShare(context.Background(), nil)
	verifyNilRequestError(t, err, "BarrierShamirUnsealShare")
}

func TestClient_BarrierShamirUnsealShares_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	err := client.BarrierShamirUnsealShares(context.Background(), nil)
	verifyNilRequestError(t, err, "BarrierShamirUnsealShares")
}

func TestClient_BarrierGenerateRecoveryKeys_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.BarrierGenerateRecoveryKeys(context.Background(), nil)
	verifyNilRequestError(t, err, "BarrierGenerateRecoveryKeys")
}

func TestClient_BarrierRecoverWithKeys_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	err := client.BarrierRecoverWithKeys(context.Background(), nil)
	verifyNilRequestError(t, err, "BarrierRecoverWithKeys")
}

func TestClient_CeremonyClaimCertComplete_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.CeremonyClaimCertComplete(context.Background(), nil)
	verifyNilRequestError(t, err, "CeremonyClaimCertComplete")
}

func TestClient_CeremonyClaimShare_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.CeremonyClaimShare(context.Background(), nil)
	verifyNilRequestError(t, err, "CeremonyClaimShare")
}

func TestClient_CeremonyEnrollExternalOfficer_NilRequest(t *testing.T) {
	client := createTestClient(t)
	defer client.Close()

	_, err := client.CeremonyEnrollExternalOfficer(context.Background(), nil)
	verifyNilRequestError(t, err, "CeremonyEnrollExternalOfficer")
}
