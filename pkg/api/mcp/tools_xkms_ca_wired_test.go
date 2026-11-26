//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingCAService is a go-xkms CAService that records the request each
// verb received and answers with a fixed response.
type recordingCAService struct {
	got interface{}
}

func (f *recordingCAService) GetCABundle(_ context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	f.got = req
	return &xkmstransport.GetCABundleResponse{}, nil
}

func (f *recordingCAService) GetCACertificate(_ context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	f.got = req
	return &xkmstransport.GetCACertificateResponse{}, nil
}

func (f *recordingCAService) SignCSR(_ context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	f.got = req
	return &xkmstransport.SignCSRResponse{}, nil
}

func (f *recordingCAService) IssueCertificate(_ context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	f.got = req
	return &xkmstransport.IssueCertificateResponse{}, nil
}

func (f *recordingCAService) RevokeCertificate(_ context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	f.got = req
	return &xkmstransport.RevokeCertificateResponse{}, nil
}

func (f *recordingCAService) GenerateCRL(_ context.Context, req *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	f.got = req
	return &xkmstransport.GenerateCRLResponse{}, nil
}

func (f *recordingCAService) IsRevoked(_ context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	f.got = req
	return &xkmstransport.IsRevokedResponse{Revoked: true}, nil
}

// TestTrustedCAHandlersServeTheWiredCA pins #1330 on the server's MCP
// transport: once the server wires the go-xkms CA (SetCAService), each
// xkms.ca.* tool hands go-xkms's own request, built from the tool arguments,
// to that CA and returns its response, instead of "operation not supported".
func TestTrustedCAHandlersServeTheWiredCA(t *testing.T) {
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	ca := &recordingCAService{}
	registry.SetCAService(ca)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
		args    map[string]interface{}
		want    interface{}
	}{
		{"GetBundle", registry.handleTrustedCAGetBundle,
			map[string]interface{}{"store_type": "tpm2", "algorithm": "ECDSA", "tenant_id": "t1"},
			&xkmstransport.GetCABundleRequest{StoreType: "tpm2", Algorithm: "ECDSA", TenantID: "t1"}},
		{"GetCert", registry.handleTrustedCAGetCert,
			map[string]interface{}{"identity": "root"},
			&xkmstransport.GetCACertificateRequest{Identity: "root"}},
		{"SignCSR", registry.handleTrustedCASignCSR,
			map[string]interface{}{"csr_pem": "PEM", "profile": "server", "validity_days": float64(30)},
			&xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM"), Profile: "server", ValidityDays: 30}},
		// The attestation proof reaches the CA (which verifies or refuses
		// it), in go-xkms's JSON encoding: bytes as base64.
		{"SignCSRAttested", registry.handleTrustedCASignCSR,
			map[string]interface{}{
				"csr_pem": "PEM", "tenant_id": "t1", "attestation_format": "tpm2",
				"attestation_chain": []interface{}{"AQI="}, "attestation_data": "AwQ=",
				"attestation_sig": "BQY=", "attestation_nonce": "Bwg=",
			},
			&xkmstransport.SignCSRRequest{
				CSRPEM: []byte("PEM"), TenantID: "t1", AttestationFormat: "tpm2",
				AttestationChain: [][]byte{{1, 2}}, AttestationData: []byte{3, 4},
				AttestationSig: []byte{5, 6}, AttestationNonce: []byte{7, 8},
			}},
		{"IssueCert", registry.handleTrustedCAIssueCert,
			map[string]interface{}{"profile": "server", "common_name": "node-1", "organization": "o", "sans": []interface{}{"node-1"}, "algorithm": "ed25519"},
			&xkmstransport.IssueCertificateRequest{CommonName: "node-1", Profile: "server", Organization: "o", SANs: []string{"node-1"}, Algorithm: "ed25519"}},
		{"RevokeCert", registry.handleTrustedCARevokeCert,
			map[string]interface{}{"serial_number": "0a", "reason": float64(1)},
			&xkmstransport.RevokeCertificateRequest{SerialNumber: "0a", Reason: 1}},
		{"GenerateCRL", registry.handleTrustedCAGenerateCRL,
			map[string]interface{}{"tenant_id": "t1"},
			&xkmstransport.GenerateCRLRequest{TenantID: "t1"}},
		{"IsRevoked", registry.handleTrustedCAIsRevoked,
			map[string]interface{}{"serial_number": "0a"},
			&xkmstransport.IsRevokedRequest{SerialNumber: "0a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca.got = nil
			resp, err := tt.handler(ctx, tt.args)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.want, ca.got)
		})
	}

	resp, err := registry.handleTrustedCAIsRevoked(ctx, map[string]interface{}{"serial_number": "0a"})
	require.NoError(t, err)
	r, ok := resp.(*xkmstransport.IsRevokedResponse)
	require.True(t, ok, "IsRevoked returned %T", resp)
	assert.True(t, r.Revoked)

	// The required arguments are still checked before the CA is reached.
	ca.got = nil
	_, err = registry.handleTrustedCAIsRevoked(ctx, map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, ca.got)

	// A malformed attestation proof is refused, never signed without it.
	_, err = registry.handleTrustedCASignCSR(ctx, map[string]interface{}{"csr_pem": "PEM", "attestation_data": "!!"})
	require.Error(t, err)
	assert.Nil(t, ca.got)
}
