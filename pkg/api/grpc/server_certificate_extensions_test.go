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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// CrossSign
// =============================================================================

func TestServer_CrossSign_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.CertificateSvc = &mockCertificateService{
		CrossSignFunc: func(_ context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
			assert.Equal(t, []byte("foreign-root"), req.ForeignRootPEM)
			assert.Equal(t, "tenant-a", req.TenantID)
			return &service.CrossSignResponse{CrossSignedPEM: []byte("cross-signed")}, nil
		},
	}

	resp, err := srv.CrossSign(context.Background(), &pb.CrossSignRequest{
		ForeignRootPem: []byte("foreign-root"),
		TenantId:       "tenant-a",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []byte("cross-signed"), resp.CrossSignedPem)
	assert.NotNil(t, resp.Timestamp)
}

func TestServer_CrossSign_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.CrossSign(ctx, nil)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing foreign root", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.CrossSign(ctx, &pb.CrossSignRequest{})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("certificate service unavailable", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.CrossSign(ctx, &pb.CrossSignRequest{ForeignRootPem: []byte("r")})
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("CA lacks the capability", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = &mockCertificateService{
			CrossSignFunc: func(context.Context, *service.CrossSignRequest) (*service.CrossSignResponse, error) {
				return nil, service.ErrCertExtNotSupported
			},
		}
		_, err := srv.CrossSign(ctx, &pb.CrossSignRequest{ForeignRootPem: []byte("r")})
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// Chain
// =============================================================================

func TestServer_Chain_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.CertificateSvc = &mockCertificateService{
		ChainFunc: func(_ context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
			assert.Equal(t, []byte("leaf"), req.CertificatePEM)
			return &service.CertificateChainResponse{
				ChainPEM: [][]byte{[]byte("issuer"), []byte("root")},
			}, nil
		},
	}

	resp, err := srv.Chain(context.Background(), &pb.CertificateChainRequest{
		CertificatePem: []byte("leaf"),
	})
	require.NoError(t, err)
	require.Len(t, resp.ChainPem, 2)
	assert.Equal(t, []byte("issuer"), resp.ChainPem[0])
	assert.Equal(t, []byte("root"), resp.ChainPem[1])
}

func TestServer_Chain_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.Chain(ctx, nil)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing certificate", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.Chain(ctx, &pb.CertificateChainRequest{})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("certificate service unavailable", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.Chain(ctx, &pb.CertificateChainRequest{CertificatePem: []byte("c")})
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func TestServer_IssueDeviceIdentity_Success(t *testing.T) {
	notAfter := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)

	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.CertificateSvc = &mockCertificateService{
		IssueDeviceIdentityFunc: func(_ context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
			assert.Equal(t, "idevid", req.Type)
			assert.Equal(t, "device-7", req.DeviceID)
			assert.Equal(t, []byte("tcg-csr"), req.CSRPEM)
			assert.Equal(t, 45, req.ValidityDays)
			return &service.IssueDeviceIdentityResponse{
				CertificatePEM: []byte("idevid-cert"),
				ChainPEM:       []byte("chain"),
				SerialNumber:   "dead",
				NotAfter:       notAfter,
			}, nil
		},
	}

	resp, err := srv.IssueDeviceIdentity(context.Background(), &pb.IssueDeviceIdentityRequest{
		Type:         "idevid",
		DeviceId:     "device-7",
		CsrPem:       []byte("tcg-csr"),
		ValidityDays: 45,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("idevid-cert"), resp.CertificatePem)
	assert.Equal(t, "dead", resp.SerialNumber)
	require.NotNil(t, resp.NotAfter)
	assert.Equal(t, notAfter, resp.NotAfter.AsTime())
}

func TestServer_IssueDeviceIdentity_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.IssueDeviceIdentity(ctx, nil)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing type", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.IssueDeviceIdentity(ctx, &pb.IssueDeviceIdentityRequest{DeviceId: "d"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing device ID", func(t *testing.T) {
		srv, _ := createTestServerWithMockService(t)
		_, err := srv.IssueDeviceIdentity(ctx, &pb.IssueDeviceIdentityRequest{Type: "ek"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("certificate service unavailable", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = nil
		_, err := srv.IssueDeviceIdentity(ctx, &pb.IssueDeviceIdentityRequest{Type: "ek", DeviceId: "d"})
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("unknown identity type surfaces as invalid argument", func(t *testing.T) {
		srv, mockSvc := createTestServerWithMockService(t)
		mockSvc.CertificateSvc = &mockCertificateService{
			IssueDeviceIdentityFunc: func(context.Context, *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
				return nil, service.ErrCertExtUnknownIdentityType
			},
		}
		_, err := srv.IssueDeviceIdentity(ctx, &pb.IssueDeviceIdentityRequest{Type: "ldevid", DeviceId: "d"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// =============================================================================
// Authorization mapping
// =============================================================================

// TestCertificateExtensionGRPCPermissions pins the permission each new RPC
// resolves to. The gRPC map is default-deny, so an unmapped method would be
// denied outright; these assertions additionally pin WHICH permission, so the
// privilege boundary between minting a CA and issuing a leaf cannot be
// weakened silently.
func TestCertificateExtensionGRPCPermissions(t *testing.T) {
	cases := map[string]struct{ resource, action string }{
		"/quicbaas.certificate.v1.CertificateService/CrossSign":           {"ca", "create"},
		"/quicbaas.certificate.v1.CertificateService/Chain":               {"certs", "generate"},
		"/quicbaas.certificate.v1.CertificateService/IssueDeviceIdentity": {"certs", "generate"},
	}

	for method, want := range cases {
		t.Run(method, func(t *testing.T) {
			got, ok := grpcMethodPermissions[method]
			require.True(t, ok, "method %s must be mapped or it is default-denied", method)
			assert.Equal(t, want.resource, got.resource)
			assert.Equal(t, want.action, got.action)
		})
	}
}

// TestCrossSignRequiresCAAuthority guards the boundary qrdb documents:
// certs:generate must not imply the authority to mint a signing CA.
func TestCrossSignRequiresCAAuthority(t *testing.T) {
	crossSign := grpcMethodPermissions["/quicbaas.certificate.v1.CertificateService/CrossSign"]
	issue := grpcMethodPermissions["/quicbaas.certificate.v1.CertificateService/IssueCertificate"]

	assert.NotEqual(t, issue, crossSign,
		"cross-signing must not share the leaf-issuance permission")
	assert.Equal(t, "ca", crossSign.resource)
}
