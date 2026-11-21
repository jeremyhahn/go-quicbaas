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
	"encoding/base64"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTCGCAService is a wired CA that also serves the go-xkms TCG verbs
// and records the request each one receives.
type recordingTCGCAService struct {
	recordingCAService
	tcgGot interface{}
}

func (c *recordingTCGCAService) IssueEKCertificate(_ context.Context, req *xkmstransport.IssueEKCertificateRequest) (*xkmstransport.IssueEKCertificateResponse, error) {
	c.tcgGot = req
	return &xkmstransport.IssueEKCertificateResponse{SerialNumber: "0a"}, nil
}

func (c *recordingTCGCAService) IssueAKCertificate(_ context.Context, req *xkmstransport.IssueAKCertificateRequest) (*xkmstransport.IssueAKCertificateResponse, error) {
	c.tcgGot = req
	return &xkmstransport.IssueAKCertificateResponse{SerialNumber: "0b"}, nil
}

func (c *recordingTCGCAService) SignTCGCSR(_ context.Context, req *xkmstransport.SignTCGCSRRequest) (*xkmstransport.SignTCGCSRResponse, error) {
	c.tcgGot = req
	return &xkmstransport.SignTCGCSRResponse{IAKCertDER: []byte("IAK")}, nil
}

func (c *recordingTCGCAService) EnrollDevice(_ context.Context, req *xkmstransport.EnrollDeviceRequest) (*xkmstransport.EnrollDeviceResponse, error) {
	c.tcgGot = req
	return &xkmstransport.EnrollDeviceResponse{IAKCertDER: []byte("IAK"), CredentialBlob: []byte("BLOB")}, nil
}

// TestTCGCAHandlersServeTheWiredCA pins #1330's TCGCAService on MCP: each
// xkms.ca.tcg.* tool decodes its base64 input and delegates to the wired CA.
func TestTCGCAHandlersServeTheWiredCA(t *testing.T) {
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	ca := &recordingTCGCAService{}
	registry.SetCAService(ca)
	ctx := context.Background()
	b64 := base64.StdEncoding.EncodeToString

	tests := []struct {
		name    string
		handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
		args    map[string]interface{}
		want    interface{}
	}{
		{"IssueEKCert", registry.handleTCGCAIssueEKCert,
			map[string]interface{}{"common_name": "dev-1", "organization": "o", "ek_public_key": b64([]byte("EK")), "tenant_id": "t1"},
			&xkmstransport.IssueEKCertificateRequest{CommonName: "dev-1", Organization: "o", EKPublicKey: []byte("EK"), TenantID: "t1"}},
		{"IssueAKCert", registry.handleTCGCAIssueAKCert,
			map[string]interface{}{"common_name": "ak-1", "public_key": b64([]byte("AK"))},
			&xkmstransport.IssueAKCertificateRequest{CommonName: "ak-1", PublicKey: []byte("AK")}},
		{"SignCSR", registry.handleTCGCASignCSR,
			map[string]interface{}{"common_name": "dev-1", "tcg_csr": b64([]byte("CSR"))},
			&xkmstransport.SignTCGCSRRequest{CommonName: "dev-1", TCGCSR: []byte("CSR")}},
		{"Enroll", registry.handleTCGCAEnroll,
			map[string]interface{}{"common_name": "dev-1", "packed_csr": b64([]byte("CSR"))},
			&xkmstransport.EnrollDeviceRequest{CommonName: "dev-1", PackedCSR: []byte("CSR")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca.tcgGot = nil
			resp, err := tt.handler(ctx, tt.args)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.want, ca.tcgGot)
		})
	}

	resp, err := registry.handleTCGCAEnroll(ctx, map[string]interface{}{"common_name": "d", "packed_csr": b64([]byte{1})})
	require.NoError(t, err)
	r, ok := resp.(*xkmstransport.EnrollDeviceResponse)
	require.True(t, ok, "Enroll returned %T", resp)
	assert.Equal(t, []byte("IAK"), r.IAKCertDER)
	assert.Equal(t, []byte("BLOB"), r.CredentialBlob)
}

// TestTCGCAHandlersRefuse covers the argument checks and a wired CA without
// the TCG verbs; none of them reaches the CA.
func TestTCGCAHandlersRefuse(t *testing.T) {
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	ca := &recordingTCGCAService{}
	registry.SetCAService(ca)
	ctx := context.Background()

	for _, args := range []map[string]interface{}{
		{},
		{"common_name": "d"},
		{"common_name": "d", "packed_csr": "not base64!"},
	} {
		_, err := registry.handleTCGCAEnroll(ctx, args)
		require.Error(t, err)
	}
	assert.Nil(t, ca.tcgGot)

	plain := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	plain.SetCAService(&recordingCAService{})
	_, err := plain.handleTCGCASignCSR(ctx, map[string]interface{}{"common_name": "d", "tcg_csr": "AQ=="})
	require.Error(t, err)
}
