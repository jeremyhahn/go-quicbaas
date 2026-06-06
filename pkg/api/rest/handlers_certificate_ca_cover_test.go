//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestHandleCAInfo_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	certSvc := &MockCertificateService{}
	certSvc.On("CAInfo", mock.Anything, mock.Anything).Return(&service.CAInfoResponse{
		CommonName:   "Test CA",
		Subject:      "CN=Test CA",
		SerialNumber: "01",
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyType:      "RSA",
		KeyBits:      2048,
	}, nil).Once()
	mockService.CertificateSvc = certSvc

	w := httptest.NewRecorder()
	server.HandleCAInfo(w, httptest.NewRequest(http.MethodGet, "/api/v1/certificate/ca/info", nil))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp CAInfoRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "Test CA", resp.CommonName)
	assert.True(t, resp.IsCA)
}

func TestHandleCAInfo_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	certSvc := &MockCertificateService{}
	certSvc.On("CAInfo", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrInternal, "ca", "boom")).Once()
	mockService.CertificateSvc = certSvc

	w := httptest.NewRecorder()
	server.HandleCAInfo(w, httptest.NewRequest(http.MethodGet, "/api/v1/certificate/ca/info", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleCABundle_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	certSvc := &MockCertificateService{}
	certSvc.On("GetCABundle", mock.Anything, mock.Anything).Return(&service.GetCABundleResponse{
		BundlePEM: "-----BEGIN CERTIFICATE-----\nbundle\n-----END CERTIFICATE-----",
	}, nil).Once()
	mockService.CertificateSvc = certSvc

	w := httptest.NewRecorder()
	server.HandleCABundle(w, httptest.NewRequest(http.MethodGet, "/api/v1/certificate/ca/bundle", nil))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp CABundleRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.BundlePEM, "bundle")
}

func TestHandleListCertificates_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	certSvc := &MockCertificateService{}
	certSvc.On("ListCertificates", mock.Anything, mock.MatchedBy(func(r *service.ListCertificatesRequest) bool {
		return r.CommonNameFilter == "svc" && r.IncludeRevoked && r.Limit == 5
	})).Return(&service.ListCertificatesResponse{
		Certificates: []service.CertificateSummary{{
			CommonName: "svc.example", SerialNumber: "0a",
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			DNSNames: []string{"svc.example"},
		}},
		TotalCount: 1,
	}, nil).Once()
	mockService.CertificateSvc = certSvc

	w := httptest.NewRecorder()
	server.HandleListCertificates(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/certificate/ca/list?common_name=svc&include_revoked=true&limit=5", nil))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp ListCertificatesRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Certificates, 1)
	assert.Equal(t, "svc.example", resp.Certificates[0].CommonName)
}

func TestHandleListCertificates_InvalidLimit(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()
	mockService.CertificateSvc = &MockCertificateService{}

	w := httptest.NewRecorder()
	server.HandleListCertificates(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/certificate/ca/list?limit=abc", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = httptest.NewRecorder()
	server.HandleListCertificates(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/certificate/ca/list?limit=-1", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestParseBoolQuery(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "Y", "on"} {
		assert.True(t, parseBoolQuery(v), "value %q should be true", v)
	}
	for _, v := range []string{"", "0", "false", "no", "maybe"} {
		assert.False(t, parseBoolQuery(v), "value %q should be false", v)
	}
}
