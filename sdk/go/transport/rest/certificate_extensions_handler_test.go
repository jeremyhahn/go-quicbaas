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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// =============================================================================
// CrossSign
// =============================================================================

func TestCertificateRESTHandler_CrossSign(t *testing.T) {
	var gotPath, gotMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method

		var req crossSignRESTWireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, []byte("foreign-root"), req.ForeignRootPEM)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(crossSignRESTWireResponse{
			CrossSignedPEM: []byte("cross-signed"),
		}))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "CrossSign",
		&certificate.CrossSignRequest{ForeignRootPEM: []byte("foreign-root")})
	require.NoError(t, err)

	assert.Equal(t, "/api/v1/certificate/cross-sign", gotPath)
	assert.Equal(t, http.MethodPost, gotMethod)

	typed, ok := resp.(*certificate.CrossSignResponse)
	require.True(t, ok, "CrossSign must return *certificate.CrossSignResponse, got %T", resp)
	assert.Equal(t, []byte("cross-signed"), typed.CrossSignedPEM)
}

func TestCertificateRESTHandler_CrossSign_Validation(t *testing.T) {
	h := &certificateRESTHandler{}
	ctx := context.Background()

	_, err := h.handleCrossSign(ctx, nil)
	assert.Error(t, err)

	_, err = h.handleCrossSign(ctx, &certificate.ChainRequest{})
	assert.Error(t, err)

	_, err = h.handleCrossSign(ctx, &certificate.CrossSignRequest{})
	assert.Error(t, err)
}

// =============================================================================
// Chain
// =============================================================================

func TestCertificateRESTHandler_Chain(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		var req certificateChainRESTWireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, []byte("leaf"), req.CertificatePEM)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(certificateChainRESTWireResponse{
			ChainPEM: [][]byte{[]byte("issuer"), []byte("root")},
		}))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "Chain",
		&certificate.ChainRequest{CertificatePEM: []byte("leaf")})
	require.NoError(t, err)

	assert.Equal(t, "/api/v1/certificate/chain", gotPath)

	typed, ok := resp.(*certificate.ChainResponse)
	require.True(t, ok, "Chain must return *certificate.ChainResponse, got %T", resp)
	require.Len(t, typed.ChainPEM, 2)
	assert.Equal(t, []byte("issuer"), typed.ChainPEM[0], "chain must stay issuer-first")
	assert.Equal(t, []byte("root"), typed.ChainPEM[1], "chain must stay root-last")
}

func TestCertificateRESTHandler_Chain_Validation(t *testing.T) {
	h := &certificateRESTHandler{}
	ctx := context.Background()

	_, err := h.handleChain(ctx, &certificate.CrossSignRequest{})
	assert.Error(t, err)

	_, err = h.handleChain(ctx, &certificate.ChainRequest{})
	assert.Error(t, err)
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func TestCertificateRESTHandler_IssueDeviceIdentity(t *testing.T) {
	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		var req deviceIdentityRESTWireRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "ek", req.Type)
		assert.Equal(t, "device-rest", req.DeviceID)
		assert.Equal(t, []byte("ek-pub"), req.EKPub)
		assert.Equal(t, 14, req.ValidityDays)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(deviceIdentityRESTWireResponse{
			CertificatePEM: []byte("ek-cert"),
			ChainPEM:       []byte("chain"),
			SerialNumber:   "1234",
		}))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "quicbaas.Certificate", "IssueDeviceIdentity",
		&certificate.DeviceIdentityRequest{
			Type:         certificate.DeviceIdentityEK,
			DeviceID:     "device-rest",
			EKPub:        []byte("ek-pub"),
			ValidityDays: 14,
		})
	require.NoError(t, err)

	assert.Equal(t, "/api/v1/certificate/device-identity", gotPath)

	typed, ok := resp.(*certificate.DeviceIdentityResponse)
	require.True(t, ok, "IssueDeviceIdentity must return *certificate.DeviceIdentityResponse, got %T", resp)
	assert.Equal(t, []byte("ek-cert"), typed.CertificatePEM)
	assert.Equal(t, "1234", typed.SerialNumber)
}

func TestCertificateRESTHandler_IssueDeviceIdentity_Validation(t *testing.T) {
	h := &certificateRESTHandler{}
	ctx := context.Background()

	_, err := h.handleIssueDeviceIdentity(ctx, &certificate.ChainRequest{})
	assert.Error(t, err)

	_, err = h.handleIssueDeviceIdentity(ctx, &certificate.DeviceIdentityRequest{DeviceID: "d"})
	assert.Error(t, err)

	_, err = h.handleIssueDeviceIdentity(ctx, &certificate.DeviceIdentityRequest{
		Type: certificate.DeviceIdentityEK,
	})
	assert.Error(t, err)
}

// TestCertificateExtensionEndpointMapping pins the endpoint each extension
// verb resolves to. Without explicit routing the generic fallback would
// lowercase the method name and produce "/api/v1/certificate/crosssign".
func TestCertificateExtensionEndpointMapping(t *testing.T) {
	cases := map[string]string{
		"CrossSign":           "/api/v1/certificate/cross-sign",
		"Chain":               "/api/v1/certificate/chain",
		"IssueDeviceIdentity": "/api/v1/certificate/device-identity",
	}
	tr := &Transport{}
	for method, wantPath := range cases {
		gotPath, gotMethod := tr.mapCertificateMethod(method)
		assert.Equal(t, wantPath, gotPath, "method %s", method)
		assert.Equal(t, http.MethodPost, gotMethod, "method %s", method)
	}
}
