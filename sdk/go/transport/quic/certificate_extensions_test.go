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

package quic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// =============================================================================
// Protocol allocation
// =============================================================================

// TestCertExtSubOpValues pins the wire values. pkg/api/quic/protocol.go
// declares the same constants; a silent divergence would route a cross-sign to
// whatever the server has at that byte, so the values are asserted literally
// rather than by symbol.
func TestCertExtSubOpValues(t *testing.T) {
	assert.Equal(t, byte(0x60), SubOpCertExtCrossSign)
	assert.Equal(t, byte(0x61), SubOpCertExtChain)
	assert.Equal(t, byte(0x62), SubOpCertExtIssueDeviceIdentity)
}

// TestCertExtSubOpRangeIsDisjointFromSiblings proves the certificate extension
// range does not overlap the other vocabularies that share OpOrchestrator.
func TestCertExtSubOpRangeIsDisjointFromSiblings(t *testing.T) {
	occupied := map[byte]string{}
	for method, subOp := range accountMethodToSubOp {
		occupied[subOp] = "account/" + method
	}
	for method, subOp := range projectMethodToSubOp {
		occupied[subOp] = "project/" + method
	}

	for method, subOp := range certExtMethodToSubOp {
		if owner, clash := occupied[subOp]; clash {
			t.Fatalf("certificate extension %s (%#x) collides with %s", method, subOp, owner)
		}
	}
}

// TestCertExtMethodToSubOp covers the lookup for both wired and unwired
// methods.
func TestCertExtMethodToSubOp(t *testing.T) {
	cases := map[string]byte{
		"CrossSign":           SubOpCertExtCrossSign,
		"Chain":               SubOpCertExtChain,
		"IssueDeviceIdentity": SubOpCertExtIssueDeviceIdentity,
	}
	for method, want := range cases {
		got, ok := CertExtMethodToSubOp(method)
		require.True(t, ok, "method %s must map to a sub-op", method)
		assert.Equal(t, want, got)
	}

	// The six base certificate verbs keep their own top-level opcodes and must
	// NOT resolve here, or the transport would prepend a stray byte.
	for _, method := range []string{
		"IssueCertificate", "SignCSR", "RevokeCertificate",
		"VerifyCertificate", "GetCACertificate", "GetTLSConfig",
	} {
		_, ok := CertExtMethodToSubOp(method)
		assert.False(t, ok, "base verb %s must not carry a certificate extension sub-op", method)
	}
}

// TestCertExtMethodsRouteToOrchestratorOpcode pins the dispatch byte both
// spellings of the service name resolve to.
func TestCertExtMethodsRouteToOrchestratorOpcode(t *testing.T) {
	for _, method := range []string{"CrossSign", "Chain", "IssueDeviceIdentity"} {
		opcode, ok := MethodToOpcode("quicbaas.Certificate", method)
		require.True(t, ok, "quicbaas.Certificate/%s must resolve to an opcode", method)
		assert.Equal(t, OpOrchestrator, opcode)

		opcode, ok = MethodToOpcode("Certificate", method)
		require.True(t, ok, "Certificate/%s must resolve to an opcode", method)
		assert.Equal(t, OpOrchestrator, opcode)
	}
}

// =============================================================================
// Response decoding
// =============================================================================

func TestDecodeCertificateResponse_Extensions(t *testing.T) {
	t.Run("CrossSign", func(t *testing.T) {
		body, err := json.Marshal(map[string]interface{}{"cross_signed_pem": []byte("cross-signed")})
		require.NoError(t, err)

		resp, err := decodeCertificateResponse("CrossSign", body)
		require.NoError(t, err)

		typed, ok := resp.(*certificate.CrossSignResponse)
		require.True(t, ok, "got %T", resp)
		assert.Equal(t, []byte("cross-signed"), typed.CrossSignedPEM)
	})

	t.Run("Chain", func(t *testing.T) {
		body, err := json.Marshal(map[string]interface{}{
			"chain_pem": [][]byte{[]byte("issuer"), []byte("root")},
		})
		require.NoError(t, err)

		resp, err := decodeCertificateResponse("Chain", body)
		require.NoError(t, err)

		typed, ok := resp.(*certificate.ChainResponse)
		require.True(t, ok, "got %T", resp)
		require.Len(t, typed.ChainPEM, 2)
		assert.Equal(t, []byte("issuer"), typed.ChainPEM[0])
	})

	t.Run("IssueDeviceIdentity", func(t *testing.T) {
		body, err := json.Marshal(map[string]interface{}{
			"certificate_pem": []byte("device-cert"),
			"chain_pem":       []byte("chain"),
			"serial_number":   "abc",
		})
		require.NoError(t, err)

		resp, err := decodeCertificateResponse("IssueDeviceIdentity", body)
		require.NoError(t, err)

		typed, ok := resp.(*certificate.DeviceIdentityResponse)
		require.True(t, ok, "got %T", resp)
		assert.Equal(t, []byte("device-cert"), typed.CertificatePEM)
		assert.Equal(t, "abc", typed.SerialNumber)
	})
}

func TestDecodeCertificateResponse_ExtensionsMalformed(t *testing.T) {
	for _, method := range []string{"CrossSign", "Chain", "IssueDeviceIdentity"} {
		t.Run(method, func(t *testing.T) {
			_, err := decodeCertificateResponse(method, []byte("{"))
			assert.Error(t, err)
		})
	}
}
