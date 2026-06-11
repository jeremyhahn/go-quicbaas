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
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"errors"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestNewServerValidation tests NewServer parameter validation.
func TestNewServerValidation(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	t.Run("NilService", func(t *testing.T) {
		srv, err := NewServer(nil, log, 1, "test")
		assert.Error(t, err)
		assert.Nil(t, srv)
		assert.Contains(t, err.Error(), "service")
	})

	t.Run("NilLogger", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		srv, err := NewServer(mockSvc, nil, 1, "test")
		assert.Error(t, err)
		assert.Nil(t, srv)
		assert.Contains(t, err.Error(), "logger")
	})
}

// TestPutRequestValidation tests Put request validation.
func TestPutRequestValidation(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.PutRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "NilRequest",
			req:         nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "EmptyEntityType",
			req: &pb.PutRequest{
				EntityType: "",
				Id:         1,
				Data:       []byte("test"),
			},
			expectError: true,
			errorMsg:    "entity_type",
		},
		{
			name: "EmptyData",
			req: &pb.PutRequest{
				EntityType: "user",
				Id:         1,
				Data:       []byte{},
			},
			expectError: false, // Empty data is valid - key-value stores allow empty values
		},
		{
			name: "ValidRequest",
			req: &pb.PutRequest{
				EntityType: "user",
				Id:         1,
				Data:       []byte("test"),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePutRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Merged from validation_extended_test.go ---

func TestValidateScanRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.ScanRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.ScanRequest{
				EntityType: "",
				IndexName:  "email",
			},
			expectError: true,
		},
		{
			name: "empty index name",
			req: &pb.ScanRequest{
				EntityType: "user",
				IndexName:  "",
			},
			expectError: true,
		},
		{
			name: "page size too large",
			req: &pb.ScanRequest{
				EntityType: "user",
				IndexName:  "email",
				PageSize:   20000,
			},
			expectError: true,
		},
		{
			name: "valid request",
			req: &pb.ScanRequest{
				EntityType: "user",
				IndexName:  "email",
				PageSize:   100,
			},
			expectError: false,
		},
		{
			name: "valid request with zero page size",
			req: &pb.ScanRequest{
				EntityType: "user",
				IndexName:  "email",
				PageSize:   0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScanRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTimeSeriesQueryRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.TimeSeriesQueryRequest
		expectError bool
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
		},
		{
			name: "empty entity type",
			req: &pb.TimeSeriesQueryRequest{
				EntityType: "",
				StartTime:  1000,
				EndTime:    2000,
			},
			expectError: true,
		},
		{
			name: "invalid time range - equal",
			req: &pb.TimeSeriesQueryRequest{
				EntityType: "sensor",
				StartTime:  2000,
				EndTime:    2000,
			},
			expectError: true,
		},
		{
			name: "invalid time range - reversed",
			req: &pb.TimeSeriesQueryRequest{
				EntityType: "sensor",
				StartTime:  2000,
				EndTime:    1000,
			},
			expectError: true,
		},
		{
			name: "page size too large",
			req: &pb.TimeSeriesQueryRequest{
				EntityType: "sensor",
				StartTime:  1000,
				EndTime:    2000,
				PageSize:   20000,
			},
			expectError: true,
		},
		{
			name: "valid request",
			req: &pb.TimeSeriesQueryRequest{
				EntityType: "sensor",
				StartTime:  1000,
				EndTime:    2000,
				PageSize:   100,
			},
			expectError: false,
		},
		{
			name: "valid request with zero page size",
			req: &pb.TimeSeriesQueryRequest{
				EntityType: "sensor",
				StartTime:  1000,
				EndTime:    2000,
				PageSize:   0,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTimeSeriesQueryRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Merged from server_additional_test.go ---

func TestNewServer_Success_WithMockService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()
	srv, err := NewServer(mockSvc, log, 1, "v")
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestStreamRecoveryInterceptor_PanicHandled(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	interceptor := StreamRecoveryInterceptor(log)
	handler := func(s interface{}, ss grpc.ServerStream) error { panic("boom") }
	err := interceptor(nil, &dummyStream{ctx: context.Background()}, &grpc.StreamServerInfo{FullMethod: "/panic"}, handler)
	_, ok := status.FromError(err)
	assert.True(t, ok)
}

// fakeScanServer implements pb.QuicBaaS_ScanIndexServer for testing.
type fakeScanServer struct {
	ctx  context.Context
	sent []*pb.ScanResponse
}

func (f *fakeScanServer) Send(r *pb.ScanResponse) error {
	f.sent = append(f.sent, r)
	return nil
}

func (f *fakeScanServer) SetHeader(metadata.MD) error  { return nil }
func (f *fakeScanServer) SendHeader(metadata.MD) error { return nil }
func (f *fakeScanServer) SetTrailer(metadata.MD)       {}
func (f *fakeScanServer) Context() context.Context     { return f.ctx }
func (f *fakeScanServer) SendMsg(any) error            { return nil }
func (f *fakeScanServer) RecvMsg(any) error            { return io.EOF }

func TestScanIndex_RangeParams(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := testutil.NewMockQuicBaaS()
	s, err := NewServer(mockSvc, log, 1, "v")
	require.NoError(t, err)

	// For this test, we're just ensuring ScanIndex handles basic request validation
	// Mock the service to return some scan responses and close the channel
	mockSvc.On("ScanIndex", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			// Get the channel and close it to signal completion
			ch := args.Get(2).(chan<- *service.ScanResponse)
			close(ch)
		}).
		Return(nil).Once()

	err = s.ScanIndex(&pb.ScanRequest{EntityType: "user", IndexName: "Name"}, &fakeScanServer{ctx: context.Background()})
	// The error might vary depending on implementation, but we're just checking it doesn't panic
	_ = err
	mockSvc.AssertExpectations(t)
}

// --- Merged from server_handlers_boost_test.go ---

// =============================================================================
// Mock Certificate Service
// =============================================================================

type mockCertificateService struct {
	mock.Mock
}

func (m *mockCertificateService) IssueCertificate(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.IssueCertificateResponse), args.Error(1)
}

func (m *mockCertificateService) SignCSR(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SignCSRResponse), args.Error(1)
}

func (m *mockCertificateService) RevokeCertificate(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RevokeCertificateResponse), args.Error(1)
}

func (m *mockCertificateService) VerifyCertificate(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.VerifyCertificateResponse), args.Error(1)
}

func (m *mockCertificateService) GetCACertificate(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetCACertificateResponse), args.Error(1)
}

func (m *mockCertificateService) GetTLSConfig(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetTLSConfigResponse), args.Error(1)
}

func (m *mockCertificateService) IsInitialized() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockCertificateService) CAService() xkmstransport.Client {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(xkmstransport.Client)
}

func (m *mockCertificateService) GetCABundle(ctx context.Context, req *service.GetCABundleRequest) (*service.GetCABundleResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetCABundleResponse), args.Error(1)
}

func (m *mockCertificateService) GenerateCRL(ctx context.Context, req *service.GenerateCRLRequest) (*service.GenerateCRLResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateCRLResponse), args.Error(1)
}

func (m *mockCertificateService) IsRevoked(ctx context.Context, req *service.IsRevokedRequest) (*service.IsRevokedResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.IsRevokedResponse), args.Error(1)
}

func (m *mockCertificateService) CAInfo(ctx context.Context, req *service.CAInfoRequest) (*service.CAInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CAInfoResponse), args.Error(1)
}

func (m *mockCertificateService) ListCertificates(ctx context.Context, req *service.ListCertificatesRequest) (*service.ListCertificatesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListCertificatesResponse), args.Error(1)
}

// =============================================================================
// Certificate Service Success Tests
// =============================================================================

func TestServer_IssueCertificate_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	now := time.Now()
	mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).Return(&service.IssueCertificateResponse{
		CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
		PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----",
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		ChainPEM:         "chain",
		SerialNumber:     "1234567890",
		NotBefore:        now,
		NotAfter:         now.Add(365 * 24 * time.Hour),
	}, nil).Once()

	resp, err := srv.IssueCertificate(context.Background(), &pb.IssueCertificateRequest{
		CommonName:   "test.example.com",
		Organization: "Test Org",
		ValidityDays: 365,
		Usage:        pb.CertificateUsage_CERTIFICATE_USAGE_SERVER,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----", resp.CertificatePem)
	assert.Equal(t, "1234567890", resp.SerialNumber)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_SignCSR_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	now := time.Now()
	mockCertSvc.On("SignCSR", mock.Anything, mock.Anything).Return(&service.SignCSRResponse{
		CertificatePEM:   "-----BEGIN CERTIFICATE-----\nsigned\n-----END CERTIFICATE-----",
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		ChainPEM:         "chain",
		SerialNumber:     "9876543210",
		NotBefore:        now,
		NotAfter:         now.Add(365 * 24 * time.Hour),
	}, nil).Once()

	resp, err := srv.SignCSR(context.Background(), &pb.SignCSRRequest{
		CsrPem:       "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
		ValidityDays: 365,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "9876543210", resp.SerialNumber)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_RevokeCertificate_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("RevokeCertificate", mock.Anything, mock.Anything).Return(&service.RevokeCertificateResponse{
		Success: true,
	}, nil).Once()

	resp, err := srv.RevokeCertificate(context.Background(), &pb.RevokeCertificateRequest{
		SerialNumber: "12345",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_VerifyCertificate_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).Return(&service.VerifyCertificateResponse{
		Valid:        true,
		ErrorMessage: "",
	}, nil).Once()

	resp, err := srv.VerifyCertificate(context.Background(), &pb.VerifyCertificateRequest{
		CertificatePem: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Valid)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_GetCACertificate_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).Return(&service.GetCACertificateResponse{
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
		CABundlePEM:      "bundle",
		Identity:         "test-ca",
	}, nil).Once()

	resp, err := srv.GetCACertificate(context.Background(), &pb.GetCACertificateRequest{})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.CaCertificatePem)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_GetTLSConfig_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).Return(&service.GetTLSConfigResponse{
		Config: &tls.Config{
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13,
		},
	}, nil).Once()

	resp, err := srv.GetTLSConfig(context.Background(), &pb.GetTLSConfigRequest{
		ServerName: "test.example.com",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	mockCertSvc.AssertExpectations(t)
}

// =============================================================================
// Dynamic Secrets Service Tests
// =============================================================================

func TestServer_CreateDynamicSecretsRole_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.CreateDynamicSecretsRole(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_CreateDynamicSecretsRole_EmptyName(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
		Name: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_CreateDynamicSecretsRole_EmptyGeneratorType(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_CreateDynamicSecretsRole_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.CreateDynamicSecretsRole(context.Background(), &pb.DynSecretsCreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "postgres",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_GetDynamicSecretsRole_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.GetDynamicSecretsRole(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_GetDynamicSecretsRole_EmptyName(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.GetDynamicSecretsRole(context.Background(), &pb.DynSecretsGetRoleRequest{
		Name: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_GetDynamicSecretsRole_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.GetDynamicSecretsRole(context.Background(), &pb.DynSecretsGetRoleRequest{
		Name: "test-role",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_ListDynamicSecretsRoles_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.ListDynamicSecretsRoles(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_ListDynamicSecretsRoles_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.ListDynamicSecretsRoles(context.Background(), &pb.DynSecretsListRolesRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_DeleteDynamicSecretsRole_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.DeleteDynamicSecretsRole(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_DeleteDynamicSecretsRole_EmptyName(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.DeleteDynamicSecretsRole(context.Background(), &pb.DynSecretsDeleteRoleRequest{
		Name: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_DeleteDynamicSecretsRole_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.DeleteDynamicSecretsRole(context.Background(), &pb.DynSecretsDeleteRoleRequest{
		Name: "test-role",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_GenerateCredential_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.GenerateCredential(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_GenerateCredential_EmptyRoleName(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.GenerateCredential(context.Background(), &pb.GenerateCredentialRequest{
		RoleName: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_GenerateCredential_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.GenerateCredential(context.Background(), &pb.GenerateCredentialRequest{
		RoleName: "test-role",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_RenewLease_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.RenewLease(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_RenewLease_EmptyLeaseID(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.RenewLease(context.Background(), &pb.RenewLeaseRequest{
		LeaseId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_RenewLease_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.RenewLease(context.Background(), &pb.RenewLeaseRequest{
		LeaseId:   "lease-123",
		Increment: durationpb.New(time.Hour),
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_RevokeLease_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.RevokeLease(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_RevokeLease_EmptyLeaseID(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.RevokeLease(context.Background(), &pb.RevokeLeaseRequest{
		LeaseId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_RevokeLease_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.RevokeLease(context.Background(), &pb.RevokeLeaseRequest{
		LeaseId: "lease-123",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_ListLeases_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.ListLeases(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_ListLeases_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.ListLeases(context.Background(), &pb.ListLeasesRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

// =============================================================================
// Embedding Service Tests
// =============================================================================

func TestServer_EmbeddingGenerate_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingGenerate(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_EmbeddingGenerate_EmptyText(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{
		Text: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_EmbeddingGenerate_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingGenerate(context.Background(), &pb.EmbeddingGenerateRequest{
		Text: "hello world",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_EmbeddingBatchGenerate_NilRequest(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingBatchGenerate(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_EmbeddingBatchGenerate_EmptyItems(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
		Items: nil,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_EmbeddingBatchGenerate_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
		Items: []*pb.EmbeddingBatchItem{{Id: "1", Text: "hello"}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_EmbeddingListProviders_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingListProviders(context.Background(), &pb.EmbeddingListProvidersRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_EmbeddingGetProviderInfo_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingGetProviderInfo(context.Background(), &pb.EmbeddingGetProviderInfoRequest{
		Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_EmbeddingSetDefaultProvider_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingSetDefaultProvider(context.Background(), &pb.EmbeddingSetDefaultProviderRequest{
		Provider: pb.EmbeddingProvider_EMBEDDING_PROVIDER_OPENAI,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_EmbeddingGetCacheStats_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingGetCacheStats(context.Background(), &pb.EmbeddingGetCacheStatsRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestServer_EmbeddingClearCache_ServiceNotAvailable(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	_, err := srv.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

// =============================================================================
// Certificate Service Error Handling Tests
// =============================================================================

func TestServer_IssueCertificate_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("IssueCertificate", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.IssueCertificate(context.Background(), &pb.IssueCertificateRequest{
		CommonName: "test.example.com",
	})

	require.Error(t, err)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_SignCSR_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("SignCSR", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.SignCSR(context.Background(), &pb.SignCSRRequest{
		CsrPem: "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
	})

	require.Error(t, err)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_RevokeCertificate_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("RevokeCertificate", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.RevokeCertificate(context.Background(), &pb.RevokeCertificateRequest{
		SerialNumber: "12345",
	})

	require.Error(t, err)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_VerifyCertificate_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("VerifyCertificate", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.VerifyCertificate(context.Background(), &pb.VerifyCertificateRequest{
		CertificatePem: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
	})

	require.Error(t, err)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_GetCACertificate_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("GetCACertificate", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.GetCACertificate(context.Background(), &pb.GetCACertificateRequest{})

	require.Error(t, err)
	mockCertSvc.AssertExpectations(t)
}

func TestServer_GetTLSConfig_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockCertSvc := &mockCertificateService{}
	mockSvc.CertificateSvc = mockCertSvc

	mockCertSvc.On("GetTLSConfig", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.GetTLSConfig(context.Background(), &pb.GetTLSConfigRequest{
		ServerName: "test.example.com",
	})

	require.Error(t, err)
	mockCertSvc.AssertExpectations(t)
}

// --- Merged from server_streaming_coverage_test.go ---

// =============================================================================
// StreamGetObject tests
// =============================================================================

func TestStreamGetObject_NilRequest(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamGetObjectServer{ctx: context.Background()}

	err := srv.StreamGetObject(nil, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStreamGetObject_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}
	stream := &mockStreamGetObjectServer{ctx: context.Background()}

	err := srv.StreamGetObject(&pb.GetObjectRequest{Key: "test"}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestStreamGetObject_EmptyKey(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamGetObjectServer{ctx: context.Background()}

	err := srv.StreamGetObject(&pb.GetObjectRequest{Key: ""}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStreamGetObject_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, _ *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return nil, errors.New("storage backend down")
		},
	}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamGetObjectServer{ctx: context.Background()}

	err := srv.StreamGetObject(&pb.GetObjectRequest{Key: "test-key"}, stream)
	require.Error(t, err)
}

func TestStreamGetObject_Success_SmallObject(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, _ *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{
				Key:         "test-key",
				Value:       []byte("small data"),
				ContentType: "text/plain",
				Size:        10,
			}, nil
		},
	}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamGetObjectServer{ctx: context.Background()}

	err := srv.StreamGetObject(&pb.GetObjectRequest{Key: "test-key", IncludeMetadata: true}, stream)
	require.NoError(t, err)
	assert.Len(t, stream.chunks, 1)
	assert.True(t, stream.chunks[0].IsLast)
	assert.Equal(t, []byte("small data"), stream.chunks[0].Data)
}

func TestStreamGetObject_Success_LargeObject(t *testing.T) {
	// Create data larger than 64KB to force multiple chunks
	largeData := make([]byte, 128*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	mockSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, _ *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{
				Key:   "large-key",
				Value: largeData,
				Size:  int64(len(largeData)),
			}, nil
		},
	}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamGetObjectServer{ctx: context.Background()}

	err := srv.StreamGetObject(&pb.GetObjectRequest{Key: "large-key"}, stream)
	require.NoError(t, err)
	assert.Equal(t, 2, len(stream.chunks))
	assert.False(t, stream.chunks[0].IsLast)
	assert.True(t, stream.chunks[1].IsLast)
}

func TestStreamGetObject_SendError(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		getObjectFunc: func(_ context.Context, _ *service.GetObjectRequest) (*service.GetObjectResponse, error) {
			return &service.GetObjectResponse{
				Key:   "test-key",
				Value: []byte("data"),
				Size:  4,
			}, nil
		},
	}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamGetObjectServer{
		ctx:       context.Background(),
		sendError: errors.New("connection reset"),
	}

	err := srv.StreamGetObject(&pb.GetObjectRequest{Key: "test-key"}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.Unknown, status.Code(err))
}

// =============================================================================
// StreamPutObject tests
// =============================================================================

func TestStreamPutObject_NilService(t *testing.T) {
	srv := &objectStorageServer{service: nil}
	stream := &mockStreamPutObjectServer{ctx: context.Background()}

	err := srv.StreamPutObject(stream)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestStreamPutObject_RecvError(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamPutObjectServer{
		ctx:       context.Background(),
		recvError: errors.New("connection broken"),
	}

	err := srv.StreamPutObject(stream)
	require.Error(t, err)
	assert.Equal(t, codes.Unknown, status.Code(err))
}

func TestStreamPutObject_NoMetadata(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	// Send data chunks but no metadata
	stream := &mockStreamPutObjectServer{
		ctx: context.Background(),
		chunks: []*pb.PutObjectChunk{
			{Payload: &pb.PutObjectChunk_Data{Data: []byte("data")}},
		},
	}

	err := srv.StreamPutObject(stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStreamPutObject_EmptyKey(t *testing.T) {
	mockSvc := &mockObjectStorageService{}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamPutObjectServer{
		ctx: context.Background(),
		chunks: []*pb.PutObjectChunk{
			{Payload: &pb.PutObjectChunk_Metadata{Metadata: &pb.PutObjectRequest{Key: ""}}},
		},
	}

	err := srv.StreamPutObject(stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStreamPutObject_Success(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		putObjectFunc: func(_ context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return &service.PutObjectResponse{
				Key:       req.Key,
				ObjectID:  42,
				Size:      int64(len(req.Value)),
				Backend:   "local",
				Version:   1,
				CreatedAt: time.Now().UnixNano(),
			}, nil
		},
	}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamPutObjectServer{
		ctx: context.Background(),
		chunks: []*pb.PutObjectChunk{
			{Payload: &pb.PutObjectChunk_Metadata{Metadata: &pb.PutObjectRequest{Key: "my-key", ContentType: "text/plain"}}},
			{Payload: &pb.PutObjectChunk_Data{Data: []byte("chunk1")}},
			{Payload: &pb.PutObjectChunk_Data{Data: []byte("chunk2")}},
		},
	}

	err := srv.StreamPutObject(stream)
	require.NoError(t, err)
	assert.NotNil(t, stream.response)
	assert.Equal(t, "my-key", stream.response.Key)
	assert.Equal(t, uint64(42), stream.response.ObjectId)
}

func TestStreamPutObject_ServiceError(t *testing.T) {
	mockSvc := &mockObjectStorageService{
		putObjectFunc: func(_ context.Context, _ *service.PutObjectRequest) (*service.PutObjectResponse, error) {
			return nil, errors.New("disk full")
		},
	}
	srv := NewObjectStorageServer(mockSvc).(*objectStorageServer)
	stream := &mockStreamPutObjectServer{
		ctx: context.Background(),
		chunks: []*pb.PutObjectChunk{
			{Payload: &pb.PutObjectChunk_Metadata{Metadata: &pb.PutObjectRequest{Key: "my-key"}}},
			{Payload: &pb.PutObjectChunk_Data{Data: []byte("data")}},
		},
	}

	err := srv.StreamPutObject(stream)
	require.Error(t, err)
}

// =============================================================================
// StreamFetchObjects tests
// =============================================================================

// mockStreamFetchObjectsServer implements pb.ObjectTransferService_StreamFetchObjectsServer.
type mockStreamFetchObjectsServer struct {
	ctx       context.Context
	responses []*pb.FetchObjectResponse
	sendError error
}

func (m *mockStreamFetchObjectsServer) Send(resp *pb.FetchObjectResponse) error {
	if m.sendError != nil {
		return m.sendError
	}
	m.responses = append(m.responses, resp)
	return nil
}
func (m *mockStreamFetchObjectsServer) SetHeader(md metadata.MD) error  { return nil }
func (m *mockStreamFetchObjectsServer) SendHeader(md metadata.MD) error { return nil }
func (m *mockStreamFetchObjectsServer) SetTrailer(md metadata.MD)       {}
func (m *mockStreamFetchObjectsServer) Context() context.Context        { return m.ctx }
func (m *mockStreamFetchObjectsServer) SendMsg(msg interface{}) error   { return nil }
func (m *mockStreamFetchObjectsServer) RecvMsg(msg interface{}) error   { return nil }

func TestStreamFetchObjects_NilRequest(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)
	stream := &mockStreamFetchObjectsServer{ctx: context.Background()}

	err := srv.StreamFetchObjects(nil, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestStreamFetchObjects_ShardNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)
	stream := &mockStreamFetchObjectsServer{ctx: context.Background()}

	err := srv.StreamFetchObjects(&pb.StreamFetchObjectsRequest{
		ShardId: 99,
		Items:   []*pb.ObjectFetchItem{{ObjectRef: "obj-1"}},
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestStreamFetchObjects_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	// Register a store
	srv.RegisterStore(1, ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("object-data-" + ref), nil
	}))

	stream := &mockStreamFetchObjectsServer{ctx: context.Background()}

	err := srv.StreamFetchObjects(&pb.StreamFetchObjectsRequest{
		ShardId: 1,
		Items: []*pb.ObjectFetchItem{
			{ObjectRef: "obj-1"},
			{ObjectRef: "obj-2"},
		},
	}, stream)
	require.NoError(t, err)
	assert.Len(t, stream.responses, 2)
	assert.True(t, stream.responses[0].Found)
	assert.True(t, stream.responses[1].Found)
}

func TestStreamFetchObjects_SendError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	srv.RegisterStore(1, ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("data"), nil
	}))

	stream := &mockStreamFetchObjectsServer{
		ctx:       context.Background(),
		sendError: errors.New("broken pipe"),
	}

	err := srv.StreamFetchObjects(&pb.StreamFetchObjectsRequest{
		ShardId: 1,
		Items:   []*pb.ObjectFetchItem{{ObjectRef: "obj-1"}},
	}, stream)
	require.Error(t, err)
}

func TestStreamFetchObjects_ContextCanceled(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	srv.RegisterStore(1, ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("data"), nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	stream := &mockStreamFetchObjectsServer{ctx: ctx}

	err := srv.StreamFetchObjects(&pb.StreamFetchObjectsRequest{
		ShardId: 1,
		Items:   []*pb.ObjectFetchItem{{ObjectRef: "obj-1"}, {ObjectRef: "obj-2"}},
	}, stream)
	// Context is canceled, should return error
	require.Error(t, err)
}

// =============================================================================
// WatchSecretsByPrefix tests
// =============================================================================

// mockWatchSecretsStream implements pb.SecretsManagement_WatchSecretsByPrefixServer.
type mockWatchSecretsStream struct {
	ctx       context.Context
	sent      []*pb.SecretChange
	sendError error
}

func (m *mockWatchSecretsStream) Send(change *pb.SecretChange) error {
	if m.sendError != nil {
		return m.sendError
	}
	m.sent = append(m.sent, change)
	return nil
}
func (m *mockWatchSecretsStream) SetHeader(md metadata.MD) error  { return nil }
func (m *mockWatchSecretsStream) SendHeader(md metadata.MD) error { return nil }
func (m *mockWatchSecretsStream) SetTrailer(md metadata.MD)       {}
func (m *mockWatchSecretsStream) Context() context.Context        { return m.ctx }
func (m *mockWatchSecretsStream) SendMsg(msg interface{}) error   { return nil }
func (m *mockWatchSecretsStream) RecvMsg(msg interface{}) error   { return nil }

func TestWatchSecretsByPrefix_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})

	svc := &mockSecretsService{
		watchSecretsByPrefixFunc: func(_ context.Context, _ *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return nil, errors.New("watch not supported")
		},
	}

	srv := NewSecretsServer(svc, log)
	stream := &mockWatchSecretsStream{ctx: context.Background()}

	err := srv.WatchSecretsByPrefix(&pb.WatchSecretsRequest{Prefix: "test/"}, stream)
	require.Error(t, err)
}

func TestWatchSecretsByPrefix_ChannelClosed(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})

	ch := make(chan *service.SecretChange, 1)
	close(ch) // Close immediately

	svc := &mockSecretsService{
		watchSecretsByPrefixFunc: func(_ context.Context, _ *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return ch, nil
		},
	}

	srv := NewSecretsServer(svc, log)
	stream := &mockWatchSecretsStream{ctx: context.Background()}

	err := srv.WatchSecretsByPrefix(&pb.WatchSecretsRequest{Prefix: "test/"}, stream)
	require.NoError(t, err) // Channel closing returns nil
}

func TestWatchSecretsByPrefix_ContextCanceled(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})

	ch := make(chan *service.SecretChange)
	ctx, cancel := context.WithCancel(context.Background())

	svc := &mockSecretsService{
		watchSecretsByPrefixFunc: func(_ context.Context, _ *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return ch, nil
		},
	}

	srv := NewSecretsServer(svc, log)
	stream := &mockWatchSecretsStream{ctx: ctx}

	// Cancel context after short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := srv.WatchSecretsByPrefix(&pb.WatchSecretsRequest{Prefix: "test/"}, stream)
	// Context canceled
	require.Error(t, err)
}

func TestWatchSecretsByPrefix_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})

	ch := make(chan *service.SecretChange, 3)
	ch <- &service.SecretChange{
		Path:       "test/secret1",
		ChangeType: service.SecretChangeCreated,
		Data:       map[string][]byte{"value": []byte("data1")},
		Version:    1,
		Timestamp:  time.Now(),
	}
	ch <- &service.SecretChange{
		Path:       "test/secret2",
		ChangeType: service.SecretChangeUpdated,
		Data:       map[string][]byte{"value": []byte("data2")},
		Version:    2,
		Timestamp:  time.Now(),
	}
	ch <- &service.SecretChange{
		Path:       "test/secret3",
		ChangeType: service.SecretChangeDeleted,
		Version:    3,
		Timestamp:  time.Now(),
	}
	close(ch) // Close after sending

	svc := &mockSecretsService{
		watchSecretsByPrefixFunc: func(_ context.Context, _ *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return ch, nil
		},
	}

	srv := NewSecretsServer(svc, log)
	stream := &mockWatchSecretsStream{ctx: context.Background()}

	err := srv.WatchSecretsByPrefix(&pb.WatchSecretsRequest{
		Prefix:       "test/",
		StartVersion: 0,
	}, stream)
	require.NoError(t, err)
	assert.Len(t, stream.sent, 3)
	assert.Equal(t, pb.SecretChangeType_SECRET_CHANGE_TYPE_CREATED, stream.sent[0].ChangeType)
	assert.Equal(t, pb.SecretChangeType_SECRET_CHANGE_TYPE_UPDATED, stream.sent[1].ChangeType)
	assert.Equal(t, pb.SecretChangeType_SECRET_CHANGE_TYPE_DELETED, stream.sent[2].ChangeType)
}

func TestWatchSecretsByPrefix_SendError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})

	ch := make(chan *service.SecretChange, 1)
	ch <- &service.SecretChange{
		Path:       "test/secret1",
		ChangeType: service.SecretChangeCreated,
		Data:       map[string][]byte{"value": []byte("data1")},
		Version:    1,
		Timestamp:  time.Now(),
	}

	svc := &mockSecretsService{
		watchSecretsByPrefixFunc: func(_ context.Context, _ *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return ch, nil
		},
	}

	srv := NewSecretsServer(svc, log)
	stream := &mockWatchSecretsStream{
		ctx:       context.Background(),
		sendError: errors.New("send failed"),
	}

	err := srv.WatchSecretsByPrefix(&pb.WatchSecretsRequest{Prefix: "test/"}, stream)
	require.Error(t, err)
}

// =============================================================================
// StreamBatchPut main function coverage
// =============================================================================

// --- Merged from coverage_91_target_test.go ---

// =============================================================================
// Streaming validation error paths in server.go
// These cover the validation-error branches inside the actual List, Query,
// ScanIndex, and TimeSeriesQuery streaming RPC methods (lines 455-459,
// 675-679, 754-758, 972-976).
// =============================================================================

func TestServer_List_ValidationError_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockListStreamHandler(context.Background())

	err := srv.List(nil, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_List_ValidationError_EmptyEntityType(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockListStreamHandler(context.Background())

	err := srv.List(&pb.ListRequest{EntityType: "", PageSize: 10}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_List_ValidationError_ExcessivePageSize(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockListStreamHandler(context.Background())

	err := srv.List(&pb.ListRequest{EntityType: "users", PageSize: 20000}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_Query_ValidationError_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockQueryStreamHandler(context.Background())

	err := srv.Query(nil, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_Query_ValidationError_EmptyEntityType(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockQueryStreamHandler(context.Background())

	err := srv.Query(&pb.QueryRequest{
		EntityType: "",
		IndexName:  "idx",
		Value:      []byte("v"),
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_Query_ValidationError_EmptyIndexName(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockQueryStreamHandler(context.Background())

	err := srv.Query(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "",
		Value:      []byte("v"),
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_Query_ValidationError_NoValueOrRange(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockQueryStreamHandler(context.Background())

	err := srv.Query(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "idx",
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_ScanIndex_ValidationError_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockScanStreamHandler(context.Background())

	err := srv.ScanIndex(nil, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_ScanIndex_ValidationError_EmptyEntityType(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockScanStreamHandler(context.Background())

	err := srv.ScanIndex(&pb.ScanRequest{
		EntityType: "",
		IndexName:  "idx",
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_ScanIndex_ValidationError_EmptyIndexName(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockScanStreamHandler(context.Background())

	err := srv.ScanIndex(&pb.ScanRequest{
		EntityType: "users",
		IndexName:  "",
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_TimeSeriesQuery_ValidationError_NilRequest(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockTimeSeriesStreamHandler(context.Background())

	err := srv.TimeSeriesQuery(nil, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_TimeSeriesQuery_ValidationError_EmptyEntityType(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockTimeSeriesStreamHandler(context.Background())

	err := srv.TimeSeriesQuery(&pb.TimeSeriesQueryRequest{
		EntityType: "",
		StartTime:  1,
		EndTime:    2,
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestServer_TimeSeriesQuery_ValidationError_InvalidTimeRange(t *testing.T) {
	srv, _ := newMockTestServer(t)
	stream := NewMockTimeSeriesStreamHandler(context.Background())

	err := srv.TimeSeriesQuery(&pb.TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  100,
		EndTime:    50,
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// Embedding int32 overflow clamping paths
// =============================================================================

func TestServer_EmbeddingBatchGenerate_Int32Overflow(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	now := time.Now()

	mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
		BatchGenerateEmbeddingsFunc: func(_ context.Context, _ *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
			return &service.BatchGenerateEmbeddingsResponse{
				Results:        []*service.BatchEmbeddingResult{},
				SuccessCount:   0x100000000,
				ErrorCount:     0x100000000,
				TotalTokens:    0x100000000,
				ProcessingTime: time.Millisecond,
				Timestamp:      now,
			}, nil
		},
	}

	resp, err := srv.EmbeddingBatchGenerate(context.Background(), &pb.EmbeddingBatchGenerateRequest{
		Items: []*pb.EmbeddingBatchItem{{Id: "1", Text: "test"}},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0x7FFFFFFF), resp.SuccessCount)
	assert.Equal(t, int32(0x7FFFFFFF), resp.ErrorCount)
	assert.Equal(t, int32(0x7FFFFFFF), resp.TotalTokens)
}

func TestServer_EmbeddingGetCacheStats_Int32Overflow(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	now := time.Now()

	mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
		GetCacheStatsFunc: func(_ context.Context, _ *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
			return &service.GetEmbeddingCacheStatsResponse{
				Enabled:   true,
				Size:      0x100000000,
				Capacity:  0x100000000,
				Hits:      100,
				Misses:    10,
				HitRate:   0.91,
				Timestamp: now,
			}, nil
		},
	}

	resp, err := srv.EmbeddingGetCacheStats(context.Background(), &pb.EmbeddingGetCacheStatsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(0x7FFFFFFF), resp.Size)
	assert.Equal(t, int32(0x7FFFFFFF), resp.Capacity)
}

func TestServer_EmbeddingClearCache_Int32Overflow(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	now := time.Now()

	mockSvc.EmbeddingService = &mocks.MockEmbeddingService{
		ClearCacheFunc: func(_ context.Context, _ *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
			return &service.ClearEmbeddingCacheResponse{
				EntriesCleared: 0x100000000,
				Timestamp:      now,
			}, nil
		},
	}

	resp, err := srv.EmbeddingClearCache(context.Background(), &pb.EmbeddingClearCacheRequest{All: true})
	require.NoError(t, err)
	assert.Equal(t, int32(0x7FFFFFFF), resp.EntriesCleared)
}

// =============================================================================
// Transaction validation - invalid compare condition and failure operation
// =============================================================================

func TestValidateTransactionRequest_InvalidCompareCondition(t *testing.T) {
	req := &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "",
				Id:         1,
				Target:     pb.CompareTarget_COMPARE_TARGET_VALUE,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Value:      []byte("test"),
			},
		},
		Success: []*pb.TransactionOp{
			{
				EntityType: "users",
				Id:         1,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte("data"),
			},
		},
	}

	err := validateTransactionRequest(req)
	require.Error(t, err)
}

func TestValidateTransactionRequest_InvalidFailureOperation(t *testing.T) {
	req := &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "users",
				Id:         1,
				Target:     pb.CompareTarget_COMPARE_TARGET_VALUE,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Value:      []byte("test"),
			},
		},
		Failure: []*pb.TransactionOp{
			{
				EntityType: "",
				Id:         1,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte("data"),
			},
		},
	}

	err := validateTransactionRequest(req)
	require.Error(t, err)
}

// =============================================================================
// Validation - excessive page size variants
// =============================================================================

func TestValidateQueryRequest_ExcessivePageSize(t *testing.T) {
	err := validateQueryRequest(&pb.QueryRequest{
		EntityType: "users",
		IndexName:  "email_idx",
		Value:      []byte("test@example.com"),
		PageSize:   20000,
	})
	require.Error(t, err)
}

func TestValidateScanRequest_ExcessivePageSize(t *testing.T) {
	err := validateScanRequest(&pb.ScanRequest{
		EntityType: "users",
		IndexName:  "name_idx",
		PageSize:   20000,
	})
	require.Error(t, err)
}

func TestValidateTimeSeriesQueryRequest_ExcessivePageSize(t *testing.T) {
	err := validateTimeSeriesQueryRequest(&pb.TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		PageSize:   20000,
	})
	require.Error(t, err)
}

// --- Merged from remaining_coverage_91_extended_test.go ---

// =============================================================================
// XKMS Server - Validation Tests
// =============================================================================

func TestGRPCRemaining91_XKMSServer_Health(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	resp, err := srv.Health(context.Background(), &xkmspb.HealthRequest{})
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp.Status)
	assert.NotEmpty(t, resp.Version)
}

func TestGRPCRemaining91_XKMSServer_GetBackendInfo_EmptyName(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	_, err := srv.GetBackendInfo(context.Background(), &xkmspb.GetBackendInfoRequest{Name: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_GetBackendInfo_NotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	_, err := srv.GetBackendInfo(context.Background(), &xkmspb.GetBackendInfoRequest{Name: "nonexistent-backend-xyz"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_GenerateKey_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *xkmspb.GenerateKeyRequest
	}{
		{"EmptyKeyID", &xkmspb.GenerateKeyRequest{KeyId: "", Backend: "b", KeyType: "signing"}},
		{"EmptyBackend", &xkmspb.GenerateKeyRequest{KeyId: "k", Backend: "", KeyType: "signing"}},
		{"EmptyKeyType", &xkmspb.GenerateKeyRequest{KeyId: "k", Backend: "b", KeyType: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := srv.GenerateKey(ctx, tt.req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestGRPCRemaining91_XKMSServer_GenerateKey_BackendNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	_, err := srv.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId: "test-key", Backend: "nonexistent-xyz", KeyType: "signing",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_ListKeys_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.ListKeys(context.Background(), &xkmspb.ListKeysRequest{Backend: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_ListKeys_BackendNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.ListKeys(context.Background(), &xkmspb.ListKeysRequest{Backend: "nonexistent-xyz"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_GetKey_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.GetKey(ctx, &xkmspb.GetKeyRequest{KeyId: "", Backend: "b"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.GetKey(ctx, &xkmspb.GetKeyRequest{KeyId: "k", Backend: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.GetKey(ctx, &xkmspb.GetKeyRequest{KeyId: "k", Backend: "nonexistent-xyz"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_DeleteKey_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.DeleteKey(ctx, &xkmspb.DeleteKeyRequest{KeyId: "", Backend: "b"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.DeleteKey(ctx, &xkmspb.DeleteKeyRequest{KeyId: "k", Backend: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.DeleteKey(ctx, &xkmspb.DeleteKeyRequest{KeyId: "k", Backend: "nonexistent-xyz"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_RotateKey_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.RotateKey(ctx, &xkmspb.RotateKeyRequest{KeyId: "", Backend: "b"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.RotateKey(ctx, &xkmspb.RotateKeyRequest{KeyId: "k", Backend: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.RotateKey(ctx, &xkmspb.RotateKeyRequest{KeyId: "k", Backend: "nonexistent-xyz"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_Sign_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.Sign(ctx, &xkmspb.SignRequest{KeyId: "", Backend: "b"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.Sign(ctx, &xkmspb.SignRequest{KeyId: "k", Backend: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.Sign(ctx, &xkmspb.SignRequest{KeyId: "k", Backend: "nonexistent-xyz"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_Verify_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.Verify(ctx, &xkmspb.VerifyRequest{KeyId: "", Backend: "b", Signature: []byte("sig")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.Verify(ctx, &xkmspb.VerifyRequest{KeyId: "k", Backend: "", Signature: []byte("sig")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySignature", func(t *testing.T) {
		_, err := srv.Verify(ctx, &xkmspb.VerifyRequest{KeyId: "k", Backend: "b", Signature: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.Verify(ctx, &xkmspb.VerifyRequest{KeyId: "k", Backend: "nonexistent-xyz", Signature: []byte("sig")})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_Encrypt_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.Encrypt(ctx, &xkmspb.EncryptRequest{KeyId: "", Backend: "b", Plaintext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.Encrypt(ctx, &xkmspb.EncryptRequest{KeyId: "k", Backend: "", Plaintext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyPlaintext", func(t *testing.T) {
		_, err := srv.Encrypt(ctx, &xkmspb.EncryptRequest{KeyId: "k", Backend: "b", Plaintext: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.Encrypt(ctx, &xkmspb.EncryptRequest{KeyId: "k", Backend: "nonexistent-xyz", Plaintext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_Decrypt_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.Decrypt(ctx, &xkmspb.DecryptRequest{KeyId: "", Backend: "b", Ciphertext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.Decrypt(ctx, &xkmspb.DecryptRequest{KeyId: "k", Backend: "", Ciphertext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyCiphertext", func(t *testing.T) {
		_, err := srv.Decrypt(ctx, &xkmspb.DecryptRequest{KeyId: "k", Backend: "b", Ciphertext: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.Decrypt(ctx, &xkmspb.DecryptRequest{KeyId: "k", Backend: "nonexistent-xyz", Ciphertext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_SaveCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyKeyID", func(t *testing.T) {
		_, err := srv.SaveCert(ctx, &xkmspb.SaveCertRequest{KeyId: "", CertPem: "pem"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyCertPem", func(t *testing.T) {
		_, err := srv.SaveCert(ctx, &xkmspb.SaveCertRequest{KeyId: "k", CertPem: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("InvalidPEM", func(t *testing.T) {
		// No backends available, but test PEM validation path
		_, err := srv.SaveCert(ctx, &xkmspb.SaveCertRequest{KeyId: "k", CertPem: "not-valid-pem"})
		require.Error(t, err)
		// Could be Unavailable (no backends) or InvalidArgument (invalid PEM)
	})
}

func TestGRPCRemaining91_XKMSServer_GetCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.GetCert(context.Background(), &xkmspb.GetCertRequest{KeyId: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_DeleteCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	_, err := srv.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{KeyId: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_Seal_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.Seal(ctx, &xkmspb.SealRequest{Backend: "", Data: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyData", func(t *testing.T) {
		_, err := srv.Seal(ctx, &xkmspb.SealRequest{Backend: "b", Data: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_Unseal_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.Unseal(ctx, &xkmspb.UnsealRequest{Backend: "", Ciphertext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyCiphertext", func(t *testing.T) {
		_, err := srv.Unseal(ctx, &xkmspb.UnsealRequest{Backend: "b", Ciphertext: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("BackendNotFound", func(t *testing.T) {
		_, err := srv.Unseal(ctx, &xkmspb.UnsealRequest{Backend: "nonexistent-xyz", Ciphertext: []byte("data")})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_CanSeal(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)

	t.Run("WithBackend", func(t *testing.T) {
		resp, err := srv.CanSeal(context.Background(), &xkmspb.CanSealRequest{Backend: "nonexistent"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("WithoutBackend", func(t *testing.T) {
		resp, err := srv.CanSeal(context.Background(), &xkmspb.CanSealRequest{Backend: ""})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

func TestGRPCRemaining91_XKMSServer_BarrierOperations(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("BarrierStatus", func(t *testing.T) {
		_, err := srv.BarrierStatus(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("BarrierInitialize", func(t *testing.T) {
		_, err := srv.BarrierInitialize(ctx, &xkmspb.BarrierInitializeRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("BarrierUnseal", func(t *testing.T) {
		_, err := srv.BarrierUnseal(ctx, &xkmspb.BarrierUnsealRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("BarrierSeal", func(t *testing.T) {
		_, err := srv.BarrierSeal(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// XKMS Server - Helper Function Tests
// =============================================================================

func TestGRPCRemaining91_XKMSServer_BackendDescription(t *testing.T) {
	tests := []struct {
		input    xkmssdk.BackendType
		expected string
	}{
		{xkmssdk.BackendTypeSoftware, "Software-based key storage"},
		{xkmssdk.BackendTypePKCS11, "Hardware Security Module (PKCS#11)"},
		{xkmssdk.BackendTypeTPM2, "Trusted Platform Module 2.0"},
		{xkmssdk.BackendTypeAWSKMS, "AWS Key Management Service"},
		{xkmssdk.BackendTypeGCPKMS, "Google Cloud Key Management Service"},
		{xkmssdk.BackendTypeAzureKV, "Azure Key Vault"},
		{xkmssdk.BackendTypeVault, "HashiCorp Vault"},
		{xkmssdk.BackendTypeSymmetric, "Symmetric key storage"},
		{xkmssdk.BackendType("unknown-type"), "unknown-type"},
	}
	for _, tt := range tests {
		result := xkmsBackendDescription(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestGRPCRemaining91_XKMSServer_ParseHash(t *testing.T) {
	tests := []struct {
		input string
		bits  int
	}{
		{"sha256", 256},
		{"SHA-256", 256},
		{"SHA256", 256},
		{"sha384", 384},
		{"SHA-384", 384},
		{"SHA384", 384},
		{"sha512", 512},
		{"SHA-512", 512},
		{"SHA512", 512},
		{"unknown", 256}, // default SHA256
		{"", 256},        // default SHA256
	}
	for _, tt := range tests {
		h := xkmsParseHash(tt.input)
		assert.Equal(t, tt.bits/8, h.Size(), "hash: %s", tt.input)
	}
}

func TestGRPCRemaining91_XKMSServer_AlgorithmString(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected string
	}{
		{"RSA", &xkmssdk.KeyAttributes{KeyAlgorithm: x509.RSA}, "RSA"},
		{"ECDSA", &xkmssdk.KeyAttributes{KeyAlgorithm: x509.ECDSA}, "ECDSA"},
		{"Ed25519", &xkmssdk.KeyAttributes{KeyAlgorithm: x509.Ed25519}, "Ed25519"},
		{"Symmetric", &xkmssdk.KeyAttributes{SymmetricAlgorithm: "aes256-gcm"}, "aes256-gcm"},
		{"KeyType", &xkmssdk.KeyAttributes{KeyType: xkmssdk.KeyTypeSigning}, string(xkmssdk.KeyTypeSigning)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := xkmsAlgorithmString(tt.attrs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGRPCRemaining91_XKMSServer_ExtractPublicKeyPEM(t *testing.T) {
	t.Run("UnsupportedType", func(t *testing.T) {
		result := xkmsExtractPublicKeyPEM("not-a-key")
		assert.Empty(t, result)
	})

	t.Run("NilInput", func(t *testing.T) {
		result := xkmsExtractPublicKeyPEM(nil)
		assert.Empty(t, result)
	})
}

func TestGRPCRemaining91_XKMSServer_KeyNotFoundError(t *testing.T) {
	err := &XKMSKeyNotFoundError{KeyID: "test-key-123"}
	assert.Equal(t, "xkms: key not found: test-key-123", err.Error())
}

// =============================================================================
// XKMS PIV - Validation Tests
// =============================================================================

func TestGRPCRemaining91_XKMSServer_PIV_ListPIVSlots_EmptyBackend(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	_, err := srv.ListPIVSlots(context.Background(), &xkmspb.ListPIVSlotsRequest{Backend: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_XKMSServer_PIV_GetCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.GetPIVCertificate(ctx, &xkmspb.GetPIVCertificateRequest{Backend: "", Slot: "9a", Format: "pem"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySlot", func(t *testing.T) {
		_, err := srv.GetPIVCertificate(ctx, &xkmspb.GetPIVCertificateRequest{Backend: "b", Slot: "", Format: "pem"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyFormat", func(t *testing.T) {
		_, err := srv.GetPIVCertificate(ctx, &xkmspb.GetPIVCertificateRequest{Backend: "b", Slot: "9a", Format: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_StoreCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.StorePIVCertificate(ctx, &xkmspb.StorePIVCertificateRequest{Backend: "", Slot: "9a", Format: "pem", Certificate: []byte("cert")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySlot", func(t *testing.T) {
		_, err := srv.StorePIVCertificate(ctx, &xkmspb.StorePIVCertificateRequest{Backend: "b", Slot: "", Format: "pem", Certificate: []byte("cert")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyFormat", func(t *testing.T) {
		_, err := srv.StorePIVCertificate(ctx, &xkmspb.StorePIVCertificateRequest{Backend: "b", Slot: "9a", Format: "", Certificate: []byte("cert")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyCertificate", func(t *testing.T) {
		_, err := srv.StorePIVCertificate(ctx, &xkmspb.StorePIVCertificateRequest{Backend: "b", Slot: "9a", Format: "pem", Certificate: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_DeleteCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.DeletePIVCertificate(ctx, &xkmspb.DeletePIVCertificateRequest{Backend: "", Slot: "9a"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySlot", func(t *testing.T) {
		_, err := srv.DeletePIVCertificate(ctx, &xkmspb.DeletePIVCertificateRequest{Backend: "b", Slot: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_GenerateKey_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.GeneratePIVKey(ctx, &xkmspb.GeneratePIVKeyRequest{Backend: "", Slot: "9a"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySlot", func(t *testing.T) {
		_, err := srv.GeneratePIVKey(ctx, &xkmspb.GeneratePIVKeyRequest{Backend: "b", Slot: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_ExportCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.ExportPIVCertificate(ctx, &xkmspb.GetPIVCertificateRequest{Backend: "", Slot: "9a", Format: "pem"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySlot", func(t *testing.T) {
		_, err := srv.ExportPIVCertificate(ctx, &xkmspb.GetPIVCertificateRequest{Backend: "b", Slot: "", Format: "pem"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyFormat", func(t *testing.T) {
		_, err := srv.ExportPIVCertificate(ctx, &xkmspb.GetPIVCertificateRequest{Backend: "b", Slot: "9a", Format: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_GenerateCSR_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.GeneratePIVCSR(ctx, &xkmspb.GeneratePIVCSRRequest{Backend: "", Slot: "9a"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptySlot", func(t *testing.T) {
		_, err := srv.GeneratePIVCSR(ctx, &xkmspb.GeneratePIVCSRRequest{Backend: "b", Slot: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_ImportCert_Validation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewXKMSServer(log)
	ctx := context.Background()

	t.Run("EmptyBackend", func(t *testing.T) {
		_, err := srv.ImportPIVCertificate(ctx, &xkmspb.StorePIVCertificateRequest{Backend: "", Slot: "9a", Format: "pem", Certificate: []byte("c")})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("EmptyCert", func(t *testing.T) {
		_, err := srv.ImportPIVCertificate(ctx, &xkmspb.StorePIVCertificateRequest{Backend: "b", Slot: "9a", Format: "pem", Certificate: nil})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestGRPCRemaining91_XKMSServer_PIV_ErrorMapping(t *testing.T) {
	t.Run("NilError", func(t *testing.T) {
		err := xkmsMapPIVError(nil, "test")
		assert.Nil(t, err)
	})

	t.Run("PIVNotInitialized", func(t *testing.T) {
		err := xkmsMapPIVError(xkmssdk.ServiceErrPIVNotInitialized, "test")
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})

	t.Run("PIVBackendNotFound", func(t *testing.T) {
		err := xkmsMapPIVError(xkmssdk.ServiceErrPIVBackendNotFound, "test")
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("PIVInvalidAlgorithm", func(t *testing.T) {
		err := xkmsMapPIVError(xkmssdk.ServiceErrPIVInvalidAlgorithm, "test")
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("UnknownError", func(t *testing.T) {
		err := xkmsMapPIVError(stderrors.New("unknown error"), "test")
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

// =============================================================================
// Client KMS - Nil Request Tests
// =============================================================================

func TestGRPCRemaining91_Client_KMS_NilRequests(t *testing.T) {
	client := &Client{conn: nil, connected: false, closed: false}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"KMSGenerateKey", func() error { _, err := client.KMSGenerateKey(ctx, nil); return err }},
		{"KMSImportKey", func() error { _, err := client.KMSImportKey(ctx, nil); return err }},
		{"KMSGetKey", func() error { _, err := client.KMSGetKey(ctx, nil); return err }},
		{"KMSRotateKey", func() error { _, err := client.KMSRotateKey(ctx, nil); return err }},
		{"KMSDeleteKey", func() error { _, err := client.KMSDeleteKey(ctx, nil); return err }},
		{"KMSEncrypt", func() error { _, err := client.KMSEncrypt(ctx, nil); return err }},
		{"KMSDecrypt", func() error { _, err := client.KMSDecrypt(ctx, nil); return err }},
		{"KMSGenerateDataKey", func() error { _, err := client.KMSGenerateDataKey(ctx, nil); return err }},
		{"KMSUpdateKeyStatus", func() error { _, err := client.KMSUpdateKeyStatus(ctx, nil); return err }},
		{"KMSListKeyVersions", func() error { _, err := client.KMSListKeyVersions(ctx, nil); return err }},
		{"KMSExportKey", func() error { _, err := client.KMSExportKey(ctx, nil); return err }},
		{"KMSUnwrapKey", func() error { _, err := client.KMSUnwrapKey(ctx, nil); return err }},
		{"KMSSetKeySafety", func() error { _, err := client.KMSSetKeySafety(ctx, nil); return err }},
		{"KMSClearKeySafety", func() error { _, err := client.KMSClearKeySafety(ctx, nil); return err }},
		{"KMSCreateDEK", func() error { _, err := client.KMSCreateDEK(ctx, nil); return err }},
		{"KMSGetDEK", func() error { _, err := client.KMSGetDEK(ctx, nil); return err }},
		{"KMSRotateDEK", func() error { _, err := client.KMSRotateDEK(ctx, nil); return err }},
		{"KMSDeleteDEK", func() error { _, err := client.KMSDeleteDEK(ctx, nil); return err }},
		{"KMSGetDEKStatus", func() error { _, err := client.KMSGetDEKStatus(ctx, nil); return err }},
		{"KMSSetDefaultDEK", func() error { _, err := client.KMSSetDefaultDEK(ctx, nil); return err }},
		{"KMSSetDEKSafety", func() error { _, err := client.KMSSetDEKSafety(ctx, nil); return err }},
		{"KMSClearDEKSafety", func() error { _, err := client.KMSClearDEKSafety(ctx, nil); return err }},
		{"KMSStartReencryption", func() error { _, err := client.KMSStartReencryption(ctx, nil); return err }},
		{"KMSGetReencryptionProgress", func() error { _, err := client.KMSGetReencryptionProgress(ctx, nil); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
		})
	}
}

func TestGRPCRemaining91_Client_KMS_GetSupportedAlgorithms_NilConn(t *testing.T) {
	client := &Client{conn: nil, connected: false, closed: true}
	_, err := client.KMSGetSupportedAlgorithms(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// =============================================================================
// Client KMS - Conversion Helper Tests
// =============================================================================

func TestGRPCRemaining91_ClientKMS_ConvertKeyAttributesToProto(t *testing.T) {
	t.Run("NilAttrs", func(t *testing.T) {
		_, _, _, _, err := convertKeyAttributesToProto(nil)
		require.Error(t, err)
	})

	t.Run("NoAlgorithm", func(t *testing.T) {
		attrs := &xkmssdk.KeyAttributes{}
		_, _, _, _, err := convertKeyAttributesToProto(attrs)
		require.Error(t, err)
	})

	t.Run("UnsupportedSymmetric", func(t *testing.T) {
		attrs := &xkmssdk.KeyAttributes{SymmetricAlgorithm: "unsupported-alg"}
		_, _, _, _, err := convertKeyAttributesToProto(attrs)
		require.Error(t, err)
	})

	t.Run("UnsupportedAsymmetric", func(t *testing.T) {
		attrs := &xkmssdk.KeyAttributes{KeyAlgorithm: x509.PublicKeyAlgorithm(99)}
		_, _, _, _, err := convertKeyAttributesToProto(attrs)
		require.Error(t, err)
	})
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoKeyHandle_Nil(t *testing.T) {
	result := convertProtoKeyHandle(nil)
	assert.Nil(t, result)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoKeyMetadata_Nil(t *testing.T) {
	result := convertProtoKeyMetadata(nil)
	assert.Nil(t, result)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoKeyMetadata_WithLastUsedAt(t *testing.T) {
	pm := &pb.KeyMetadata{
		KeyId:      "key-1",
		Name:       "test",
		LastUsedAt: time.Now().Unix(),
		CreatedAt:  time.Now().Unix(),
		UpdatedAt:  time.Now().Unix(),
	}
	result := convertProtoKeyMetadata(pm)
	assert.NotNil(t, result)
	assert.NotNil(t, result.LastUsedAt)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoKeyVersionInfo_Nil(t *testing.T) {
	result := convertProtoKeyVersionInfo("key-1", nil)
	assert.Nil(t, result)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoKeyVersionInfo_WithDisabledAt(t *testing.T) {
	v := &pb.KeyVersionInfo{
		Version:    2,
		Status:     pb.KeyStatus_KEY_STATUS_DISABLED,
		Algorithm:  pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
		CreatedAt:  time.Now().Unix(),
		DisabledAt: time.Now().Unix(),
	}
	result := convertProtoKeyVersionInfo("key-1", v)
	assert.NotNil(t, result)
	assert.NotNil(t, result.DisabledAt)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoDEKMetadata_Nil(t *testing.T) {
	result := convertProtoDEKMetadata(nil)
	assert.Nil(t, result)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoDEKMetadata_WithRotatedAt(t *testing.T) {
	pm := &pb.DEKMetadata{
		Path:      "/test/dek",
		RotatedAt: time.Now().Unix(),
		CreatedAt: time.Now().Unix(),
	}
	result := convertProtoDEKMetadata(pm)
	assert.NotNil(t, result)
	assert.NotNil(t, result.RotatedAt)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoReencryptionProgress_Nil(t *testing.T) {
	result := convertProtoReencryptionProgress(nil)
	assert.Nil(t, result)
}

func TestGRPCRemaining91_ClientKMS_ConvertProtoReencryptionProgress_Full(t *testing.T) {
	pm := &pb.ReencryptionProgress{
		InProgress:          true,
		TotalKeys:           100,
		ProcessedKeys:       50,
		FailedKeys:          2,
		StartedAt:           time.Now().Unix(),
		EstimatedCompletion: time.Now().Add(time.Hour).Unix(),
	}
	result := convertProtoReencryptionProgress(pm)
	assert.NotNil(t, result)
	assert.True(t, result.InProgress)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.EstimatedCompletion)
	assert.InDelta(t, 50.0, result.PercentComplete, 0.1)
}

// =============================================================================
// Client KMS - Wrapping/Export/Algorithm Conversion Tests
// =============================================================================

func TestGRPCRemaining91_ClientKMS_WrappingAlgorithmConversions(t *testing.T) {
	algorithms := []string{"AES-KW", "AES-KWP", "AES-GCM", "RSA-OAEP-SHA256", "RSA-OAEP-SHA384", "RSA-OAEP-SHA512", "ECDH-ES+A256KW", "unknown"}
	for _, alg := range algorithms {
		proto := convertServiceToProtoWrappingAlgorithm(alg)
		back := convertProtoToServiceWrappingAlgorithm(proto)
		if alg == "unknown" {
			assert.Empty(t, back)
		} else {
			assert.Equal(t, alg, back)
		}
	}
}

func TestGRPCRemaining91_ClientKMS_ExportFormatConversions(t *testing.T) {
	formats := []string{"raw", "jwk", "pem", "unknown"}
	for _, fmt := range formats {
		proto := convertServiceToProtoExportFormat(fmt)
		back := convertProtoToServiceExportFormat(proto)
		if fmt == "unknown" {
			assert.Empty(t, back)
		} else {
			assert.Equal(t, fmt, back)
		}
	}
}

func TestGRPCRemaining91_ClientKMS_KeyAlgorithmConversions(t *testing.T) {
	algorithms := []string{
		"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
		"CHACHA20-POLY1305", "XCHACHA20-POLY1305",
		"RSA-2048", "RSA-3072", "RSA-4096",
		"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
		"ED25519", "ED448", "X25519", "X448",
		"HMAC-SHA256", "HMAC-SHA384", "HMAC-SHA512",
		"unknown",
	}
	for _, alg := range algorithms {
		proto := convertServiceToProtoKeyAlgorithm(alg)
		if alg != "unknown" {
			assert.NotEqual(t, pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, proto, "algorithm: %s", alg)
		}
	}
}

func TestGRPCRemaining91_ClientKMS_ServiceKeyAlgorithmToProto(t *testing.T) {
	algos := []service.KeyAlgorithm{
		service.KeyAlgAES128GCM, service.KeyAlgAES192GCM, service.KeyAlgAES256GCM,
		service.KeyAlgChaCha20Poly1305, service.KeyAlgXChaCha20Poly1305,
		service.KeyAlgRSA2048, service.KeyAlgRSA3072, service.KeyAlgRSA4096,
		service.KeyAlgECDSAP256, service.KeyAlgECDSAP384, service.KeyAlgECDSAP521,
		service.KeyAlgEd25519, service.KeyAlgEd448,
		service.KeyAlgX25519, service.KeyAlgX448,
		service.KeyAlgHMACSHA256, service.KeyAlgHMACSHA384, service.KeyAlgHMACSHA512,
		service.KeyAlgorithm("unknown"),
	}
	for _, alg := range algos {
		proto := convertServiceKeyAlgorithmToProto(alg)
		back := convertProtoToServiceKeyAlgorithm(proto)
		if alg == "unknown" {
			assert.Empty(t, back)
		} else {
			assert.Equal(t, alg, back)
		}
	}
}

func TestGRPCRemaining91_ClientKMS_KeyStatusConversions(t *testing.T) {
	statuses := []service.KeyStatus{
		service.KeyStatusEnabled, service.KeyStatusDisabled,
		service.KeyStatusPendingDeletion, service.KeyStatusDeleted,
		service.KeyStatusRotating, service.KeyStatusCompromised,
		service.KeyStatus("unknown"),
	}
	for _, s := range statuses {
		proto := convertServiceToProtoKeyStatus(s)
		back := convertProtoToServiceKeyStatus(proto)
		if s == "unknown" {
			assert.Empty(t, back)
		} else {
			assert.Equal(t, s, back)
		}
	}
}

func TestGRPCRemaining91_ClientKMS_KeyUsageConversions(t *testing.T) {
	usages := []pb.KeyUsage{
		pb.KeyUsage_KEY_USAGE_ENCRYPT, pb.KeyUsage_KEY_USAGE_DECRYPT,
		pb.KeyUsage_KEY_USAGE_SIGN, pb.KeyUsage_KEY_USAGE_VERIFY,
		pb.KeyUsage_KEY_USAGE_WRAP_KEY, pb.KeyUsage_KEY_USAGE_DERIVE_KEY,
		pb.KeyUsage_KEY_USAGE_GENERATE_MAC, pb.KeyUsage_KEY_USAGE_VERIFY_MAC,
		pb.KeyUsage_KEY_USAGE_UNSPECIFIED,
	}
	result := convertProtoToServiceKeyUsageList(usages)
	assert.Len(t, result, len(usages))
	assert.Equal(t, service.KeyUsageEncrypt, result[0])
	assert.Equal(t, service.KeyUsageDecrypt, result[1])
}

func TestGRPCRemaining91_ClientKMS_UnsupportedKeyAlgorithmError(t *testing.T) {
	err := &unsupportedKeyAlgorithmError{Algorithm: "test-alg"}
	assert.Equal(t, "unsupported key algorithm", err.Error())
}

// =============================================================================
// Client Crypto - Nil Request Tests
// =============================================================================

func TestGRPCRemaining91_Client_Crypto_NilRequests(t *testing.T) {
	client := &Client{conn: nil, connected: false, closed: false}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"KMSSign", func() error { _, err := client.KMSSign(ctx, nil); return err }},
		{"KMSVerify", func() error { _, err := client.KMSVerify(ctx, nil); return err }},
		{"KMSHash", func() error { _, err := client.KMSHash(ctx, nil); return err }},
		{"KMSGenerateHMAC", func() error { _, err := client.KMSGenerateHMAC(ctx, nil); return err }},
		{"KMSVerifyHMAC", func() error { _, err := client.KMSVerifyHMAC(ctx, nil); return err }},
		{"KMSRewrap", func() error { _, err := client.KMSRewrap(ctx, nil); return err }},
		{"KMSGenerateRandom", func() error { _, err := client.KMSGenerateRandom(ctx, nil); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
		})
	}
}

// =============================================================================
// Client Crypto - Hash Algorithm Conversion Tests
// =============================================================================

func TestGRPCRemaining91_ClientCrypto_HashAlgorithmConversions(t *testing.T) {
	algos := []service.HashAlgorithm{
		service.HashAlgSHA256, service.HashAlgSHA384, service.HashAlgSHA512,
		service.HashAlgSHA3_256, service.HashAlgSHA3_384, service.HashAlgSHA3_512,
		service.HashAlgBLAKE2b256, service.HashAlgBLAKE2b512,
		service.HashAlgorithm("unknown"),
	}
	for _, alg := range algos {
		proto := convertServiceHashAlgorithmToProto(alg)
		back := convertProtoToServiceHashAlgorithm(proto)
		if alg == "unknown" {
			assert.Equal(t, service.HashAlgSHA256, back) // default
		} else {
			assert.Equal(t, alg, back)
		}
	}
}

// =============================================================================
// Client Secrets - Nil Request Tests
// =============================================================================

func TestGRPCRemaining91_Client_Secrets_NilRequests(t *testing.T) {
	client := &Client{conn: nil, connected: false, closed: false}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"StoreSecret", func() error { _, err := client.StoreSecret(ctx, nil); return err }},
		{"GetSecret", func() error { _, err := client.GetSecret(ctx, nil); return err }},
		{"ListSecrets", func() error { _, err := client.ListSecrets(ctx, nil); return err }},
		{"DeleteSecret", func() error { _, err := client.DeleteSecret(ctx, nil); return err }},
		{"ListSecretVersions", func() error { _, err := client.ListSecretVersions(ctx, nil); return err }},
		{"RollbackSecret", func() error { _, err := client.RollbackSecret(ctx, nil); return err }},
		{"RotateSecret", func() error { _, err := client.RotateSecret(ctx, nil); return err }},
		{"UpdateSecretStatus", func() error { _, err := client.UpdateSecretStatus(ctx, nil); return err }},
		{"GetSecretsByPrefix", func() error { _, err := client.GetSecretsByPrefix(ctx, nil); return err }},
		{"WatchSecretsByPrefix", func() error { _, err := client.WatchSecretsByPrefix(ctx, nil); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
		})
	}
}

// =============================================================================
// Client Secrets - Conversion Helper Tests
// =============================================================================

func TestGRPCRemaining91_ClientSecrets_ConvertProtoSecret_Nil(t *testing.T) {
	assert.Nil(t, convertProtoSecret(nil))
}

func TestGRPCRemaining91_ClientSecrets_ConvertProtoSecretVersion_Nil(t *testing.T) {
	assert.Nil(t, convertProtoSecretVersion(nil))
}

func TestGRPCRemaining91_ClientSecrets_ConvertProtoSecretVersion_WithTimestamps(t *testing.T) {
	v := &pb.SecretVersion{
		SecretId:  "s1",
		Version:   2,
		CreatedAt: time.Now().Unix(),
		DeletedAt: time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	result := convertProtoSecretVersion(v)
	assert.NotNil(t, result)
	assert.NotNil(t, result.DeletedAt)
	assert.NotNil(t, result.ExpiresAt)
}

func TestGRPCRemaining91_ClientSecrets_StatusConversions(t *testing.T) {
	t.Run("ServiceToProto", func(t *testing.T) {
		assert.Equal(t, pb.SecretStatus_SECRET_STATUS_ENABLED, convertServiceSecretStatus(service.SecretStatusEnabled))
		assert.Equal(t, pb.SecretStatus_SECRET_STATUS_DISABLED, convertServiceSecretStatus(service.SecretStatusDisabled))
		assert.Equal(t, pb.SecretStatus_SECRET_STATUS_UNSPECIFIED, convertServiceSecretStatus(service.SecretStatus("unknown")))
	})

	t.Run("ProtoToService", func(t *testing.T) {
		assert.Equal(t, service.SecretStatusEnabled, convertProtoSecretStatusToService(pb.SecretStatus_SECRET_STATUS_ENABLED))
		assert.Equal(t, service.SecretStatusDisabled, convertProtoSecretStatusToService(pb.SecretStatus_SECRET_STATUS_DISABLED))
		assert.Equal(t, service.SecretStatusEnabled, convertProtoSecretStatusToService(pb.SecretStatus_SECRET_STATUS_UNSPECIFIED))
	})
}

// =============================================================================
// Server Object Transfer - Tests
// =============================================================================

func TestGRPCRemaining91_ObjectTransfer_NewServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)
	assert.NotNil(t, srv)
}

func TestGRPCRemaining91_ObjectTransfer_RegisterUnregister(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("data"), nil
	})

	srv.RegisterStore(1, store)
	assert.Contains(t, srv.stores, uint64(1))

	srv.UnregisterStore(1)
	assert.NotContains(t, srv.stores, uint64(1))
}

func TestGRPCRemaining91_ObjectTransfer_FetchObject_NilRequest(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	_, err := srv.FetchObject(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_ObjectTransfer_FetchObject_EmptyRef(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	_, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{ObjectRef: ""})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPCRemaining91_ObjectTransfer_FetchObject_ShardNotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	resp, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-1",
		ShardId:   999,
	})
	require.NoError(t, err)
	assert.False(t, resp.Found)
	assert.Contains(t, resp.ErrorMessage, "shard not found")
}

func TestGRPCRemaining91_ObjectTransfer_FetchObject_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("test-object-data"), nil
	})
	srv.RegisterStore(1, store)

	resp, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-1",
		ShardId:   1,
	})
	require.NoError(t, err)
	assert.True(t, resp.Found)
	assert.Equal(t, []byte("test-object-data"), resp.Data)
	assert.Equal(t, int64(16), resp.Size)
	assert.NotEmpty(t, resp.Checksum)
}

func TestGRPCRemaining91_ObjectTransfer_FetchObject_ReadError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return nil, stderrors.New("read failed")
	})
	srv.RegisterStore(1, store)

	resp, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-1",
		ShardId:   1,
	})
	require.NoError(t, err)
	assert.False(t, resp.Found)
	assert.Contains(t, resp.ErrorMessage, "read failed")
}

func TestGRPCRemaining91_ObjectTransfer_FetchObject_ChecksumMismatch(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("test-data"), nil
	})
	srv.RegisterStore(1, store)

	resp, err := srv.FetchObject(context.Background(), &pb.FetchObjectRequest{
		ObjectRef: "ref-1",
		ShardId:   1,
		Checksum:  []byte("wrong-checksum"),
	})
	require.NoError(t, err)
	assert.True(t, resp.Found)
	assert.Contains(t, resp.ErrorMessage, "checksum mismatch")
}

func TestGRPCRemaining91_ObjectTransfer_FetchSingleObject_ReadError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return nil, stderrors.New("disk failure")
	})

	resp := srv.fetchSingleObject(store, &pb.ObjectFetchItem{ObjectRef: "ref-1"})
	assert.False(t, resp.Found)
	assert.Contains(t, resp.ErrorMessage, "disk failure")
}

func TestGRPCRemaining91_ObjectTransfer_FetchSingleObject_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("data"), nil
	})

	resp := srv.fetchSingleObject(store, &pb.ObjectFetchItem{ObjectRef: "ref-1"})
	assert.True(t, resp.Found)
	assert.Equal(t, []byte("data"), resp.Data)
}

func TestGRPCRemaining91_ObjectTransfer_FetchSingleObject_ChecksumMismatch(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := NewObjectTransferServer(log)

	store := ObjectStoreReaderFunc(func(ref string) ([]byte, error) {
		return []byte("data"), nil
	})

	resp := srv.fetchSingleObject(store, &pb.ObjectFetchItem{ObjectRef: "ref-1", Checksum: []byte("wrong")})
	assert.True(t, resp.Found)
	assert.Contains(t, resp.ErrorMessage, "checksum mismatch")
}

// =============================================================================
// VectorDB Wrapper - Health Tests
// =============================================================================

func TestGRPCRemaining91_VectorDBWrapper_Health_NilServer(t *testing.T) {
	wrapper := &VectorDBWrapper{server: nil}
	assert.Panics(t, func() { wrapper.Health(context.Background(), &pb.VectorHealthRequest{}) })
}

func TestGRPCRemaining91_VectorDBWrapper_Health_NilService(t *testing.T) {
	srv := &Server{service: nil, nodeID: 1, version: "v1.0"}
	wrapper := NewVectorDBWrapper(srv)
	resp, err := wrapper.Health(context.Background(), &pb.VectorHealthRequest{})
	require.NoError(t, err)
	assert.Equal(t, pb.VectorHealthStatus_VECTOR_HEALTH_STATUS_UNHEALTHY, resp.Status)
	assert.Equal(t, uint64(1), resp.NodeId)
	assert.Equal(t, "v1.0", resp.Version)
}

func TestGRPCRemaining91_VectorAdminWrapper_New(t *testing.T) {
	srv := &Server{}
	wrapper := NewVectorAdminWrapper(srv)
	assert.NotNil(t, wrapper)
}

// =============================================================================
// Client - Helper Function Tests
// =============================================================================

func TestGRPCRemaining91_Client_IsUnixSocketAddress(t *testing.T) {
	tests := []struct {
		addr     string
		expected bool
	}{
		{"unix:///tmp/test.sock", true},
		{"unix-abstract://test", true},
		{"/tmp/test.sock", true},
		{"./test.sock", true},
		{"../test.sock", true},
		{"test.sock", true},
		{"localhost:50051", false},
		{"tcp://localhost:50051", false},
		{"host:50051", false},
	}
	for _, tt := range tests {
		result := IsUnixSocketAddress(tt.addr)
		assert.Equal(t, tt.expected, result, "addr: %s", tt.addr)
	}
}

func TestGRPCRemaining91_Client_NormalizeUnixSocketAddress(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"unix:///tmp/test.sock", "unix:///tmp/test.sock"},
		{"unix-abstract://test", "unix-abstract://test"},
		{"/tmp/test.sock", "unix:///tmp/test.sock"},
		{"localhost:50051", "localhost:50051"},
	}
	for _, tt := range tests {
		result := NormalizeUnixSocketAddress(tt.input)
		assert.Contains(t, result, tt.contains, "input: %s", tt.input)
	}
}

func TestGRPCRemaining91_Client_AddMetadata(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: map[string]string{"key": "value"},
		},
	}
	ctx := client.addMetadata(context.Background())
	assert.NotNil(t, ctx)

	// Call again to test cached md path
	ctx2 := client.addMetadata(context.Background())
	assert.NotNil(t, ctx2)
}

func TestGRPCRemaining91_Client_AddMetadata_Empty(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: map[string]string{},
		},
	}
	ctx := client.addMetadata(context.Background())
	assert.NotNil(t, ctx)
}

func TestGRPCRemaining91_Client_IsConnected(t *testing.T) {
	t.Run("Connected", func(t *testing.T) {
		client := &Client{connected: true, closed: false}
		assert.True(t, client.isConnected())
	})

	t.Run("Disconnected", func(t *testing.T) {
		client := &Client{connected: false, closed: false}
		assert.False(t, client.isConnected())
	})

	t.Run("Closed", func(t *testing.T) {
		client := &Client{connected: true, closed: true}
		assert.False(t, client.isConnected())
	})
}

func TestGRPCRemaining91_Client_GetClient_NilClient(t *testing.T) {
	client := &Client{client: nil, pool: nil, connected: false}
	_, err := client.getClient()
	require.Error(t, err)
}

func TestGRPCRemaining91_Client_Connect_AlreadyConnected(t *testing.T) {
	client := &Client{connected: true}
	err := client.Connect(context.Background())
	assert.NoError(t, err)
}

func TestGRPCRemaining91_Client_Connect_Closed(t *testing.T) {
	client := &Client{closed: true}
	err := client.Connect(context.Background())
	require.Error(t, err)
}

func TestGRPCRemaining91_Client_Connect_NilOpts(t *testing.T) {
	client := &Client{opts: nil}
	err := client.Connect(context.Background())
	require.Error(t, err)
}

func TestGRPCRemaining91_Client_Close_AlreadyClosed(t *testing.T) {
	client := &Client{closed: true}
	err := client.Close()
	assert.NoError(t, err)
}

func TestGRPCRemaining91_Client_Close_NilConn(t *testing.T) {
	client := &Client{closed: false, conn: nil, pool: nil}
	err := client.Close()
	assert.NoError(t, err)
	assert.True(t, client.closed)
}

func TestGRPCRemaining91_Client_Disconnect(t *testing.T) {
	client := &Client{closed: true}
	err := client.Disconnect()
	assert.NoError(t, err)
}

func TestGRPCRemaining91_Client_Conn(t *testing.T) {
	client := &Client{conn: nil}
	assert.Nil(t, client.Conn())
}

// =============================================================================
// Vector Export - Serialization Tests
// =============================================================================

func TestGRPCRemaining91_VectorExport_Serialize_JSON(t *testing.T) {
	batch := &VectorExportBatch{
		Namespace: "test-ns",
		BatchNum:  1,
		Total:     10,
		Vectors: []VectorExportEntry{
			{ID: "v1", Vector: []float32{1.0, 2.0, 3.0}},
		},
	}

	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestGRPCRemaining91_VectorExport_Serialize_DefaultFormat(t *testing.T) {
	batch := &VectorExportBatch{Namespace: "ns", Vectors: []VectorExportEntry{}}
	data, err := serializeExportBatch(batch, "unsupported-format", "")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestGRPCRemaining91_VectorExport_Serialize_Gzip(t *testing.T) {
	batch := &VectorExportBatch{
		Namespace: "test-ns",
		Vectors:   []VectorExportEntry{{ID: "v1", Vector: []float32{1.0, 2.0}}},
	}

	data, err := serializeExportBatch(batch, "json", "gzip")
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Deserialize back
	result, err := deserializeExportBatch(data, "json", "gzip")
	require.NoError(t, err)
	assert.Equal(t, "test-ns", result.Namespace)
	assert.Len(t, result.Vectors, 1)
}

func TestGRPCRemaining91_VectorExport_Deserialize_NoCompression(t *testing.T) {
	batch := &VectorExportBatch{
		Namespace: "test-ns",
		Vectors:   []VectorExportEntry{{ID: "v1", Vector: []float32{1.0}}},
	}

	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)

	result, err := deserializeExportBatch(data, "json", "")
	require.NoError(t, err)
	assert.Equal(t, "test-ns", result.Namespace)
}

func TestGRPCRemaining91_VectorExport_Deserialize_InvalidGzip(t *testing.T) {
	_, err := deserializeExportBatch([]byte("not gzip data"), "json", "gzip")
	require.Error(t, err)
}

func TestGRPCRemaining91_VectorExport_Deserialize_InvalidJSON(t *testing.T) {
	_, err := deserializeExportBatch([]byte("not json"), "json", "none")
	require.Error(t, err)
}

// =============================================================================
// Client KMS Backend Tests
// =============================================================================

func TestGRPCRemaining91_Client_KMSBackend_Closed(t *testing.T) {
	client := &Client{conn: nil, connected: false, closed: true}
	ctx := context.Background()

	t.Run("BackendStatus", func(t *testing.T) {
		_, err := client.KMSBackendStatus(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "closed")
	})

	t.Run("BackendTest", func(t *testing.T) {
		_, err := client.KMSBackendTest(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "closed")
	})
}

// PEP tests (convertPEPError, convertPolicyUpdateType) are in server_pep_unit_test.go.
// server_pep.go is untagged, so its tests must be too (the file previously carried a
// phantom //go:build grpc tag that no Makefile target ever set).

// =============================================================================
// API Error Types
// =============================================================================

func TestGRPCRemaining91_API_ErrorTypes(t *testing.T) {
	t.Run("NilInputError", func(t *testing.T) {
		err := api.NewNilInputError("op", "param")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil")
	})

	t.Run("ClientClosedError", func(t *testing.T) {
		err := api.NewClientClosedError("op")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "closed")
	})
}

// =============================================================================
// Server Timeseries - Additional Coverage
// =============================================================================

func TestGRPCRemaining91_ServerTimeseries_NilService(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := &Server{
		logger:  log,
		service: nil, // No timeseries service
	}
	ctx := context.Background()

	t.Run("TSWritePoint", func(t *testing.T) {
		_, err := srv.TSWritePoint(ctx, &pb.TSWritePointRequest{Metric: "m"})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSWriteBatch", func(t *testing.T) {
		_, err := srv.TSWriteBatch(ctx, &pb.TSWriteBatchRequest{
			Points: []*pb.TSDataPoint{{Metric: "m", Value: 1.0}},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSQuery", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{
			Metric: "m", StartTime: 1, EndTime: 2,
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSDeleteSeries", func(t *testing.T) {
		_, err := srv.TSDeleteSeries(ctx, &pb.TSDeleteSeriesRequest{Metric: "m"})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSGetRetentionPolicy", func(t *testing.T) {
		_, err := srv.TSGetRetentionPolicy(ctx, &pb.TSGetRetentionPolicyRequest{Metric: "m"})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, &pb.TSSetRetentionPolicyRequest{
			Metric: "m",
			Policy: &pb.TSRetentionPolicy{DurationSeconds: 3600},
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSListMetrics", func(t *testing.T) {
		_, err := srv.TSListMetrics(ctx, &pb.TSListMetricsRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}
