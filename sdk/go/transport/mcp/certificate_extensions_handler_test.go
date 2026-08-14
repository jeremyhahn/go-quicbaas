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
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// TestCertificateExtensionToolNames pins the MCP tool each extension method
// resolves to. A mismatch with the server registration
// (pkg/api/mcp/certificate_extensions_mcp.go) surfaces as
// "tool not registered" at runtime, so it is pinned here.
func TestCertificateExtensionToolNames(t *testing.T) {
	cases := map[string]string{
		"CrossSign":           "quicbaas_certificate_cross_sign",
		"Chain":               "quicbaas_certificate_chain",
		"IssueDeviceIdentity": "quicbaas_certificate_issue_device_identity",
	}
	for method, want := range cases {
		got, ok := mapCertificateMethodToTool(method)
		require.True(t, ok, "method %s must map to an MCP tool", method)
		assert.Equal(t, want, got)
	}
}

func TestTransformCrossSignRequest(t *testing.T) {
	h := &certificateHandler{}

	t.Run("encodes the foreign root as base64", func(t *testing.T) {
		args, err := h.transformCrossSignRequest(&certificate.CrossSignRequest{
			ForeignRootPEM: []byte("foreign-root"),
		})
		require.NoError(t, err)
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("foreign-root")), args["foreign_root_pem"])
	})

	t.Run("rejects the wrong request type", func(t *testing.T) {
		_, err := h.transformCrossSignRequest(&certificate.ChainRequest{})
		assert.Error(t, err)
	})

	t.Run("rejects a missing foreign root", func(t *testing.T) {
		_, err := h.transformCrossSignRequest(&certificate.CrossSignRequest{})
		assert.Error(t, err)
	})
}

func TestTransformCrossSignResponse(t *testing.T) {
	h := &certificateHandler{}

	resp, err := h.transformCrossSignResponse(map[string]interface{}{
		"cross_signed_pem": "cross-signed",
	})
	require.NoError(t, err)

	typed, ok := resp.(*certificate.CrossSignResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, []byte("cross-signed"), typed.CrossSignedPEM)
}

func TestTransformChainRequestAndResponse(t *testing.T) {
	h := &certificateHandler{}

	args, err := h.transformChainRequest(&certificate.ChainRequest{CertificatePEM: []byte("leaf")})
	require.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("leaf")), args["certificate_pem"])

	resp, err := h.transformChainResponse(map[string]interface{}{
		"chain_pem": []interface{}{"issuer", "root"},
	})
	require.NoError(t, err)

	typed, ok := resp.(*certificate.ChainResponse)
	require.True(t, ok, "got %T", resp)
	require.Len(t, typed.ChainPEM, 2)
	assert.Equal(t, []byte("issuer"), typed.ChainPEM[0])
	assert.Equal(t, []byte("root"), typed.ChainPEM[1])
}

func TestTransformChainRequest_Errors(t *testing.T) {
	h := &certificateHandler{}

	_, err := h.transformChainRequest(&certificate.CrossSignRequest{})
	assert.Error(t, err)

	_, err = h.transformChainRequest(&certificate.ChainRequest{})
	assert.Error(t, err)
}

func TestTransformIssueDeviceIdentityRequest(t *testing.T) {
	h := &certificateHandler{}

	t.Run("encodes every supplied blob and omits the empty ones", func(t *testing.T) {
		args, err := h.transformIssueDeviceIdentityRequest(&certificate.DeviceIdentityRequest{
			Type:         certificate.DeviceIdentityIDevID,
			DeviceID:     "device-1",
			CSRPEM:       []byte("tcg-csr"),
			ValidityDays: 60,
			TenantID:     "tenant-a",
		})
		require.NoError(t, err)

		assert.Equal(t, "idevid", args["type"])
		assert.Equal(t, "device-1", args["device_id"])
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("tcg-csr")), args["csr_pem"])
		assert.Equal(t, 60, args["validity_days"])
		assert.Equal(t, "tenant-a", args["tenant_id"])

		_, hasEK := args["ek_pub"]
		assert.False(t, hasEK, "absent key material must not be sent as an empty string")
	})

	t.Run("rejects the wrong request type", func(t *testing.T) {
		_, err := h.transformIssueDeviceIdentityRequest(&certificate.ChainRequest{})
		assert.Error(t, err)
	})

	t.Run("rejects a missing type", func(t *testing.T) {
		_, err := h.transformIssueDeviceIdentityRequest(&certificate.DeviceIdentityRequest{DeviceID: "d"})
		assert.Error(t, err)
	})

	t.Run("rejects a missing device ID", func(t *testing.T) {
		_, err := h.transformIssueDeviceIdentityRequest(&certificate.DeviceIdentityRequest{
			Type: certificate.DeviceIdentityEK,
		})
		assert.Error(t, err)
	})
}

func TestTransformIssueDeviceIdentityResponse(t *testing.T) {
	h := &certificateHandler{}

	resp, err := h.transformIssueDeviceIdentityResponse(map[string]interface{}{
		"certificate_pem": "device-cert",
		"chain_pem":       "chain",
		"serial_number":   "0a",
	})
	require.NoError(t, err)

	typed, ok := resp.(*certificate.DeviceIdentityResponse)
	require.True(t, ok, "got %T", resp)
	assert.Equal(t, []byte("device-cert"), typed.CertificatePEM)
	assert.Equal(t, []byte("chain"), typed.ChainPEM)
	assert.Equal(t, "0a", typed.SerialNumber)
}
