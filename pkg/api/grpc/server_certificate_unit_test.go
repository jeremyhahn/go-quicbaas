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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// IssueCertificate Tests
// =============================================================================

func TestServer_IssueCertificate(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.IssueCertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_common_name", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.IssueCertificate(context.Background(), &pb.IssueCertificateRequest{
			CommonName: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("certificate_service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		// Certificate() returns nil by default
		mockSvc.CertificateSvc = nil
		_, err := srv.IssueCertificate(context.Background(), &pb.IssueCertificateRequest{
			CommonName: "test.example.com",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// SignCSR Tests
// =============================================================================

func TestServer_SignCSR(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.SignCSR(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_csr_pem", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.SignCSR(context.Background(), &pb.SignCSRRequest{
			CsrPem: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.SignCSR(context.Background(), &pb.SignCSRRequest{
			CsrPem: "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// RevokeCertificate Tests
// =============================================================================

func TestServer_RevokeCertificate(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RevokeCertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_serial_number", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.RevokeCertificate(context.Background(), &pb.RevokeCertificateRequest{
			SerialNumber: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.RevokeCertificate(context.Background(), &pb.RevokeCertificateRequest{
			SerialNumber: "12345",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// VerifyCertificate Tests
// =============================================================================

func TestServer_VerifyCertificate(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.VerifyCertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty_certificate_pem", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.VerifyCertificate(context.Background(), &pb.VerifyCertificateRequest{
			CertificatePem: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.VerifyCertificate(context.Background(), &pb.VerifyCertificateRequest{
			CertificatePem: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// GetCACertificate Tests
// =============================================================================

func TestServer_GetCACertificate(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GetCACertificate(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.GetCACertificate(context.Background(), &pb.GetCACertificateRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// GetTLSConfig Tests
// =============================================================================

func TestServer_GetTLSConfig(t *testing.T) {
	t.Run("nil_request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.GetTLSConfig(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service_not_available", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.GetTLSConfig(context.Background(), &pb.GetTLSConfigRequest{
			ServerName: "test.example.com",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// Certificate Conversion Helper Tests
// =============================================================================

func TestProtoCertUsageToString(t *testing.T) {
	tests := []struct {
		name     string
		usage    pb.CertificateUsage
		expected string
	}{
		{name: "client", usage: pb.CertificateUsage_CERTIFICATE_USAGE_CLIENT, expected: "client"},
		{name: "server", usage: pb.CertificateUsage_CERTIFICATE_USAGE_SERVER, expected: "server"},
		{name: "both", usage: pb.CertificateUsage_CERTIFICATE_USAGE_BOTH, expected: "both"},
		{name: "unspecified", usage: pb.CertificateUsage_CERTIFICATE_USAGE_UNSPECIFIED, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoCertUsageToString(tt.usage)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoCertKeyAlgToString(t *testing.T) {
	tests := []struct {
		name     string
		alg      pb.CertKeyAlgorithm
		expected string
	}{
		{name: "ecdsa", alg: pb.CertKeyAlgorithm_CERT_KEY_ALG_ECDSA, expected: "ecdsa"},
		{name: "rsa", alg: pb.CertKeyAlgorithm_CERT_KEY_ALG_RSA, expected: "rsa"},
		{name: "unspecified_defaults_to_ecdsa", alg: pb.CertKeyAlgorithm_CERT_KEY_ALG_UNSPECIFIED, expected: "ecdsa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoCertKeyAlgToString(tt.alg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoTLSVersionToString(t *testing.T) {
	tests := []struct {
		name     string
		version  pb.TLSVersion
		expected string
	}{
		{name: "tls_1_2", version: pb.TLSVersion_TLS_VERSION_1_2, expected: "1.2"},
		{name: "tls_1_3", version: pb.TLSVersion_TLS_VERSION_1_3, expected: "1.3"},
		{name: "unspecified_defaults_to_1_2", version: pb.TLSVersion_TLS_VERSION_UNSPECIFIED, expected: "1.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoTLSVersionToString(tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertTLSConfigToPEM(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		err := convertTLSConfigToPEM(nil)
		assert.NoError(t, err)
	})

	t.Run("nil_config", func(t *testing.T) {
		err := convertTLSConfigToPEM(&service.GetTLSConfigResponse{Config: nil})
		assert.NoError(t, err)
	})
}

// --- Merged from server_handlers_boost_test.go (certificate tests) ---
