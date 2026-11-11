//go:build mcp

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

package mcp

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

// testQuicBaaSWithCertificateService wraps MockQuicBaaS and provides a configurable CertificateService.
type testQuicBaaSWithCertificateService struct {
	*MockQuicBaaS
	certSvc service.CertificateService
}

func (m *testQuicBaaSWithCertificateService) Certificate() service.CertificateService {
	return m.certSvc
}

func (m *testQuicBaaSWithCertificateService) SetSignalService(svc interface{}) {}

func newTestQuicBaaSWithCertificateService(certSvc service.CertificateService) *testQuicBaaSWithCertificateService {
	return &testQuicBaaSWithCertificateService{
		MockQuicBaaS: &MockQuicBaaS{},
		certSvc:      certSvc,
	}
}

// createTestLogger creates a logger for testing.
func createTestLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)
	return log
}

// TestGetAllCertificateTools tests that all 6 certificate tools are returned.
func TestGetAllCertificateTools(t *testing.T) {
	tools := getAllCertificateTools()
	assert.Len(t, tools, 9, "expected 6 base certificate tools plus 3 extension tools")

	expectedTools := map[string]bool{
		"quicbaas_certificate_issue":          false,
		"quicbaas_certificate_sign_csr":       false,
		"quicbaas_certificate_revoke":         false,
		"quicbaas_certificate_verify":         false,
		"quicbaas_certificate_get_ca":         false,
		"quicbaas_certificate_get_tls_config": false,

		// Certificate extensions — Security().CertificateExt().
		"quicbaas_certificate_cross_sign":            false,
		"quicbaas_certificate_chain":                 false,
		"quicbaas_certificate_issue_device_identity": false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool name: %s", tool.Name)
		}
		expectedTools[tool.Name] = true
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

// TestIsCertificateTool tests the isCertificateTool helper function.
func TestIsCertificateTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"issue tool", "quicbaas_certificate_issue", true},
		{"sign csr tool", "quicbaas_certificate_sign_csr", true},
		{"revoke tool", "quicbaas_certificate_revoke", true},
		{"verify tool", "quicbaas_certificate_verify", true},
		{"get ca tool", "quicbaas_certificate_get_ca", true},
		{"get tls config tool", "quicbaas_certificate_get_tls_config", true},
		{"unknown tool", "quicbaas_unknown", false},
		{"kms tool", "quicbaas_kms_generate_key", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCertificateTool(tt.toolName)
			assert.Equal(t, tt.expected, result, "isCertificateTool(%s)", tt.toolName)
		})
	}
}

// TestCertificateToolsServiceNotConfigured tests error handling when certificate service is not configured.
func TestCertificateToolsServiceNotConfigured(t *testing.T) {
	log := createTestLogger(t)
	mockSvc := &MockQuicBaaS{} // Returns nil for Certificate()
	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "issue - service not configured",
			toolName: "quicbaas_certificate_issue",
			args: map[string]interface{}{
				"common_name": "test.example.com",
			},
		},
		{
			name:     "sign csr - service not configured",
			toolName: "quicbaas_certificate_sign_csr",
			args: map[string]interface{}{
				"csr_pem": "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----",
			},
		},
		{
			name:     "revoke - service not configured",
			toolName: "quicbaas_certificate_revoke",
			args: map[string]interface{}{
				"serial_number": "12345",
			},
		},
		{
			name:     "verify - service not configured",
			toolName: "quicbaas_certificate_verify",
			args: map[string]interface{}{
				"certificate_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
			},
		},
		{
			name:     "get ca - service not configured",
			toolName: "quicbaas_certificate_get_ca",
			args:     map[string]interface{}{},
		},
		{
			name:     "get tls config - service not configured",
			toolName: "quicbaas_certificate_get_tls_config",
			args:     map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not configured")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "certificate service not configured")
		})
	}
}

// TestCertificateIssue tests the quicbaas_certificate_issue tool.
func TestCertificateIssue(t *testing.T) {
	t.Run("success with minimal parameters", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		now := time.Now()
		notAfter := now.Add(365 * 24 * time.Hour)

		mockCertSvc.On("IssueCertificate", mock.Anything, mock.MatchedBy(func(req *service.IssueCertificateRequest) bool {
			return req.CommonName == "test.example.com"
		})).Return(&service.IssueCertificateResponse{
			CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
			PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----",
			CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
			ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
			SerialNumber:     "12345",
			NotBefore:        now,
			NotAfter:         notAfter,
		}, nil)

		args := map[string]interface{}{
			"common_name": "test.example.com",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_issue", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Contains(t, resultMap["certificate_pem"], "test-cert")
		assert.Contains(t, resultMap["private_key_pem"], "test-key")
		assert.Equal(t, "12345", resultMap["serial_number"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("success with all parameters", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		now := time.Now()
		notAfter := now.Add(365 * 24 * time.Hour)

		mockCertSvc.On("IssueCertificate", mock.Anything, mock.MatchedBy(func(req *service.IssueCertificateRequest) bool {
			return req.CommonName == "test.example.com" &&
				req.Organization == "Test Org" &&
				req.OrganizationalUnit == "Engineering" &&
				req.Country == "US" &&
				req.Province == "CA" &&
				req.Locality == "San Francisco" &&
				req.Usage == "server" &&
				req.KeyAlgorithm == "ecdsa" &&
				req.ValidityDays == 365 &&
				req.IsCA == true &&
				req.MaxPathLen == 1 &&
				req.KeySize == 256 &&
				len(req.DNSNames) == 2 &&
				len(req.IPAddresses) == 1 &&
				len(req.EmailAddresses) == 1
		})).Return(&service.IssueCertificateResponse{
			CertificatePEM:   "-----BEGIN CERTIFICATE-----\nfull-cert\n-----END CERTIFICATE-----",
			PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\nfull-key\n-----END PRIVATE KEY-----",
			CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
			ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
			SerialNumber:     "67890",
			NotBefore:        now,
			NotAfter:         notAfter,
		}, nil)

		args := map[string]interface{}{
			"common_name":         "test.example.com",
			"organization":        "Test Org",
			"organizational_unit": "Engineering",
			"country":             "US",
			"province":            "CA",
			"locality":            "San Francisco",
			"dns_names":           `["test.example.com", "www.test.example.com"]`,
			"ip_addresses":        `["192.0.2.1"]`,
			"email_addresses":     `["admin@example.com"]`,
			"uris":                `["https://example.com"]`,
			"validity_days":       365,
			"usage":               "server",
			"is_ca":               "true",
			"max_path_len":        1,
			"key_algorithm":       "ecdsa",
			"key_size":            256,
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_issue", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Contains(t, resultMap["certificate_pem"], "full-cert")
		assert.Equal(t, "67890", resultMap["serial_number"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("error missing common_name", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		args := map[string]interface{}{
			"organization": "Test Org",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_issue", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "common_name is required")
	})

	t.Run("error from service", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).Return(
			nil, errors.New("internal certificate error"))

		args := map[string]interface{}{
			"common_name": "test.example.com",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_issue", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "issue certificate failed")

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateSignCSR tests the quicbaas_certificate_sign_csr tool.
func TestCertificateSignCSR(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		now := time.Now()
		notAfter := now.Add(365 * 24 * time.Hour)

		mockCertSvc.On("SignCSR", mock.Anything, mock.MatchedBy(func(req *service.SignCSRRequest) bool {
			return req.CSRPEM == "-----BEGIN CERTIFICATE REQUEST-----\ntest-csr\n-----END CERTIFICATE REQUEST-----" &&
				req.Usage == "client" &&
				req.ValidityDays == 180
		})).Return(&service.SignCSRResponse{
			CertificatePEM:   "-----BEGIN CERTIFICATE-----\nsigned-cert\n-----END CERTIFICATE-----",
			CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
			ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
			SerialNumber:     "99999",
			NotBefore:        now,
			NotAfter:         notAfter,
		}, nil)

		args := map[string]interface{}{
			"csr_pem":       "-----BEGIN CERTIFICATE REQUEST-----\ntest-csr\n-----END CERTIFICATE REQUEST-----",
			"validity_days": "180",
			"usage":         "client",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_sign_csr", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Contains(t, resultMap["certificate_pem"], "signed-cert")
		assert.Equal(t, "99999", resultMap["serial_number"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("error missing csr_pem", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		args := map[string]interface{}{
			"validity_days": "180",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_sign_csr", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "csr_pem is required")
	})

	t.Run("error from service", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("SignCSR", mock.Anything, mock.Anything).Return(
			nil, errors.New("invalid CSR format"))

		args := map[string]interface{}{
			"csr_pem": "invalid-csr",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_sign_csr", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "sign csr failed")

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateRevoke tests the quicbaas_certificate_revoke tool.
func TestCertificateRevoke(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("RevokeCertificate", mock.Anything, mock.MatchedBy(func(req *service.RevokeCertificateRequest) bool {
			return req.SerialNumber == "12345"
		})).Return(&service.RevokeCertificateResponse{
			Success: true,
		}, nil)

		args := map[string]interface{}{
			"serial_number": "12345",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_revoke", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, true, resultMap["success"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("error missing serial_number", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		args := map[string]interface{}{}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_revoke", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "serial_number is required")
	})

	t.Run("error from service", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("RevokeCertificate", mock.Anything, mock.Anything).Return(
			nil, errors.New("certificate not found"))

		args := map[string]interface{}{
			"serial_number": "nonexistent",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_revoke", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "revoke certificate failed")

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateVerify tests the quicbaas_certificate_verify tool.
func TestCertificateVerify(t *testing.T) {
	t.Run("success valid certificate", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("VerifyCertificate", mock.Anything, mock.MatchedBy(func(req *service.VerifyCertificateRequest) bool {
			return req.CertificatePEM == "-----BEGIN CERTIFICATE-----\nvalid-cert\n-----END CERTIFICATE-----"
		})).Return(&service.VerifyCertificateResponse{
			Valid: true,
		}, nil)

		args := map[string]interface{}{
			"certificate_pem": "-----BEGIN CERTIFICATE-----\nvalid-cert\n-----END CERTIFICATE-----",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_verify", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, true, resultMap["valid"])
		_, hasError := resultMap["error_message"]
		assert.False(t, hasError, "should not have error_message for valid cert")

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("success invalid certificate with error message", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).Return(&service.VerifyCertificateResponse{
			Valid:        false,
			ErrorMessage: "certificate has expired",
		}, nil)

		args := map[string]interface{}{
			"certificate_pem": "-----BEGIN CERTIFICATE-----\nexpired-cert\n-----END CERTIFICATE-----",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_verify", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, false, resultMap["valid"])
		assert.Equal(t, "certificate has expired", resultMap["error_message"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("error missing certificate_pem", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		args := map[string]interface{}{}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_verify", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "certificate_pem is required")
	})

	t.Run("error from service", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).Return(
			nil, errors.New("CA not initialized"))

		args := map[string]interface{}{
			"certificate_pem": "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_verify", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "verify certificate failed")

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateGetCA tests the quicbaas_certificate_get_ca tool.
func TestCertificateGetCA(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(&service.GetCACertificateResponse{
			CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
			CABundlePEM:      "-----BEGIN CERTIFICATE-----\nbundle\n-----END CERTIFICATE-----",
			Identity:         "CN=QuicBaaS CA,O=QuicBaaS",
		}, nil)

		args := map[string]interface{}{}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_get_ca", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Contains(t, resultMap["ca_certificate_pem"], "ca-cert")
		assert.Contains(t, resultMap["ca_bundle_pem"], "bundle")
		assert.Equal(t, "CN=QuicBaaS CA,O=QuicBaaS", resultMap["identity"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("error from service", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(
			nil, errors.New("CA not available"))

		args := map[string]interface{}{}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_get_ca", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get ca certificate failed")

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateGetTLSConfig tests the quicbaas_certificate_get_tls_config tool.
func TestCertificateGetTLSConfig(t *testing.T) {
	t.Run("success with config", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("GetTLSConfig", mock.Anything, mock.MatchedBy(func(req *service.GetTLSConfigRequest) bool {
			return req.ServerName == "test.example.com" &&
				req.RequireClientCert == true &&
				req.IsServer == true &&
				req.MinVersion == "1.3"
		})).Return(&service.GetTLSConfigResponse{
			Config: &tls.Config{
				MinVersion: tls.VersionTLS13,
				ServerName: "test.example.com",
			},
		}, nil)

		args := map[string]interface{}{
			"server_name":         "test.example.com",
			"require_client_cert": "true",
			"is_server":           "true",
			"min_version":         "1.3",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_get_tls_config", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, true, resultMap["configured"])
		assert.Equal(t, uint16(tls.VersionTLS13), resultMap["min_version"])
		assert.Equal(t, "test.example.com", resultMap["server_name"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("success with nil config", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(&service.GetTLSConfigResponse{
			Config: nil,
		}, nil)

		args := map[string]interface{}{}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_get_tls_config", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		resultMap, ok := result.(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, false, resultMap["configured"])

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("error from service", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(
			nil, errors.New("TLS configuration error"))

		args := map[string]interface{}{}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_get_tls_config", args)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get tls config failed")

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateToolsRegistration tests that all certificate tools are properly registered.
func TestCertificateToolsRegistration(t *testing.T) {
	log := createTestLogger(t)
	mockCertSvc := new(MockCertificateService)
	mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
	registry := NewToolRegistry(mockSvc, log)

	tools := registry.ListTools()
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{
		"quicbaas_certificate_issue",
		"quicbaas_certificate_sign_csr",
		"quicbaas_certificate_revoke",
		"quicbaas_certificate_verify",
		"quicbaas_certificate_get_ca",
		"quicbaas_certificate_get_tls_config",
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
}

// TestCertificateToolSchemas tests the tool schemas for each certificate tool.
func TestCertificateToolSchemas(t *testing.T) {
	t.Run("issue tool schema", func(t *testing.T) {
		tool := CertificateIssueTool
		assert.Equal(t, "quicbaas_certificate_issue", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type)
		assert.Contains(t, tool.InputSchema.Required, "common_name")
		assert.Contains(t, tool.InputSchema.Properties, "common_name")
		assert.Contains(t, tool.InputSchema.Properties, "organization")
		assert.Contains(t, tool.InputSchema.Properties, "dns_names")
		assert.Contains(t, tool.InputSchema.Properties, "usage")
		assert.Contains(t, tool.InputSchema.Properties, "key_algorithm")
	})

	t.Run("sign csr tool schema", func(t *testing.T) {
		tool := CertificateSignCSRTool
		assert.Equal(t, "quicbaas_certificate_sign_csr", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type)
		assert.Contains(t, tool.InputSchema.Required, "csr_pem")
		assert.Contains(t, tool.InputSchema.Properties, "csr_pem")
		assert.Contains(t, tool.InputSchema.Properties, "validity_days")
		assert.Contains(t, tool.InputSchema.Properties, "usage")
	})

	t.Run("revoke tool schema", func(t *testing.T) {
		tool := CertificateRevokeTool
		assert.Equal(t, "quicbaas_certificate_revoke", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type)
		assert.Contains(t, tool.InputSchema.Required, "serial_number")
		assert.Contains(t, tool.InputSchema.Properties, "serial_number")
	})

	t.Run("verify tool schema", func(t *testing.T) {
		tool := CertificateVerifyTool
		assert.Equal(t, "quicbaas_certificate_verify", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type)
		assert.Contains(t, tool.InputSchema.Required, "certificate_pem")
		assert.Contains(t, tool.InputSchema.Properties, "certificate_pem")
	})

	t.Run("get ca tool schema", func(t *testing.T) {
		tool := CertificateGetCATool
		assert.Equal(t, "quicbaas_certificate_get_ca", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type)
		assert.Len(t, tool.InputSchema.Required, 0)
	})

	t.Run("get tls config tool schema", func(t *testing.T) {
		tool := CertificateGetTLSConfigTool
		assert.Equal(t, "quicbaas_certificate_get_tls_config", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type)
		assert.Len(t, tool.InputSchema.Required, 0)
		assert.Contains(t, tool.InputSchema.Properties, "server_name")
		assert.Contains(t, tool.InputSchema.Properties, "require_client_cert")
		assert.Contains(t, tool.InputSchema.Properties, "is_server")
		assert.Contains(t, tool.InputSchema.Properties, "min_version")
	})
}

// TestCertificateIssueJSONParsing tests JSON array parameter parsing in certificate issue.
func TestCertificateIssueJSONParsing(t *testing.T) {
	t.Run("invalid dns_names JSON is silently ignored", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		now := time.Now()

		mockCertSvc.On("IssueCertificate", mock.Anything, mock.MatchedBy(func(req *service.IssueCertificateRequest) bool {
			return req.CommonName == "test.example.com" && len(req.DNSNames) == 0
		})).Return(&service.IssueCertificateResponse{
			CertificatePEM:   "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----",
			PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----",
			CACertificatePEM: "",
			ChainPEM:         "",
			SerialNumber:     "11111",
			NotBefore:        now,
			NotAfter:         now.Add(365 * 24 * time.Hour),
		}, nil)

		args := map[string]interface{}{
			"common_name": "test.example.com",
			"dns_names":   "not-valid-json",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_issue", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		mockCertSvc.AssertExpectations(t)
	})

	t.Run("invalid ip_addresses JSON is silently ignored", func(t *testing.T) {
		log := createTestLogger(t)
		mockCertSvc := new(MockCertificateService)
		mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
		registry := NewToolRegistry(mockSvc, log)

		now := time.Now()

		mockCertSvc.On("IssueCertificate", mock.Anything, mock.MatchedBy(func(req *service.IssueCertificateRequest) bool {
			return req.CommonName == "test.example.com" && len(req.IPAddresses) == 0
		})).Return(&service.IssueCertificateResponse{
			CertificatePEM:   "-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----",
			PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----",
			CACertificatePEM: "",
			ChainPEM:         "",
			SerialNumber:     "22222",
			NotBefore:        now,
			NotAfter:         now.Add(365 * 24 * time.Hour),
		}, nil)

		args := map[string]interface{}{
			"common_name":  "test.example.com",
			"ip_addresses": "{invalid}",
		}

		result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_issue", args)
		require.NoError(t, err)
		require.NotNil(t, result)

		mockCertSvc.AssertExpectations(t)
	})
}

// TestCertificateExecuteUnknownTool tests handling of unknown certificate tool names.
func TestCertificateExecuteUnknownTool(t *testing.T) {
	log := createTestLogger(t)
	mockCertSvc := new(MockCertificateService)
	mockSvc := newTestQuicBaaSWithCertificateService(mockCertSvc)
	registry := NewToolRegistry(mockSvc, log)

	result, err := registry.ExecuteTool(context.Background(), "quicbaas_certificate_unknown", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown tool")
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
