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

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// fakeCertificateServer implements CertificateService.
type fakeCertificateServer struct {
	pb.UnimplementedCertificateServiceServer
	fail bool
}

func (f *fakeCertificateServer) e() error { return failErr(f.fail) }

func certTime() *timestamppb.Timestamp { return timestamppb.Now() }

func (f *fakeCertificateServer) IssueCertificate(context.Context, *pb.IssueCertificateRequest) (*pb.IssueCertificateResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.IssueCertificateResponse{
		CertificatePem: "cert", PrivateKeyPem: "key", CaCertificatePem: "ca", ChainPem: "chain",
		SerialNumber: "01", NotBefore: certTime(), NotAfter: certTime(),
	}, nil
}
func (f *fakeCertificateServer) SignCSR(context.Context, *pb.SignCSRRequest) (*pb.SignCSRResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.SignCSRResponse{
		CertificatePem: "cert", CaCertificatePem: "ca", ChainPem: "chain",
		SerialNumber: "02", NotBefore: certTime(), NotAfter: certTime(),
	}, nil
}
func (f *fakeCertificateServer) RevokeCertificate(context.Context, *pb.RevokeCertificateRequest) (*pb.RevokeCertificateResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RevokeCertificateResponse{Success: true}, nil
}
func (f *fakeCertificateServer) VerifyCertificate(context.Context, *pb.VerifyCertificateRequest) (*pb.VerifyCertificateResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.VerifyCertificateResponse{Valid: true, ErrorMessage: ""}, nil
}
func (f *fakeCertificateServer) GetCACertificate(context.Context, *pb.GetCACertificateRequest) (*pb.GetCACertificateResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetCACertificateResponse{CaCertificatePem: "ca", CaBundlePem: "bundle", Identity: "id"}, nil
}
func (f *fakeCertificateServer) GetTLSConfig(context.Context, *pb.GetTLSConfigRequest) (*pb.GetTLSConfigResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetTLSConfigResponse{ServerName: "host", IsServer: true}, nil
}

func newCertificateHandlerWithServer(t *testing.T, fail bool) *certificateHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterCertificateServiceServer(s, &fakeCertificateServer{fail: fail})
	})
	return newCertificateHandler(srv.dial(t))
}

var certCases = []struct {
	name   string
	method string
	req    interface{}
}{
	{"IssueCertificate", "IssueCertificate", &certificate.IssueCertificateRequest{CommonName: "x", Usage: "server", KeyAlgorithm: "ecdsa", ValidityDays: 30}},
	{"SignCSR", "SignCSR", &certificate.SignCSRRequest{CSRPEM: "csr", ValidityDays: 30, Usage: "client"}},
	{"RevokeCertificate", "RevokeCertificate", &certificate.RevokeCertificateRequest{SerialNumber: "01"}},
	{"VerifyCertificate", "VerifyCertificate", &certificate.VerifyCertificateRequest{CertificatePEM: "cert"}},
	{"GetCACertificate", "GetCACertificate", &certificate.GetCACertificateRequest{}},
	{"GetTLSConfig", "GetTLSConfig", &certificate.GetTLSConfigRequest{ServerName: "host", IsServer: true, MinVersion: "1.3"}},
}

func TestCertificateHandler_AllMethods_Success(t *testing.T) {
	h := newCertificateHandlerWithServer(t, false)
	for _, tc := range certCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Certificate", tc.method, tc.req); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestCertificateHandler_AllMethods_ServerError(t *testing.T) {
	h := newCertificateHandlerWithServer(t, true)
	for _, tc := range certCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.Certificate", tc.method, tc.req); err == nil {
				t.Fatalf("%s: expected error from failing server", tc.name)
			}
		})
	}
}

func TestCertificateHandler_GetCACertificate_NilRequest(t *testing.T) {
	h := newCertificateHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Certificate", "GetCACertificate", nil); err != nil {
		t.Fatalf("nil request should be tolerated: %v", err)
	}
}
