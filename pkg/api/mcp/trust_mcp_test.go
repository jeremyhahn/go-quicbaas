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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

var errTrustMCP = errors.New("trust: mcp boom")

// fakeTrustServiceMCP is a configurable TrustService for the MCP tests.
type fakeTrustServiceMCP struct {
	failNext bool
	lastDER  [][]byte
}

func (f *fakeTrustServiceMCP) CrossSignedRootPool(ctx context.Context, _ *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
	if f.failNext {
		return nil, errTrustMCP
	}
	return &service.CrossSignedRootPoolResponse{
		RootPEMs: [][]byte{[]byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")},
	}, nil
}

func (f *fakeTrustServiceMCP) AutoImportAIA(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
	if f.failNext {
		return nil, errTrustMCP
	}
	f.lastDER = req.ChainDER
	return &service.AutoImportAIAResponse{Imported: len(req.ChainDER)}, nil
}

func (f *fakeTrustServiceMCP) PublishDANE(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
	if f.failNext {
		return nil, errTrustMCP
	}
	f.lastDER = [][]byte{req.CertDER}
	return &service.PublishDANEResponse{Published: 4}, nil
}

func newTrustRegistry(t *testing.T, trust service.TrustService) *ToolRegistry {
	t.Helper()
	log := createTestLogger(t)
	return NewToolRegistry(&mockService{trustSvc: trust}, log)
}

func TestIsTrustTool(t *testing.T) {
	assert.True(t, isTrustTool("quicbaas_trust_cross_signed_root_pool"))
	assert.True(t, isTrustTool("quicbaas_trust_auto_import_aia"))
	assert.True(t, isTrustTool("quicbaas_trust_publish_dane"))
	assert.False(t, isTrustTool("quicbaas_certificate_issue"))
}

func TestTrustToolsRegistered(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{})
	names := map[string]bool{}
	for _, td := range reg.ListTools() {
		names[td.Name] = true
	}
	assert.True(t, names["quicbaas_trust_cross_signed_root_pool"])
	assert.True(t, names["quicbaas_trust_auto_import_aia"])
	assert.True(t, names["quicbaas_trust_publish_dane"])
}

func TestTrustCrossSignedRootPool_MCP(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{})
	result, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_cross_signed_root_pool", map[string]interface{}{})
	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 1, m["count"])
}

func TestTrustAutoImportAIA_MCP(t *testing.T) {
	fake := &fakeTrustServiceMCP{}
	reg := newTrustRegistry(t, fake)

	const chain = "-----BEGIN CERTIFICATE-----\n" +
		"MIIBhTCCASugAwIBAgIBATAKBggqhkjOPQQDAjAA\n" +
		"-----END CERTIFICATE-----\n"

	result, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_auto_import_aia", map[string]interface{}{"chain_pem": chain})
	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 1, m["imported"])
	require.Len(t, fake.lastDER, 1)
}

func TestTrustAutoImportAIA_MCP_MissingChain(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{})
	_, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_auto_import_aia", map[string]interface{}{})
	require.Error(t, err)
}

func TestTrustAutoImportAIA_MCP_InvalidPEM(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{})
	_, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_auto_import_aia", map[string]interface{}{"chain_pem": "not pem"})
	require.Error(t, err)
}

func TestTrustTools_ServiceError_MCP(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{failNext: true})
	_, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_cross_signed_root_pool", map[string]interface{}{})
	require.Error(t, err)
}

func TestTrustTools_NotConfigured_MCP(t *testing.T) {
	reg := newTrustRegistry(t, nil)
	_, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_cross_signed_root_pool", map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trust service not configured")
}

func TestTrustPublishDANE_MCP(t *testing.T) {
	fake := &fakeTrustServiceMCP{}
	reg := newTrustRegistry(t, fake)

	der := []byte{0x30, 0x82, 0x01, 0x02}
	result, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_publish_dane", map[string]interface{}{
			"name":     "kms.example.com",
			"port":     443,
			"cert_der": base64.StdEncoding.EncodeToString(der),
		})
	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 4, m["published"])
	require.Len(t, fake.lastDER, 1)
	assert.Equal(t, der, fake.lastDER[0])
}

func TestTrustPublishDANE_MCP_MissingArgs(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{})

	tests := []map[string]interface{}{
		{"port": 443, "cert_der": "MIIB"},
		{"name": "h", "cert_der": "MIIB"},
		{"name": "h", "port": 443},
		{"name": "h", "port": 443, "cert_der": "!!!not-base64!!!"},
	}
	for _, args := range tests {
		_, err := reg.ExecuteTool(context.Background(), "quicbaas_trust_publish_dane", args)
		require.Error(t, err)
	}
}

func TestTrustPublishDANE_MCP_ServiceError(t *testing.T) {
	reg := newTrustRegistry(t, &fakeTrustServiceMCP{failNext: true})
	_, err := reg.ExecuteTool(context.Background(),
		"quicbaas_trust_publish_dane", map[string]interface{}{
			"name": "h", "port": 443, "cert_der": "MIIB",
		})
	require.Error(t, err)
}
