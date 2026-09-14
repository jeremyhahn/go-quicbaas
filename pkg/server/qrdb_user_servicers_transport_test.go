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
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// How the refusal is CLASSIFIED, not just that it happens.
//
// Refusing caller-supplied attestation metadata is only half a contract. The
// other half is what the caller is told: the raw auth-factor surfaces classify
// a servicer error by matching qrdb's factor sentinels, and anything
// unrecognised falls through to a server-fault code. A correct refusal
// delivered as INTERNAL tells the caller the server broke — it reads as an
// outage, invites a retry that can never succeed, and hides the one fact they
// need, which is that their input was rejected and which field caused it.
//
// These assertions are deliberately NOT behind the `mcp` build tag. The
// sentinel they pin is what qrdb's gRPC mapper keys on, and gRPC has nothing
// to do with MCP — gating them would mean most CI passes never execute the
// contract that matters most. The MCP end-to-end half, which needs
// mcpapi.ToolRegistry and therefore the tag, lives in
// qrdb_user_servicers_mcp_test.go.
// =============================================================================

// TestAuthFactorProxy_ReservedMetadataIsClassifiedAsInvalidInput pins the
// property every transport mapper keys on.
//
// qrdb's gRPC service maps this surface with mapUserAuthFactorGRPCError
// (go-qrdb@v0.10.0-alpha pkg/api/grpc/user_auth_factor_server.go): it returns
// codes.InvalidArgument for errors.Is(err, auth.ErrInvalidFactor) and
// codes.Internal for everything else. qrdbsdk.ErrInvalidFactor is that same
// sentinel, re-exported through the SDK, so this asserts the classification the
// wire will apply without reaching behind the SDK boundary to build a server.
func TestAuthFactorProxy_ReservedMetadataIsClassifiedAsInvalidInput(t *testing.T) {
	for _, field := range []string{iam.AuthFactorMetadataAttestLevel, iam.AuthFactorMetadataAttestAAGUID} {
		t.Run(field, func(t *testing.T) {
			proxy := newRecordingProxy(&recordingAuthFactorServicer{})
			err := proxy.Add(context.Background(), 1, &qrdbsdk.AuthFactor{
				Type:     qrdbsdk.AuthFactorFIDO2,
				Metadata: map[string]string{field: "hardware"},
			})
			if err == nil {
				t.Fatal("reserved attestation metadata was accepted")
			}
			if !errors.Is(err, qrdbsdk.ErrInvalidFactor) {
				t.Errorf("the refusal is not classified as an invalid factor, so the "+
					"transport reports it as a server fault instead of invalid input: %v", err)
			}
			// The specific reason must survive the classification: a caller has
			// to learn WHICH field they may not set.
			var metadataErr *iam.AuthFactorAttestationMetadataError
			if !errors.As(err, &metadataErr) {
				t.Fatalf("want *iam.AuthFactorAttestationMetadataError, got %T", err)
			}
			if metadataErr.Field != field {
				t.Errorf("Field = %q, want %q", metadataErr.Field, field)
			}
		})
	}
}

// TestAuthFactorProxy_IsNotSubstitutableForTheCeremonyWriteThrough is the
// reachability guard behind the invariant pkg/iam pins behaviourally.
//
// The WebAuthn ceremony persists what it PROVED; this proxy stamps
// attest_level=none because the raw surfaces it fronts run no ceremony. They
// are opposite answers to the same field, and today they cannot be confused:
// the ceremony write-through consumes qrdbtransport.UserAuthFactorClient
// (webauthn_xkms_factory.go hands it QRDB.UserAuthFactors()), while this proxy
// implements qrdbsdk.UserAuthFactorServicer.
//
// The separation is currently an accident of two interfaces rather than
// anything that would object if it changed. The day this proxy also satisfies
// the ceremony's client interface, someone can wire it in — and a verified
// hardware key would be persisted as unattested, silently, with the enrollment
// still succeeding. That is a review finding waiting to become true, so it
// fails here instead.
func TestAuthFactorProxy_IsNotSubstitutableForTheCeremonyWriteThrough(t *testing.T) {
	var proxy any = newRecordingProxy(&recordingAuthFactorServicer{})

	if _, ok := proxy.(qrdbtransport.UserAuthFactorClient); ok {
		t.Fatal("the unattested-stamping proxy now satisfies the ceremony write-through's " +
			"factor-client interface. Nothing wires it there yet, but it is now substitutable, " +
			"and a verified WebAuthn enrollment routed through it would be stamped " +
			"attest_level=none while appearing to succeed. Keep the two surfaces distinct, " +
			"or teach the proxy to preserve a proof the ceremony established.")
	}
}
