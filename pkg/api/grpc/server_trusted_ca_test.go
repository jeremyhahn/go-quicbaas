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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// trustedCAFacade answers TrustedCA with the given CA.
type trustedCAFacade struct {
	service.QuicBaaS
	ca service.TrustedCAService
}

func (f *trustedCAFacade) TrustedCA() service.TrustedCAService { return f.ca }

// grpcFakeCA records each go-xkms request and answers fixed responses.
type grpcFakeCA struct{ got interface{} }

func (f *grpcFakeCA) GetCABundle(_ context.Context, r *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	f.got = r
	return &xkmstransport.GetCABundleResponse{BundlePEM: []byte("B"), Certificates: [][]byte{[]byte("D")}, ContentType: "application/x-pem-file"}, nil
}

func (f *grpcFakeCA) GetCACertificate(_ context.Context, r *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	f.got = r
	return &xkmstransport.GetCACertificateResponse{CertificatePEM: []byte("C"), IsCA: true, SerialNumber: "01"}, nil
}

func (f *grpcFakeCA) SignCSR(_ context.Context, r *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	f.got = r
	return &xkmstransport.SignCSRResponse{CertificatePEM: []byte("L"), ChainPEM: []byte("C"), SerialNumber: "0a"}, nil
}

func (f *grpcFakeCA) IssueCertificate(_ context.Context, r *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	f.got = r
	return &xkmstransport.IssueCertificateResponse{CertificatePEM: []byte("L"), SerialNumber: "0b"}, nil
}

func (f *grpcFakeCA) RevokeCertificate(_ context.Context, r *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	f.got = r
	return &xkmstransport.RevokeCertificateResponse{Success: true}, nil
}

func (f *grpcFakeCA) GenerateCRL(_ context.Context, r *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	f.got = r
	return &xkmstransport.GenerateCRLResponse{CRLPEM: []byte("CRL")}, nil
}

func (f *grpcFakeCA) IsRevoked(_ context.Context, r *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	f.got = r
	return &xkmstransport.IsRevokedResponse{Revoked: true, Reason: 1}, nil
}

// TestTrustedCAServer_DelegatesToTheWiredCA pins #1330 on the gRPC server:
// each TrustedCAService RPC converts to go-xkms's own request, calls the CA
// the server wired, and converts the response back.
func TestTrustedCAServer_DelegatesToTheWiredCA(t *testing.T) {
	ca := &grpcFakeCA{}
	s := &trustedCAServer{service: &trustedCAFacade{ca: ca}}
	ctx := context.Background()

	bundle, err := s.GetCABundle(ctx, &pb.TrustedCAGetCABundleRequest{StoreType: "tpm2", TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.GetCABundleRequest{StoreType: "tpm2", TenantID: "t1"}, ca.got)
	assert.Equal(t, []byte("B"), bundle.BundlePem)
	assert.Len(t, bundle.Certificates, 1)

	cert, err := s.GetCACertificate(ctx, &pb.TrustedCAGetCACertificateRequest{Identity: "root"})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.GetCACertificateRequest{Identity: "root"}, ca.got)
	assert.True(t, cert.IsCa)

	signed, err := s.SignCSR(ctx, &pb.TrustedCASignCSRRequest{CsrPem: []byte("PEM"), Profile: "server", ValidityDays: 30})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM"), Profile: "server", ValidityDays: 30}, ca.got)
	assert.Equal(t, "0a", signed.SerialNumber)

	issued, err := s.IssueCertificate(ctx, &pb.TrustedCAIssueCertificateRequest{CommonName: "node-1", Sans: []string{"node-1"}})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.IssueCertificateRequest{CommonName: "node-1", SANs: []string{"node-1"}}, ca.got)
	assert.Equal(t, "0b", issued.SerialNumber)

	revoked, err := s.RevokeCertificate(ctx, &pb.TrustedCARevokeCertificateRequest{SerialNumber: "0a", Reason: 1})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.RevokeCertificateRequest{SerialNumber: "0a", Reason: 1}, ca.got)
	assert.True(t, revoked.Success)

	crl, err := s.GenerateCRL(ctx, &pb.TrustedCAGenerateCRLRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.GenerateCRLRequest{TenantID: "t1"}, ca.got)
	assert.Equal(t, []byte("CRL"), crl.CrlPem)

	is, err := s.IsRevoked(ctx, &pb.TrustedCAIsRevokedRequest{SerialNumber: "0a"})
	require.NoError(t, err)
	assert.Equal(t, &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}, ca.got)
	assert.True(t, is.Revoked)
	assert.Equal(t, int32(1), is.Reason)
}

// TestTrustedCAServer_Unavailable answers Unimplemented before a CA is wired
// and InvalidArgument for a nil request.
func TestTrustedCAServer_Unavailable(t *testing.T) {
	ctx := context.Background()
	_, err := (&trustedCAServer{service: &trustedCAFacade{}}).IsRevoked(ctx, &pb.TrustedCAIsRevokedRequest{SerialNumber: "0a"})
	assert.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = (&trustedCAServer{service: &trustedCAFacade{ca: &grpcFakeCA{}}}).SignCSR(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestTrustedCAService_PermissionsMapped keeps every TrustedCAService RPC
// under the exhaustive gRPC permission guard.
func TestTrustedCAService_PermissionsMapped(t *testing.T) {
	found := false
	for _, desc := range brokerServiceDescs {
		if desc.ServiceName == pb.TrustedCAService_ServiceDesc.ServiceName {
			found = true
		}
	}
	require.True(t, found, "TrustedCAService must be listed in brokerServiceDescs")
	for _, m := range pb.TrustedCAService_ServiceDesc.Methods {
		_, _, mapped := grpcResourceAndAction("/" + pb.TrustedCAService_ServiceDesc.ServiceName + "/" + m.MethodName)
		assert.True(t, mapped, "%s is not mapped", m.MethodName)
	}
}
