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
	stderrors "errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingTrustedCAServer records each TrustedCAService request and answers
// fixed responses.
type recordingTrustedCAServer struct {
	pb.UnimplementedTrustedCAServiceServer
	got interface{}
}

func (s *recordingTrustedCAServer) GetCABundle(_ context.Context, r *pb.TrustedCAGetCABundleRequest) (*pb.TrustedCAGetCABundleResponse, error) {
	s.got = r
	return &pb.TrustedCAGetCABundleResponse{BundlePem: []byte("B"), Certificates: [][]byte{[]byte("D")}}, nil
}

func (s *recordingTrustedCAServer) GetCACertificate(_ context.Context, r *pb.TrustedCAGetCACertificateRequest) (*pb.TrustedCAGetCACertificateResponse, error) {
	s.got = r
	return &pb.TrustedCAGetCACertificateResponse{CertificatePem: []byte("C"), IsCa: true}, nil
}

func (s *recordingTrustedCAServer) SignCSR(_ context.Context, r *pb.TrustedCASignCSRRequest) (*pb.TrustedCASignCSRResponse, error) {
	s.got = r
	return &pb.TrustedCASignCSRResponse{CertificatePem: []byte("L"), SerialNumber: "0a"}, nil
}

func (s *recordingTrustedCAServer) IssueCertificate(_ context.Context, r *pb.TrustedCAIssueCertificateRequest) (*pb.TrustedCAIssueCertificateResponse, error) {
	s.got = r
	return &pb.TrustedCAIssueCertificateResponse{PrivateKeyPem: []byte("K"), SerialNumber: "0b"}, nil
}

func (s *recordingTrustedCAServer) RevokeCertificate(_ context.Context, r *pb.TrustedCARevokeCertificateRequest) (*pb.TrustedCARevokeCertificateResponse, error) {
	s.got = r
	return &pb.TrustedCARevokeCertificateResponse{Success: true}, nil
}

func (s *recordingTrustedCAServer) GenerateCRL(_ context.Context, r *pb.TrustedCAGenerateCRLRequest) (*pb.TrustedCAGenerateCRLResponse, error) {
	s.got = r
	return &pb.TrustedCAGenerateCRLResponse{CrlPem: []byte("CRL")}, nil
}

func (s *recordingTrustedCAServer) IssueEKCertificate(_ context.Context, r *pb.TrustedCAIssueEKCertificateRequest) (*pb.TrustedCAIssueEKCertificateResponse, error) {
	s.got = r
	return &pb.TrustedCAIssueEKCertificateResponse{CertificateDer: []byte("EKDER"), CertificatePem: []byte("EKPEM"), SerialNumber: "0e"}, nil
}

func (s *recordingTrustedCAServer) IssueAKCertificate(_ context.Context, r *pb.TrustedCAIssueAKCertificateRequest) (*pb.TrustedCAIssueAKCertificateResponse, error) {
	s.got = r
	return &pb.TrustedCAIssueAKCertificateResponse{CertificateDer: []byte("AKDER"), CertificatePem: []byte("AKPEM"), SerialNumber: "0f"}, nil
}

func (s *recordingTrustedCAServer) SignTCGCSR(_ context.Context, r *pb.TrustedCASignTCGCSRRequest) (*pb.TrustedCASignTCGCSRResponse, error) {
	s.got = r
	return &pb.TrustedCASignTCGCSRResponse{IakCertDer: []byte("IAK"), IdevidCertDer: []byte("IDEVID")}, nil
}

func (s *recordingTrustedCAServer) EnrollDevice(_ context.Context, r *pb.TrustedCAEnrollDeviceRequest) (*pb.TrustedCAEnrollDeviceResponse, error) {
	s.got = r
	return &pb.TrustedCAEnrollDeviceResponse{
		IakCertDer: []byte("IAK"), IdevidCertDer: []byte("IDEVID"),
		CredentialBlob: []byte("BLOB"), EncryptedSecret: []byte("ENC"), PlainSecret: []byte("PLAIN"),
	}, nil
}

// IsRevoked is left to UnimplementedTrustedCAServiceServer so the error path
// is exercised.

func startTrustedCAServer(t *testing.T, srv *recordingTrustedCAServer) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterTrustedCAServiceServer(server, srv)
	served := make(chan error, 1)
	go func() { served <- server.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
		server.Stop()
		if err := <-served; err != nil && !stderrors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	})
	return conn
}

func TestIsTrustedCAService(t *testing.T) {
	for svc, want := range map[string]bool{"quicbaas.TrustedCA": true, "TrustedCA": true, "quicbaas.PIV": false} {
		if got := isTrustedCAService(svc); got != want {
			t.Errorf("isTrustedCAService(%q) = %v, want %v", svc, got, want)
		}
	}
}

// TestTrustedCAHandler_RoundTrip pins #1330 on the SDK gRPC client: each
// go-xkms CAService request reaches TrustedCAService as its proto twin and
// the proto response comes back as the go-xkms response.
func TestTrustedCAHandler_RoundTrip(t *testing.T) {
	srv := &recordingTrustedCAServer{}
	h := newTrustedCAHandler(startTrustedCAServer(t, srv))
	ctx := context.Background()
	call := func(method string, req interface{}) interface{} {
		t.Helper()
		out, err := h.handleRequest(ctx, trustedCAServicePrefix, method, req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return out
	}

	if out := call("GetCABundle", &xkmstransport.GetCABundleRequest{StoreType: "tpm2", TenantID: "t1"}).(*xkmstransport.GetCABundleResponse); string(out.BundlePEM) != "B" || len(out.Certificates) != 1 {
		t.Fatalf("GetCABundle = %+v", out)
	}
	if got := srv.got.(*pb.TrustedCAGetCABundleRequest); got.StoreType != "tpm2" || got.TenantId != "t1" {
		t.Fatalf("server got %+v", got)
	}

	if out := call("GetCACertificate", &xkmstransport.GetCACertificateRequest{Identity: "root"}).(*xkmstransport.GetCACertificateResponse); !out.IsCA {
		t.Fatalf("GetCACertificate = %+v", out)
	}
	if got := srv.got.(*pb.TrustedCAGetCACertificateRequest); got.Identity != "root" {
		t.Fatalf("server got %+v", got)
	}

	if out := call("SignCSR", &xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM"), ValidityDays: 30}).(*xkmstransport.SignCSRResponse); out.SerialNumber != "0a" {
		t.Fatalf("SignCSR = %+v", out)
	}
	if got := srv.got.(*pb.TrustedCASignCSRRequest); string(got.CsrPem) != "PEM" || got.ValidityDays != 30 {
		t.Fatalf("server got %+v", got)
	}

	if out := call("IssueCertificate", &xkmstransport.IssueCertificateRequest{CommonName: "n", SANs: []string{"n"}}).(*xkmstransport.IssueCertificateResponse); string(out.PrivateKeyPEM) != "K" || out.SerialNumber != "0b" {
		t.Fatalf("IssueCertificate = %+v", out)
	}
	if got := srv.got.(*pb.TrustedCAIssueCertificateRequest); got.CommonName != "n" || len(got.Sans) != 1 {
		t.Fatalf("server got %+v", got)
	}

	if out := call("RevokeCertificate", &xkmstransport.RevokeCertificateRequest{SerialNumber: "0a", Reason: 1}).(*xkmstransport.RevokeCertificateResponse); !out.Success {
		t.Fatalf("RevokeCertificate = %+v", out)
	}
	if got := srv.got.(*pb.TrustedCARevokeCertificateRequest); got.SerialNumber != "0a" || got.Reason != 1 {
		t.Fatalf("server got %+v", got)
	}

	if out := call("GenerateCRL", &xkmstransport.GenerateCRLRequest{TenantID: "t1"}).(*xkmstransport.GenerateCRLResponse); string(out.CRLPEM) != "CRL" {
		t.Fatalf("GenerateCRL = %+v", out)
	}
}

// TestTrustedCAHandler_TCGRoundTrip drives the four TCG CA verbs over the
// Unix transport and proves each reaches TrustedCAService with its fields
// mapped, and that the response comes back mapped.
func TestTrustedCAHandler_TCGRoundTrip(t *testing.T) {
	srv := &recordingTrustedCAServer{}
	tr := caTestTransport(startTrustedCAServer(t, srv), transport.AddressTypeUnix)
	ctx := context.Background()
	call := func(method string, req interface{}) interface{} {
		t.Helper()
		out, err := tr.Request(ctx, "quicbaas.TCGCA", method, req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return out
	}

	ek := call("IssueEKCertificate", &xkmstransport.IssueEKCertificateRequest{
		CommonName: "ek", Organization: "org", EKPublicKey: []byte("EKPUB"), TenantID: "t1",
	}).(*xkmstransport.IssueEKCertificateResponse)
	if string(ek.CertificateDER) != "EKDER" || string(ek.CertificatePEM) != "EKPEM" || ek.SerialNumber != "0e" {
		t.Fatalf("IssueEKCertificate = %+v", ek)
	}
	if got := srv.got.(*pb.TrustedCAIssueEKCertificateRequest); got.CommonName != "ek" || got.Organization != "org" ||
		string(got.EkPublicKey) != "EKPUB" || got.TenantId != "t1" {
		t.Fatalf("server got %+v", got)
	}

	ak := call("IssueAKCertificate", &xkmstransport.IssueAKCertificateRequest{
		CommonName: "ak", Organization: "org", PublicKey: []byte("AKPUB"), TenantID: "t1",
	}).(*xkmstransport.IssueAKCertificateResponse)
	if string(ak.CertificateDER) != "AKDER" || string(ak.CertificatePEM) != "AKPEM" || ak.SerialNumber != "0f" {
		t.Fatalf("IssueAKCertificate = %+v", ak)
	}
	if got := srv.got.(*pb.TrustedCAIssueAKCertificateRequest); got.CommonName != "ak" || got.Organization != "org" ||
		string(got.PublicKey) != "AKPUB" || got.TenantId != "t1" {
		t.Fatalf("server got %+v", got)
	}

	signed := call("SignTCGCSR", &xkmstransport.SignTCGCSRRequest{
		CommonName: "dev", Organization: "org", TCGCSR: []byte("CSR"), TenantID: "t1",
	}).(*xkmstransport.SignTCGCSRResponse)
	if string(signed.IAKCertDER) != "IAK" || string(signed.IDevIDCertDER) != "IDEVID" {
		t.Fatalf("SignTCGCSR = %+v", signed)
	}
	if got := srv.got.(*pb.TrustedCASignTCGCSRRequest); got.CommonName != "dev" || got.Organization != "org" ||
		string(got.TcgCsr) != "CSR" || got.TenantId != "t1" {
		t.Fatalf("server got %+v", got)
	}

	enrolled := call("EnrollDevice", &xkmstransport.EnrollDeviceRequest{
		CommonName: "dev", Organization: "org", PackedCSR: []byte("PACKED"), TenantID: "t1",
	}).(*xkmstransport.EnrollDeviceResponse)
	if string(enrolled.IAKCertDER) != "IAK" || string(enrolled.IDevIDCertDER) != "IDEVID" ||
		string(enrolled.CredentialBlob) != "BLOB" || string(enrolled.EncryptedSecret) != "ENC" ||
		string(enrolled.PlainSecret) != "PLAIN" {
		t.Fatalf("EnrollDevice = %+v", enrolled)
	}
	if got := srv.got.(*pb.TrustedCAEnrollDeviceRequest); got.CommonName != "dev" || got.Organization != "org" ||
		string(got.PackedCsr) != "PACKED" || got.TenantId != "t1" {
		t.Fatalf("server got %+v", got)
	}
}

// TestTrustedCAHandler_Errors covers an unknown method, a wrong request type
// and a server error.
func TestTrustedCAHandler_Errors(t *testing.T) {
	h := newTrustedCAHandler(startTrustedCAServer(t, &recordingTrustedCAServer{}))
	ctx := context.Background()
	if _, err := h.handleRequest(ctx, trustedCAServicePrefix, "Nope", nil); err == nil {
		t.Fatal("unknown method: want error")
	}
	if _, err := h.handleRequest(ctx, trustedCAServicePrefix, "SignCSR", &xkmstransport.IsRevokedRequest{}); err == nil {
		t.Fatal("wrong request type: want error")
	}
	if _, err := h.handleRequest(ctx, trustedCAServicePrefix, "IsRevoked", &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}); err == nil {
		t.Fatal("unimplemented server method: want error")
	}
}
