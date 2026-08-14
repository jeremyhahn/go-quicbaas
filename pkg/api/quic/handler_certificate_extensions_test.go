//go:build quic

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
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// certExtRequest builds an OpOrchestrator request carrying a certificate
// extension sub-op followed by the JSON body, matching the wire format the SDK
// QUIC transport produces.
func certExtRequest(t *testing.T, subOp byte, body interface{}) *Request {
	t.Helper()

	payload, err := EncodePayload(body)
	require.NoError(t, err)

	withSubOp := make([]byte, 1+len(payload))
	withSubOp[0] = subOp
	copy(withSubOp[1:], payload)

	return &Request{Opcode: OpOrchestrator, Payload: withSubOp}
}

// =============================================================================
// Sub-op range
// =============================================================================

// TestCertExtSubOpWireValues pins the wire values literally. The SDK declares
// the same constants in sdk/go/transport/quic/protocol.go and asserts the same
// literals in its own TestCertExtSubOpValues; pinning both sides means a
// one-sided edit fails a test instead of silently misrouting a cross-sign to
// whichever verb the peer has at that byte.
func TestCertExtSubOpWireValues(t *testing.T) {
	assert.Equal(t, byte(0x60), SubOpCertExtCrossSign)
	assert.Equal(t, byte(0x61), SubOpCertExtChain)
	assert.Equal(t, byte(0x62), SubOpCertExtIssueDeviceIdentity)
}

// TestCertExtSubOpRangeIsDisjoint proves the certificate extension range does
// not overlap the account, project or SSH ranges that share the
// OpOrchestrator dispatch byte. An overlap would silently misroute requests.
func TestCertExtSubOpRangeIsDisjoint(t *testing.T) {
	for _, subOp := range []byte{
		SubOpCertExtCrossSign, SubOpCertExtChain, SubOpCertExtIssueDeviceIdentity,
	} {
		assert.True(t, isCertExtSubOp(subOp))
		assert.False(t, isAccountSubOp(subOp), "sub-op %#x collides with the account range", subOp)
		assert.False(t, isProjectSubOp(subOp), "sub-op %#x collides with the project range", subOp)
		assert.False(t, isSSHSubOp(subOp), "sub-op %#x collides with the SSH range", subOp)
	}

	// Bytes just outside the range must not be claimed.
	assert.False(t, isCertExtSubOp(SubOpCertExtCrossSign-1))
	assert.False(t, isCertExtSubOp(SubOpCertExtIssueDeviceIdentity+1))
}

// =============================================================================
// CrossSign
// =============================================================================

func TestHandleCertCrossSign_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		CrossSignFunc: func(_ context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
			assert.Equal(t, []byte("foreign-root"), req.ForeignRootPEM)
			return &service.CrossSignResponse{CrossSignedPEM: []byte("cross-signed")}, nil
		},
	}
	handler := createCertificateTestHandler(certSvc)

	resp := handler.HandleRequest(context.Background(), certExtRequest(t, SubOpCertExtCrossSign,
		&service.CrossSignRequest{ForeignRootPEM: []byte("foreign-root")}))

	require.Equal(t, StatusOK, resp.Status)
	var decoded service.CrossSignResponse
	require.NoError(t, json.Unmarshal(resp.Payload, &decoded))
	assert.Equal(t, []byte("cross-signed"), decoded.CrossSignedPEM)
}

func TestHandleCertCrossSign_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)
	resp := handler.HandleRequest(context.Background(), certExtRequest(t, SubOpCertExtCrossSign,
		&service.CrossSignRequest{ForeignRootPEM: []byte("r")}))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestHandleCertCrossSign_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		CrossSignFunc: func(context.Context, *service.CrossSignRequest) (*service.CrossSignResponse, error) {
			return nil, service.ErrCertExtNotSupported
		},
	}
	handler := createCertificateTestHandler(certSvc)
	resp := handler.HandleRequest(context.Background(), certExtRequest(t, SubOpCertExtCrossSign,
		&service.CrossSignRequest{ForeignRootPEM: []byte("r")}))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// Chain
// =============================================================================

func TestHandleCertChain_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		ChainFunc: func(_ context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
			assert.Equal(t, []byte("leaf"), req.CertificatePEM)
			return &service.CertificateChainResponse{
				ChainPEM: [][]byte{[]byte("issuer"), []byte("root")},
			}, nil
		},
	}
	handler := createCertificateTestHandler(certSvc)

	resp := handler.HandleRequest(context.Background(), certExtRequest(t, SubOpCertExtChain,
		&service.CertificateChainRequest{CertificatePEM: []byte("leaf")}))

	require.Equal(t, StatusOK, resp.Status)
	var decoded service.CertificateChainResponse
	require.NoError(t, json.Unmarshal(resp.Payload, &decoded))
	require.Len(t, decoded.ChainPEM, 2)
	assert.Equal(t, []byte("issuer"), decoded.ChainPEM[0])
}

func TestHandleCertChain_DecodeError(t *testing.T) {
	handler := createCertificateTestHandler(&mockCertificateService{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpOrchestrator,
		Payload: append([]byte{SubOpCertExtChain}, []byte("not json")...),
	})
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func TestHandleCertIssueDeviceIdentity_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		IssueDeviceIdentityFunc: func(_ context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
			assert.Equal(t, "ak", req.Type)
			assert.Equal(t, "device-quic", req.DeviceID)
			assert.Equal(t, []byte("ak-pub"), req.AKPub)
			return &service.IssueDeviceIdentityResponse{
				CertificatePEM: []byte("ak-cert"),
				SerialNumber:   "77",
			}, nil
		},
	}
	handler := createCertificateTestHandler(certSvc)

	resp := handler.HandleRequest(context.Background(), certExtRequest(t, SubOpCertExtIssueDeviceIdentity,
		&service.IssueDeviceIdentityRequest{Type: "ak", DeviceID: "device-quic", AKPub: []byte("ak-pub")}))

	require.Equal(t, StatusOK, resp.Status)
	var decoded service.IssueDeviceIdentityResponse
	require.NoError(t, json.Unmarshal(resp.Payload, &decoded))
	assert.Equal(t, []byte("ak-cert"), decoded.CertificatePEM)
	assert.Equal(t, "77", decoded.SerialNumber)
}

func TestHandleCertExtDispatch_UnknownSubOp(t *testing.T) {
	handler := createCertificateTestHandler(&mockCertificateService{})

	// 0x63 sits one past the certificate extension range, so
	// handleOrchestratorDispatch must NOT route it here; asserting on the
	// dispatcher directly proves the unknown-sub-op guard also fails closed.
	resp := handler.handleCertExtDispatch(context.Background(), 0x63, &Request{})
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// Authorization mapping
// =============================================================================

// TestCertExtQRDBPermissions pins the permission each sub-op resolves to and
// proves the default-deny behaviour for an unmapped sub-op.
func TestCertExtQRDBPermissions(t *testing.T) {
	cases := []struct {
		subOp    byte
		resource string
		action   string
	}{
		{SubOpCertExtCrossSign, "ca", "create"},
		{SubOpCertExtChain, "certs", "generate"},
		{SubOpCertExtIssueDeviceIdentity, "certs", "generate"},
	}

	for _, tc := range cases {
		resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{tc.subOp})
		require.True(t, ok, "sub-op %#x must be mapped or it is default-denied", tc.subOp)
		assert.Equal(t, tc.resource, resource)
		assert.Equal(t, tc.action, action)
	}
}

// TestCertExtQRDBPermissions_NeverFallsBackToOrchestratorStatus proves the
// certificate extension sub-ops are intercepted before the surface-level
// OpOrchestrator mapping. Falling through would authorize cross-signing with
// orchestrator:status — a materially weaker permission than ca:create, which
// is exactly the class of bug the account and project sub-op tables exist to
// prevent.
func TestCertExtQRDBPermissions_NeverFallsBackToOrchestratorStatus(t *testing.T) {
	surfaceResource, surfaceAction, ok := getQRDBPermission(OpOrchestrator, []byte{0x01})
	require.True(t, ok)
	require.Equal(t, "orchestrator", surfaceResource)

	for _, subOp := range []byte{
		SubOpCertExtCrossSign, SubOpCertExtChain, SubOpCertExtIssueDeviceIdentity,
	} {
		resource, action, found := getQRDBPermission(OpOrchestrator, []byte{subOp})
		require.True(t, found)
		assert.NotEqual(t, surfaceResource+":"+surfaceAction, resource+":"+action,
			"sub-op %#x must not inherit the surface-level orchestrator permission", subOp)
	}
}

// TestCertExtQRDBPermissions_UnmappedSubOpInRangeIsDenied covers the
// fail-closed branch: a byte that isCertExtSubOp accepts but the table does
// not map must deny rather than resolve to anything.
func TestCertExtQRDBPermissions_UnmappedSubOpInRangeIsDenied(t *testing.T) {
	saved := certExtSubOpPermissions[SubOpCertExtChain]
	delete(certExtSubOpPermissions, SubOpCertExtChain)
	defer func() { certExtSubOpPermissions[SubOpCertExtChain] = saved }()

	_, _, ok := getQRDBPermission(OpOrchestrator, []byte{SubOpCertExtChain})
	assert.False(t, ok, "an unmapped certificate extension sub-op must default-deny")
}

// TestCrossSignQUICRequiresCAAuthority mirrors the REST and gRPC guards: the
// leaf-issuance verb must not reach cross-signing.
func TestCrossSignQUICRequiresCAAuthority(t *testing.T) {
	resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{SubOpCertExtCrossSign})
	require.True(t, ok)
	assert.Equal(t, "ca", resource)
	assert.Equal(t, "create", action)

	leafResource, leafAction, ok := getQRDBPermission(OpCertIssue, nil)
	require.True(t, ok)
	assert.NotEqual(t, leafResource+":"+leafAction, resource+":"+action)
}
