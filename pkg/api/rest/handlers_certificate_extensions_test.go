//go:build rest

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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// postCertExt drives one certificate-extension endpoint and returns the
// recorder.
func postCertExt(t *testing.T, path string, body interface{}, setup func(*testutil.MockQuicBaaS),
	handler func(*Server, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {

	t.Helper()

	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()
	setup(mockService)

	var raw []byte
	if s, ok := body.(string); ok {
		raw = []byte(s)
	} else {
		raw, _ = json.Marshal(body)
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(server, w, req)
	return w
}

// =============================================================================
// CrossSign
// =============================================================================

func TestHandleCertificateCrossSign(t *testing.T) {
	t.Run("cross-signs successfully", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/cross-sign",
			CrossSignRESTRequest{ForeignRootPEM: []byte("foreign-root")},
			func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = &MockCertificateService{
					CrossSignFunc: func(_ context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
						assert.Equal(t, []byte("foreign-root"), req.ForeignRootPEM)
						return &service.CrossSignResponse{CrossSignedPEM: []byte("cross-signed")}, nil
					},
				}
			},
			(*Server).HandleCertificateCrossSign)

		require.Equal(t, http.StatusOK, w.Code)
		var resp CrossSignRESTResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, []byte("cross-signed"), resp.CrossSignedPEM)
		assert.NotZero(t, resp.Timestamp)
	})

	t.Run("rejects a missing foreign root", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/cross-sign",
			CrossSignRESTRequest{},
			func(m *testutil.MockQuicBaaS) {},
			(*Server).HandleCertificateCrossSign)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("rejects malformed JSON", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/cross-sign",
			"{not json",
			func(m *testutil.MockQuicBaaS) {},
			(*Server).HandleCertificateCrossSign)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("reports an unavailable certificate service", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/cross-sign",
			CrossSignRESTRequest{ForeignRootPEM: []byte("r")},
			func(m *testutil.MockQuicBaaS) { m.CertificateSvc = nil },
			(*Server).HandleCertificateCrossSign)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("maps an unimplemented CA capability to 501", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/cross-sign",
			CrossSignRESTRequest{ForeignRootPEM: []byte("r")},
			func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = &MockCertificateService{
					CrossSignFunc: func(context.Context, *service.CrossSignRequest) (*service.CrossSignResponse, error) {
						return nil, service.ErrCertExtNotSupported
					},
				}
			},
			(*Server).HandleCertificateCrossSign)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
	})
}

// =============================================================================
// Chain
// =============================================================================

func TestHandleCertificateChain(t *testing.T) {
	t.Run("returns the issuer chain", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/chain",
			CertificateChainRESTRequest{CertificatePEM: []byte("leaf")},
			func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = &MockCertificateService{
					ChainFunc: func(_ context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
						assert.Equal(t, []byte("leaf"), req.CertificatePEM)
						return &service.CertificateChainResponse{
							ChainPEM: [][]byte{[]byte("issuer"), []byte("root")},
						}, nil
					},
				}
			},
			(*Server).HandleCertificateChain)

		require.Equal(t, http.StatusOK, w.Code)
		var resp CertificateChainRESTResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.Len(t, resp.ChainPEM, 2)
		assert.Equal(t, []byte("issuer"), resp.ChainPEM[0], "chain must stay issuer-first")
		assert.Equal(t, []byte("root"), resp.ChainPEM[1], "chain must stay root-last")
	})

	t.Run("rejects a missing certificate", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/chain",
			CertificateChainRESTRequest{},
			func(m *testutil.MockQuicBaaS) {},
			(*Server).HandleCertificateChain)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("propagates a service failure", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/chain",
			CertificateChainRESTRequest{CertificatePEM: []byte("leaf")},
			func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = &MockCertificateService{
					ChainFunc: func(context.Context, *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
						return nil, &errors.QBError{Code: errors.ErrInternal, Op: "Chain", Err: assert.AnError}
					},
				}
			},
			(*Server).HandleCertificateChain)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func TestHandleCertificateDeviceIdentity(t *testing.T) {
	t.Run("issues a device identity certificate", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/device-identity",
			DeviceIdentityRESTRequest{
				Type:         "ek",
				DeviceID:     "device-1",
				EKPub:        []byte("ek-pub"),
				ValidityDays: 30,
				TenantID:     "tenant-a",
			},
			func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = &MockCertificateService{
					IssueDeviceIdentityFunc: func(_ context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
						assert.Equal(t, "ek", req.Type)
						assert.Equal(t, "device-1", req.DeviceID)
						assert.Equal(t, []byte("ek-pub"), req.EKPub)
						assert.Equal(t, 30, req.ValidityDays)
						assert.Equal(t, "tenant-a", req.TenantID)
						return &service.IssueDeviceIdentityResponse{
							CertificatePEM: []byte("device-cert"),
							SerialNumber:   "0a0b",
						}, nil
					},
				}
			},
			(*Server).HandleCertificateDeviceIdentity)

		require.Equal(t, http.StatusOK, w.Code)
		var resp DeviceIdentityRESTResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, []byte("device-cert"), resp.CertificatePEM)
		assert.Equal(t, "0a0b", resp.SerialNumber)
	})

	t.Run("rejects a missing type", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/device-identity",
			DeviceIdentityRESTRequest{DeviceID: "device-1"},
			func(m *testutil.MockQuicBaaS) {},
			(*Server).HandleCertificateDeviceIdentity)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("rejects a missing device ID", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/device-identity",
			DeviceIdentityRESTRequest{Type: "ek"},
			func(m *testutil.MockQuicBaaS) {},
			(*Server).HandleCertificateDeviceIdentity)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("maps an invalid identity type to 400", func(t *testing.T) {
		w := postCertExt(t, "/api/v1/certificate/device-identity",
			DeviceIdentityRESTRequest{Type: "ldevid", DeviceID: "device-1"},
			func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = &MockCertificateService{
					IssueDeviceIdentityFunc: func(context.Context, *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
						return nil, service.ErrCertExtUnknownIdentityType
					},
				}
			},
			(*Server).HandleCertificateDeviceIdentity)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// Authorization mapping
// =============================================================================

// TestCertificateExtensionPEPMapping pins the qrdb-canonical permission each
// certificate-extension route resolves to. Cross-signing mints a CA
// certificate and must NOT be reachable with the leaf-issuance verb.
func TestCertificateExtensionPEPMapping(t *testing.T) {
	cases := []struct {
		path     string
		resource string
		action   string
	}{
		{"/api/v1/certificate/cross-sign", "ca", "create"},
		{"/api/v1/certificate/chain", "certs", "generate"},
		{"/api/v1/certificate/device-identity", "certs", "generate"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, http.MethodPost)
			require.True(t, ok, "route %s must resolve to a permission (default-deny otherwise)", tc.path)
			assert.Equal(t, tc.resource, resource)
			assert.Equal(t, tc.action, action)
		})
	}
}

// TestCrossSignIsNotReachableWithLeafIssuanceVerb guards the privilege
// boundary qrdb documents: certs:generate must not imply the authority to mint
// a signing CA.
func TestCrossSignIsNotReachableWithLeafIssuanceVerb(t *testing.T) {
	resource, action, ok := mapRequestToPermission("/api/v1/certificate/cross-sign", http.MethodPost)
	require.True(t, ok)
	assert.NotEqual(t, "certs:generate", resource+":"+action,
		"cross-signing must require ca:create, not the leaf-issuance verb")
}
