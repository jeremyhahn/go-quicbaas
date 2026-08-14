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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// MockCertificateService is a mock implementation of service.CertificateService.
type MockCertificateService struct {
	mock.Mock

	CrossSignFunc           func(ctx context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error)
	ChainFunc               func(ctx context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error)
	IssueDeviceIdentityFunc func(ctx context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error)
}

func (m *MockCertificateService) IssueCertificate(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.IssueCertificateResponse), args.Error(1)
}

func (m *MockCertificateService) SignCSR(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SignCSRResponse), args.Error(1)
}

func (m *MockCertificateService) RevokeCertificate(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RevokeCertificateResponse), args.Error(1)
}

func (m *MockCertificateService) VerifyCertificate(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.VerifyCertificateResponse), args.Error(1)
}

func (m *MockCertificateService) GetCACertificate(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetCACertificateResponse), args.Error(1)
}

func (m *MockCertificateService) GetTLSConfig(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetTLSConfigResponse), args.Error(1)
}

func (m *MockCertificateService) IsInitialized() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockCertificateService) CAService() xkmstransport.Client {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(xkmstransport.Client)
}

func (m *MockCertificateService) GetCABundle(ctx context.Context, req *service.GetCABundleRequest) (*service.GetCABundleResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetCABundleResponse), args.Error(1)
}

func (m *MockCertificateService) GenerateCRL(ctx context.Context, req *service.GenerateCRLRequest) (*service.GenerateCRLResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateCRLResponse), args.Error(1)
}

func (m *MockCertificateService) IsRevoked(ctx context.Context, req *service.IsRevokedRequest) (*service.IsRevokedResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.IsRevokedResponse), args.Error(1)
}
func (m *MockCertificateService) CAInfo(ctx context.Context, req *service.CAInfoRequest) (*service.CAInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CAInfoResponse), args.Error(1)
}

func (m *MockCertificateService) ListCertificates(ctx context.Context, req *service.ListCertificatesRequest) (*service.ListCertificatesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListCertificatesResponse), args.Error(1)
}

// =============================================================================
// Test HandleIssueCertificate
// =============================================================================

func TestHandleIssueCertificate(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "issue certificate successfully",
			body: IssueCertificateRESTRequest{
				CommonName:         "test.example.com",
				Organization:       "Test Org",
				OrganizationalUnit: "Test Unit",
				Country:            "US",
				Province:           "CA",
				Locality:           "San Francisco",
				DNSNames:           []string{"test.example.com", "www.test.example.com"},
				IPAddresses:        []string{"192.168.1.1"},
				EmailAddresses:     []string{"admin@example.com"},
				URIs:               []string{"https://example.com"},
				ValidityDays:       365,
				Usage:              "server",
				IsCA:               false,
				MaxPathLen:         0,
				KeyAlgorithm:       "ecdsa",
				KeySize:            256,
				Metadata:           map[string]string{"env": "test"},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("IssueCertificate", mock.Anything, mock.MatchedBy(func(req *service.IssueCertificateRequest) bool {
					return req.CommonName == "test.example.com" &&
						req.Organization == "Test Org" &&
						req.Usage == "server"
				})).Return(&service.IssueCertificateResponse{
					CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
					PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----",
					CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
					ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
					SerialNumber:     "123456789",
					NotBefore:        time.Now(),
					NotAfter:         time.Now().Add(365 * 24 * time.Hour),
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp IssueCertificateRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Contains(t, resp.CertificatePEM, "BEGIN CERTIFICATE")
				assert.Contains(t, resp.PrivateKeyPEM, "BEGIN PRIVATE KEY")
				assert.Equal(t, "123456789", resp.SerialNumber)
				assert.NotZero(t, resp.NotBefore)
				assert.NotZero(t, resp.NotAfter)
				assert.NotZero(t, resp.Timestamp)
			},
		},
		{
			name: "issue certificate with minimal fields",
			body: IssueCertificateRESTRequest{
				CommonName: "minimal.example.com",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).Return(&service.IssueCertificateResponse{
					CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
					PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----",
					CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
					ChainPEM:         "",
					SerialNumber:     "987654321",
					NotBefore:        time.Now(),
					NotAfter:         time.Now().Add(90 * 24 * time.Hour),
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "missing common_name",
			body: IssueCertificateRESTRequest{
				Organization: "Test Org",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "common_name is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid JSON")
			},
		},
		{
			name: "certificate service not available",
			body: IssueCertificateRESTRequest{
				CommonName: "test.example.com",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = nil
			},
			expectedStatus: http.StatusServiceUnavailable,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "certificate service not available")
			},
		},
		{
			name: "service returns error",
			body: IssueCertificateRESTRequest{
				CommonName: "error.example.com",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "IssueCertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "service returns invalid input error",
			body: IssueCertificateRESTRequest{
				CommonName:   "bad.example.com",
				KeyAlgorithm: "invalid-algo",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInvalidInput,
					Op:   "IssueCertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/certificate/issue", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleIssueCertificate(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Test HandleSignCSR
// =============================================================================

func TestHandleSignCSR(t *testing.T) {
	validCSRPEM := "-----BEGIN CERTIFICATE REQUEST-----\nMIICVDCCATwCAQAwDzENMAsGA1UEAxMEdGVzdDAeFw0yNTAxMTUwMDAwMDBaFw0y\nNjAxMTUwMDAwMDBaMDoxCzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEeMBwGA1UE\nChMVVGVzdCBPcmdhbml6YXRpb24gTExDMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A\n-----END CERTIFICATE REQUEST-----"

	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "sign CSR successfully",
			body: SignCSRRESTRequest{
				CSRPEM:       validCSRPEM,
				ValidityDays: 365,
				Usage:        "server",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("SignCSR", mock.Anything, mock.MatchedBy(func(req *service.SignCSRRequest) bool {
					return req.CSRPEM == validCSRPEM && req.ValidityDays == 365 && req.Usage == "server"
				})).Return(&service.SignCSRResponse{
					CertificatePEM:   "-----BEGIN CERTIFICATE-----\nsigned-cert\n-----END CERTIFICATE-----",
					CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
					ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
					SerialNumber:     "111222333",
					NotBefore:        time.Now(),
					NotAfter:         time.Now().Add(365 * 24 * time.Hour),
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SignCSRRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Contains(t, resp.CertificatePEM, "BEGIN CERTIFICATE")
				assert.Equal(t, "111222333", resp.SerialNumber)
				assert.NotZero(t, resp.Timestamp)
			},
		},
		{
			name: "sign CSR with client usage",
			body: SignCSRRESTRequest{
				CSRPEM: validCSRPEM,
				Usage:  "client",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("SignCSR", mock.Anything, mock.Anything).Return(&service.SignCSRResponse{
					CertificatePEM:   "-----BEGIN CERTIFICATE-----\nclient-cert\n-----END CERTIFICATE-----",
					CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
					ChainPEM:         "",
					SerialNumber:     "444555666",
					NotBefore:        time.Now(),
					NotAfter:         time.Now().Add(90 * 24 * time.Hour),
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "missing csr_pem",
			body: SignCSRRESTRequest{
				ValidityDays: 365,
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "csr_pem is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "certificate service not available",
			body: SignCSRRESTRequest{
				CSRPEM: validCSRPEM,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = nil
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name: "service returns error",
			body: SignCSRRESTRequest{
				CSRPEM: validCSRPEM,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("SignCSR", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInvalidInput,
					Op:   "SignCSR",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/certificate/sign-csr", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleSignCSR(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Test HandleRevokeCertificate
// =============================================================================

func TestHandleRevokeCertificate(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "revoke certificate successfully",
			body: RevokeCertificateRESTRequest{
				SerialNumber: "123456789",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("RevokeCertificate", mock.Anything, mock.MatchedBy(func(req *service.RevokeCertificateRequest) bool {
					return req.SerialNumber == "123456789"
				})).Return(&service.RevokeCertificateResponse{
					Success: true,
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RevokeCertificateRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.True(t, resp.Success)
				assert.NotZero(t, resp.Timestamp)
			},
		},
		{
			name: "missing serial_number",
			body: RevokeCertificateRESTRequest{
				SerialNumber: "",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "serial_number is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "certificate service not available",
			body: RevokeCertificateRESTRequest{
				SerialNumber: "123456789",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = nil
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name: "service returns not found error",
			body: RevokeCertificateRESTRequest{
				SerialNumber: "nonexistent",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("RevokeCertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "RevokeCertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service returns internal error",
			body: RevokeCertificateRESTRequest{
				SerialNumber: "error-serial",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("RevokeCertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "RevokeCertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/certificate/revoke", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleRevokeCertificate(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Test HandleVerifyCertificate
// =============================================================================

func TestHandleVerifyCertificate(t *testing.T) {
	validCertPEM := "-----BEGIN CERTIFICATE-----\nMIICVDCCATwCAQAwDzENMAsGA1UEAxMEdGVzdDAeFw0yNTAxMTUwMDAwMDBaFw0y\nNjAxMTUwMDAwMDBaMDoxCzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEeMBwGA1UE\nChMVVGVzdCBPcmdhbml6YXRpb24gTExDMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A\n-----END CERTIFICATE-----"

	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "verify valid certificate",
			body: VerifyCertificateRESTRequest{
				CertificatePEM: validCertPEM,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("VerifyCertificate", mock.Anything, mock.MatchedBy(func(req *service.VerifyCertificateRequest) bool {
					return req.CertificatePEM == validCertPEM
				})).Return(&service.VerifyCertificateResponse{
					Valid:        true,
					ErrorMessage: "",
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp VerifyCertificateRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.True(t, resp.Valid)
				assert.Empty(t, resp.ErrorMessage)
				assert.NotZero(t, resp.Timestamp)
			},
		},
		{
			name: "verify invalid certificate",
			body: VerifyCertificateRESTRequest{
				CertificatePEM: validCertPEM,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).Return(&service.VerifyCertificateResponse{
					Valid:        false,
					ErrorMessage: "certificate has expired",
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp VerifyCertificateRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.False(t, resp.Valid)
				assert.Equal(t, "certificate has expired", resp.ErrorMessage)
			},
		},
		{
			name: "missing certificate_pem",
			body: VerifyCertificateRESTRequest{
				CertificatePEM: "",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "certificate_pem is required")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "certificate service not available",
			body: VerifyCertificateRESTRequest{
				CertificatePEM: validCertPEM,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = nil
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name: "service returns error",
			body: VerifyCertificateRESTRequest{
				CertificatePEM: validCertPEM,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "VerifyCertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/certificate/verify", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleVerifyCertificate(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Test HandleGetCACertificate
// =============================================================================

func TestHandleGetCACertificate(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get CA certificate successfully",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(&service.GetCACertificateResponse{
					CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
					CABundlePEM:      "-----BEGIN CERTIFICATE-----\nca-bundle\n-----END CERTIFICATE-----",
					Identity:         "QuicBaaS CA",
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp GetCACertificateRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.Contains(t, resp.CACertificatePEM, "BEGIN CERTIFICATE")
				assert.Contains(t, resp.CABundlePEM, "BEGIN CERTIFICATE")
				assert.Equal(t, "QuicBaaS CA", resp.Identity)
				assert.NotZero(t, resp.Timestamp)
			},
		},
		{
			name: "certificate service not available",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = nil
			},
			expectedStatus: http.StatusServiceUnavailable,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "certificate service not available")
			},
		},
		{
			name: "service returns error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GetCACertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "service returns not found error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GetCACertificate",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/certificate/ca", nil)
			w := httptest.NewRecorder()

			server.HandleGetCACertificate(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Test HandleGetTLSConfig
// =============================================================================

func TestHandleGetTLSConfig(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get TLS config successfully",
			body: GetTLSConfigRESTRequest{
				ServerName:        "example.com",
				RequireClientCert: true,
				IsServer:          true,
				MinVersion:        "1.3",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetTLSConfig", mock.Anything, mock.MatchedBy(func(req *service.GetTLSConfigRequest) bool {
					return req.ServerName == "example.com" &&
						req.RequireClientCert == true &&
						req.IsServer == true &&
						req.MinVersion == "1.3"
				})).Return(&service.GetTLSConfigResponse{
					Config: &tls.Config{},
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp GetTLSConfigRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.True(t, resp.Configured)
				assert.NotZero(t, resp.Timestamp)
			},
		},
		{
			name: "get TLS config with empty body",
			body: nil,
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(&service.GetTLSConfigResponse{
					Config: &tls.Config{},
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp GetTLSConfigRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)
				assert.True(t, resp.Configured)
			},
		},
		{
			name: "get TLS config with minimal settings",
			body: GetTLSConfigRESTRequest{
				MinVersion: "1.2",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(&service.GetTLSConfigResponse{
					Config: &tls.Config{},
				}, nil).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				assert.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid JSON")
			},
		},
		{
			name: "certificate service not available",
			body: GetTLSConfigRESTRequest{
				ServerName: "example.com",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.CertificateSvc = nil
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name: "service returns error",
			body: GetTLSConfigRESTRequest{
				ServerName: "error.com",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCertSvc := &MockCertificateService{}
				mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GetTLSConfig",
					Err:  assert.AnError,
				}).Once()
				m.CertificateSvc = mockCertSvc
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if tt.body == nil {
				bodyBytes = []byte{}
			} else if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/certificate/tls-config", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleGetTLSConfig(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

// =============================================================================
// Test Route Registration
// =============================================================================

func TestCertificateRouteRegistration(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	// Setup mock certificate service for all endpoints
	mockCertSvc := &MockCertificateService{}
	mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).Return(&service.IssueCertificateResponse{
		CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
		PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----",
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		SerialNumber:     "123",
		NotBefore:        time.Now(),
		NotAfter:         time.Now().Add(time.Hour),
	}, nil).Maybe()
	mockCertSvc.On("SignCSR", mock.Anything, mock.Anything).Return(&service.SignCSRResponse{
		CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		SerialNumber:     "456",
		NotBefore:        time.Now(),
		NotAfter:         time.Now().Add(time.Hour),
	}, nil).Maybe()
	mockCertSvc.On("RevokeCertificate", mock.Anything, mock.Anything).Return(&service.RevokeCertificateResponse{
		Success: true,
	}, nil).Maybe()
	mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).Return(&service.VerifyCertificateResponse{
		Valid: true,
	}, nil).Maybe()
	mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(&service.GetCACertificateResponse{
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		CABundlePEM:      "-----BEGIN CERTIFICATE-----\nbundle\n-----END CERTIFICATE-----",
		Identity:         "Test CA",
	}, nil).Maybe()
	mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(&service.GetTLSConfigResponse{
		Config: &tls.Config{},
	}, nil).Maybe()
	mockService.CertificateSvc = mockCertSvc

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		expectedStatus int
	}{
		{
			name:   "POST /certificate/issue",
			method: http.MethodPost,
			path:   "/api/v1/certificate/issue",
			body: IssueCertificateRESTRequest{
				CommonName: "test.example.com",
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:   "POST /certificate/sign-csr",
			method: http.MethodPost,
			path:   "/api/v1/certificate/sign-csr",
			body: SignCSRRESTRequest{
				CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "POST /certificate/revoke",
			method: http.MethodPost,
			path:   "/api/v1/certificate/revoke",
			body: RevokeCertificateRESTRequest{
				SerialNumber: "123456",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "POST /certificate/verify",
			method: http.MethodPost,
			path:   "/api/v1/certificate/verify",
			body: VerifyCertificateRESTRequest{
				CertificatePEM: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "GET /certificate/ca",
			method:         http.MethodGet,
			path:           "/api/v1/certificate/ca",
			body:           nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST /certificate/tls-config",
			method:         http.MethodPost,
			path:           "/api/v1/certificate/tls-config",
			body:           GetTLSConfigRESTRequest{},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(bodyBytes))
			if tt.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "endpoint %s %s returned unexpected status", tt.method, tt.path)
		})
	}
}

// CrossSign satisfies service.CertificateService. The certificate-extension
// verbs are exercised by the dedicated extension tests; this double records
// the call and returns the configured response.
func (m *MockCertificateService) CrossSign(ctx context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
	if m.CrossSignFunc != nil {
		return m.CrossSignFunc(ctx, req)
	}
	return &service.CrossSignResponse{}, nil
}

// Chain satisfies service.CertificateService.
func (m *MockCertificateService) Chain(ctx context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
	if m.ChainFunc != nil {
		return m.ChainFunc(ctx, req)
	}
	return &service.CertificateChainResponse{}, nil
}

// IssueDeviceIdentity satisfies service.CertificateService.
func (m *MockCertificateService) IssueDeviceIdentity(ctx context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
	if m.IssueDeviceIdentityFunc != nil {
		return m.IssueDeviceIdentityFunc(ctx, req)
	}
	return &service.IssueDeviceIdentityResponse{}, nil
}
