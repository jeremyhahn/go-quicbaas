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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// newCertExtRegistry builds a ToolRegistry backed by the supplied certificate
// service.
func newCertExtRegistry(t *testing.T, certSvc service.CertificateService) *ToolRegistry {
	t.Helper()
	return NewToolRegistry(newTestQuicBaaSWithCertificateService(certSvc), createTestLogger(t))
}

// =============================================================================
// Registration
// =============================================================================

func TestCertificateExtensionToolsAreRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range getAllCertificateTools() {
		names[tool.Name] = true
	}

	for _, want := range []string{
		"quicbaas_certificate_cross_sign",
		"quicbaas_certificate_chain",
		"quicbaas_certificate_issue_device_identity",
	} {
		assert.True(t, names[want], "tool %s must be registered", want)
		assert.True(t, isCertificateTool(want), "isCertificateTool must accept %s", want)
		assert.True(t, isCertificateExtensionTool(want))
	}

	assert.False(t, isCertificateExtensionTool("quicbaas_certificate_issue"))
	assert.False(t, isCertificateExtensionTool(""))
}

// TestCertificateExtensionToolsHaveAuthzMapping guards the default-deny gate:
// an unmapped tool is denied AND filtered out of tools/list, so it ships dead.
func TestCertificateExtensionToolsHaveAuthzMapping(t *testing.T) {
	cases := map[string]struct{ resource, action string }{
		"quicbaas_certificate_cross_sign":            {"ca", "create"},
		"quicbaas_certificate_chain":                 {"certs", "enroll"},
		"quicbaas_certificate_issue_device_identity": {"certs", "issue"},
	}

	for tool, want := range cases {
		t.Run(tool, func(t *testing.T) {
			resource, action, ok := getToolQRDBPermission(tool)
			require.True(t, ok, "tool %s must be mapped or it is invisible to every MCP client", tool)
			assert.Equal(t, want.resource, resource)
			assert.Equal(t, want.action, action)
		})
	}
}

// TestCrossSignToolRequiresCAAuthority mirrors the REST / gRPC / QUIC guards.
func TestCrossSignToolRequiresCAAuthority(t *testing.T) {
	resource, action, ok := getToolQRDBPermission("quicbaas_certificate_cross_sign")
	require.True(t, ok)
	assert.Equal(t, "ca", resource)
	assert.Equal(t, "create", action)
}

// =============================================================================
// CrossSign
// =============================================================================

func TestExecuteCertificateCrossSignTool(t *testing.T) {
	ctx := context.Background()

	t.Run("cross-signs successfully", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{
			CrossSignFunc: func(_ context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
				assert.Equal(t, []byte("foreign-root"), req.ForeignRootPEM)
				return &service.CrossSignResponse{CrossSignedPEM: []byte("cross-signed")}, nil
			},
		})

		result, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_cross_sign",
			map[string]interface{}{
				"foreign_root_pem": base64.StdEncoding.EncodeToString([]byte("foreign-root")),
			})
		require.NoError(t, err)

		m, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "cross-signed", m["cross_signed_pem"])
	})

	t.Run("rejects a missing foreign root", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_cross_sign",
			map[string]interface{}{})
		assert.Error(t, err)
	})

	t.Run("rejects non-base64 input", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_cross_sign",
			map[string]interface{}{"foreign_root_pem": "!!!not base64!!!"})
		assert.Error(t, err)
	})

	t.Run("propagates a service failure", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{
			CrossSignFunc: func(context.Context, *service.CrossSignRequest) (*service.CrossSignResponse, error) {
				return nil, service.ErrCertExtNotSupported
			},
		})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_cross_sign",
			map[string]interface{}{
				"foreign_root_pem": base64.StdEncoding.EncodeToString([]byte("r")),
			})
		assert.Error(t, err)
	})
}

// =============================================================================
// Chain
// =============================================================================

func TestExecuteCertificateChainTool(t *testing.T) {
	ctx := context.Background()

	t.Run("returns the chain issuer-first", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{
			ChainFunc: func(_ context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
				assert.Equal(t, []byte("leaf"), req.CertificatePEM)
				return &service.CertificateChainResponse{
					ChainPEM: [][]byte{[]byte("issuer"), []byte("root")},
				}, nil
			},
		})

		result, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_chain",
			map[string]interface{}{
				"certificate_pem": base64.StdEncoding.EncodeToString([]byte("leaf")),
			})
		require.NoError(t, err)

		m, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, []string{"issuer", "root"}, m["chain_pem"])
	})

	t.Run("rejects a missing certificate", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_chain",
			map[string]interface{}{})
		assert.Error(t, err)
	})
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func TestExecuteCertificateDeviceIdentityTool(t *testing.T) {
	ctx := context.Background()

	t.Run("issues a device identity certificate", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{
			IssueDeviceIdentityFunc: func(_ context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
				assert.Equal(t, "idevid", req.Type)
				assert.Equal(t, "device-mcp", req.DeviceID)
				assert.Equal(t, []byte("tcg-csr"), req.CSRPEM)
				assert.Equal(t, "tenant-m", req.TenantID)
				return &service.IssueDeviceIdentityResponse{
					CertificatePEM: []byte("idevid-cert"),
					ChainPEM:       []byte("chain"),
					SerialNumber:   "0f0f",
				}, nil
			},
		})

		result, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_issue_device_identity",
			map[string]interface{}{
				"type":      "idevid",
				"device_id": "device-mcp",
				"csr_pem":   base64.StdEncoding.EncodeToString([]byte("tcg-csr")),
				"tenant_id": "tenant-m",
			})
		require.NoError(t, err)

		m, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "idevid-cert", m["certificate_pem"])
		assert.Equal(t, "0f0f", m["serial_number"])
	})

	t.Run("rejects a missing type", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_issue_device_identity",
			map[string]interface{}{"device_id": "d"})
		assert.Error(t, err)
	})

	t.Run("rejects a missing device ID", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_issue_device_identity",
			map[string]interface{}{"type": "ek"})
		assert.Error(t, err)
	})

	t.Run("rejects non-base64 key material", func(t *testing.T) {
		registry := newCertExtRegistry(t, &MockCertificateService{})
		_, err := registry.executeCertificateTool(ctx, "quicbaas_certificate_issue_device_identity",
			map[string]interface{}{"type": "ek", "device_id": "d", "ek_pub": "!!!"})
		assert.Error(t, err)
	})
}

func TestCertificateExtensionTools_ServiceNotConfigured(t *testing.T) {
	registry := NewToolRegistry(&MockQuicBaaS{}, createTestLogger(t))
	ctx := context.Background()

	for _, tool := range []string{
		"quicbaas_certificate_cross_sign",
		"quicbaas_certificate_chain",
		"quicbaas_certificate_issue_device_identity",
	} {
		t.Run(tool, func(t *testing.T) {
			_, err := registry.executeCertificateTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
		})
	}
}
