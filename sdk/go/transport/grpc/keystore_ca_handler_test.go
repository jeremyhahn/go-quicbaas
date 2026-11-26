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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingKeystoreCAServer stands in for the node's TCP gRPC listener: it
// serves go-xkms's xkms.v1.KeystoreService with the CA and TCG CA verbs, and
// no TrustedCAService. It records the last request and counts the calls.
type recordingKeystoreCAServer struct {
	xkmsv1.UnimplementedKeystoreServiceServer
	got   interface{}
	calls int
}

func (s *recordingKeystoreCAServer) record(req interface{}) {
	s.got = req
	s.calls++
}

func (s *recordingKeystoreCAServer) GetCABundle(_ context.Context, req *xkmsv1.GetCABundleRequest) (*xkmsv1.GetCABundleResponse, error) {
	s.record(req)
	return &xkmsv1.GetCABundleResponse{BundlePem: []byte("B"), Certificates: [][]byte{[]byte("D")}, ContentType: "application/pem-certificate-chain"}, nil
}

func (s *recordingKeystoreCAServer) GetCACertificate(_ context.Context, req *xkmsv1.GetCACertificateRequest) (*xkmsv1.GetCACertificateResponse, error) {
	s.record(req)
	return &xkmsv1.GetCACertificateResponse{CertificatePem: []byte("C"), IsCa: true}, nil
}

func (s *recordingKeystoreCAServer) SignCSR(_ context.Context, req *xkmsv1.SignCSRRequest) (*xkmsv1.SignCSRResponse, error) {
	s.record(req)
	return &xkmsv1.SignCSRResponse{CertificatePem: []byte("L"), ChainPem: []byte("CH"), SerialNumber: "0a"}, nil
}

func (s *recordingKeystoreCAServer) IssueCertificate(_ context.Context, req *xkmsv1.IssueCertificateRequest) (*xkmsv1.IssueCertificateResponse, error) {
	s.record(req)
	return &xkmsv1.IssueCertificateResponse{PrivateKeyPem: []byte("K"), SerialNumber: "0b"}, nil
}

func (s *recordingKeystoreCAServer) RevokeCertificate(_ context.Context, req *xkmsv1.RevokeCertificateRequest) (*xkmsv1.RevokeCertificateResponse, error) {
	s.record(req)
	return &xkmsv1.RevokeCertificateResponse{Success: true, Message: "ok"}, nil
}

func (s *recordingKeystoreCAServer) GenerateCRL(_ context.Context, req *xkmsv1.GenerateCRLRequest) (*xkmsv1.GenerateCRLResponse, error) {
	s.record(req)
	return &xkmsv1.GenerateCRLResponse{CrlPem: []byte("CRL")}, nil
}

func (s *recordingKeystoreCAServer) IsRevoked(_ context.Context, req *xkmsv1.IsRevokedRequest) (*xkmsv1.IsRevokedResponse, error) {
	s.record(req)
	return &xkmsv1.IsRevokedResponse{Revoked: true, Reason: 1, Message: "keyCompromise"}, nil
}

func (s *recordingKeystoreCAServer) IssueEKCertificate(_ context.Context, req *xkmsv1.IssueEKCertificateRequest) (*xkmsv1.IssueEKCertificateResponse, error) {
	s.record(req)
	return &xkmsv1.IssueEKCertificateResponse{CertificateDer: []byte("EKD"), CertificatePem: []byte("EKP"), SerialNumber: "e1"}, nil
}

func (s *recordingKeystoreCAServer) IssueAKCertificate(_ context.Context, req *xkmsv1.IssueAKCertificateRequest) (*xkmsv1.IssueAKCertificateResponse, error) {
	s.record(req)
	return &xkmsv1.IssueAKCertificateResponse{CertificateDer: []byte("AKD"), SerialNumber: "a1"}, nil
}

func (s *recordingKeystoreCAServer) SignTCGCSR(_ context.Context, req *xkmsv1.SignTCGCSRRequest) (*xkmsv1.SignTCGCSRResponse, error) {
	s.record(req)
	return &xkmsv1.SignTCGCSRResponse{IakCertDer: []byte("IAK"), IdevidCertDer: []byte("IDEV")}, nil
}

func (s *recordingKeystoreCAServer) EnrollDevice(_ context.Context, req *xkmsv1.EnrollDeviceRequest) (*xkmsv1.EnrollDeviceResponse, error) {
	s.record(req)
	return &xkmsv1.EnrollDeviceResponse{
		IakCertDer: []byte("IAK"), IdevidCertDer: []byte("IDEV"),
		CredentialBlob: []byte("CB"), EncryptedSecret: []byte("ES"), PlainSecret: []byte("PS"),
	}, nil
}

func startKeystoreCAServer(t *testing.T, srv *recordingKeystoreCAServer) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	xkmsv1.RegisterKeystoreServiceServer(server, srv)
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

func caTestTransport(conn *grpc.ClientConn, at transport.AddressType) *Transport {
	return &Transport{cfg: &transport.Config{Timeout: 5 * time.Second, AddressType: at}, conn: conn}
}

func TestKeystoreCA_TCPServesEveryCAVerb(t *testing.T) {
	srv := &recordingKeystoreCAServer{}
	tr := caTestTransport(startKeystoreCAServer(t, srv), transport.AddressTypeTCP)
	ctx := context.Background()
	call := func(verb string, req interface{}) interface{} {
		t.Helper()
		resp, err := tr.Request(ctx, "quicbaas.TrustedCA", verb, req)
		if err != nil {
			t.Fatalf("%s over TCP: %v", verb, err)
		}
		return resp
	}

	bundle := call("GetCABundle", &xkmstransport.GetCABundleRequest{StoreType: "tpm2", Algorithm: "ECDSA"}).(*xkmstransport.GetCABundleResponse)
	if string(bundle.BundlePEM) != "B" || len(bundle.Certificates) != 1 || string(bundle.Certificates[0]) != "D" ||
		bundle.ContentType != "application/pem-certificate-chain" {
		t.Errorf("GetCABundle response = %+v", bundle)
	}
	if g := srv.got.(*xkmsv1.GetCABundleRequest); g.StoreType != "tpm2" || g.Algorithm != "ECDSA" {
		t.Errorf("GetCABundle request = %+v", g)
	}

	cert := call("GetCACertificate", &xkmstransport.GetCACertificateRequest{Identity: "root"}).(*xkmstransport.GetCACertificateResponse)
	if string(cert.CertificatePEM) != "C" || !cert.IsCA {
		t.Errorf("GetCACertificate response = %+v", cert)
	}
	if g := srv.got.(*xkmsv1.GetCACertificateRequest); g.Identity != "root" {
		t.Errorf("GetCACertificate request = %+v", g)
	}

	signed := call("SignCSR", &xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM"), Profile: "server", ValidityDays: 30}).(*xkmstransport.SignCSRResponse)
	if string(signed.CertificatePEM) != "L" || string(signed.ChainPEM) != "CH" || signed.SerialNumber != "0a" {
		t.Errorf("SignCSR response = %+v", signed)
	}
	if g := srv.got.(*xkmsv1.SignCSRRequest); string(g.CsrPem) != "PEM" || g.Profile != "server" || g.ValidityDays != 30 {
		t.Errorf("SignCSR request = %+v", g)
	}

	issued := call("IssueCertificate", &xkmstransport.IssueCertificateRequest{
		Profile: "client", CommonName: "n", Organization: "o", SANs: []string{"n"}, ValidityDays: 7, Algorithm: "ecdsa-p256",
	}).(*xkmstransport.IssueCertificateResponse)
	if string(issued.PrivateKeyPEM) != "K" || issued.SerialNumber != "0b" {
		t.Errorf("IssueCertificate response = %+v", issued)
	}
	if g := srv.got.(*xkmsv1.IssueCertificateRequest); g.Profile != "client" || g.CommonName != "n" || g.Organization != "o" ||
		len(g.Sans) != 1 || g.Sans[0] != "n" || g.ValidityDays != 7 || g.Algorithm != "ecdsa-p256" {
		t.Errorf("IssueCertificate request = %+v", g)
	}

	revoked := call("RevokeCertificate", &xkmstransport.RevokeCertificateRequest{SerialNumber: "0a", Reason: 1}).(*xkmstransport.RevokeCertificateResponse)
	if !revoked.Success || revoked.Message != "ok" {
		t.Errorf("RevokeCertificate response = %+v", revoked)
	}
	if g := srv.got.(*xkmsv1.RevokeCertificateRequest); g.SerialNumber != "0a" || g.Reason != 1 {
		t.Errorf("RevokeCertificate request = %+v", g)
	}

	crl := call("GenerateCRL", &xkmstransport.GenerateCRLRequest{}).(*xkmstransport.GenerateCRLResponse)
	if string(crl.CRLPEM) != "CRL" {
		t.Errorf("GenerateCRL response = %+v", crl)
	}
	if _, ok := srv.got.(*xkmsv1.GenerateCRLRequest); !ok {
		t.Errorf("GenerateCRL request = %T", srv.got)
	}

	isRev := call("IsRevoked", &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}).(*xkmstransport.IsRevokedResponse)
	if !isRev.Revoked || isRev.Reason != 1 || isRev.Message != "keyCompromise" {
		t.Errorf("IsRevoked response = %+v", isRev)
	}
	if g := srv.got.(*xkmsv1.IsRevokedRequest); g.SerialNumber != "0a" {
		t.Errorf("IsRevoked request = %+v", g)
	}
	if srv.calls != 7 {
		t.Errorf("KeystoreService calls = %d, want 7", srv.calls)
	}
}

func TestKeystoreCA_TCPServesEveryTCGVerb(t *testing.T) {
	srv := &recordingKeystoreCAServer{}
	tr := caTestTransport(startKeystoreCAServer(t, srv), transport.AddressTypeTCP)
	ctx := context.Background()
	call := func(verb string, req interface{}) interface{} {
		t.Helper()
		resp, err := tr.Request(ctx, "quicbaas.TCGCA", verb, req)
		if err != nil {
			t.Fatalf("%s over TCP: %v", verb, err)
		}
		return resp
	}

	ek := call("IssueEKCertificate", &xkmstransport.IssueEKCertificateRequest{
		CommonName: "ek", Organization: "o", EKPublicKey: []byte("EK"),
	}).(*xkmstransport.IssueEKCertificateResponse)
	if string(ek.CertificateDER) != "EKD" || string(ek.CertificatePEM) != "EKP" || ek.SerialNumber != "e1" {
		t.Errorf("IssueEKCertificate response = %+v", ek)
	}
	if g := srv.got.(*xkmsv1.IssueEKCertificateRequest); g.CommonName != "ek" || g.Organization != "o" || string(g.EkPublicKey) != "EK" {
		t.Errorf("IssueEKCertificate request = %+v", g)
	}

	ak := call("IssueAKCertificate", &xkmstransport.IssueAKCertificateRequest{
		CommonName: "ak", PublicKey: []byte("AK"),
	}).(*xkmstransport.IssueAKCertificateResponse)
	if string(ak.CertificateDER) != "AKD" || ak.SerialNumber != "a1" {
		t.Errorf("IssueAKCertificate response = %+v", ak)
	}
	if g := srv.got.(*xkmsv1.IssueAKCertificateRequest); g.CommonName != "ak" || string(g.PublicKey) != "AK" {
		t.Errorf("IssueAKCertificate request = %+v", g)
	}

	tcg := call("SignTCGCSR", &xkmstransport.SignTCGCSRRequest{
		CommonName: "d", TCGCSR: []byte("CSR"),
	}).(*xkmstransport.SignTCGCSRResponse)
	if string(tcg.IAKCertDER) != "IAK" || string(tcg.IDevIDCertDER) != "IDEV" {
		t.Errorf("SignTCGCSR response = %+v", tcg)
	}
	if g := srv.got.(*xkmsv1.SignTCGCSRRequest); g.CommonName != "d" || string(g.TcgCsr) != "CSR" {
		t.Errorf("SignTCGCSR request = %+v", g)
	}

	enr := call("EnrollDevice", &xkmstransport.EnrollDeviceRequest{
		CommonName: "d", PackedCSR: []byte("P"),
	}).(*xkmstransport.EnrollDeviceResponse)
	if string(enr.IAKCertDER) != "IAK" || string(enr.IDevIDCertDER) != "IDEV" || string(enr.CredentialBlob) != "CB" ||
		string(enr.EncryptedSecret) != "ES" || string(enr.PlainSecret) != "PS" {
		t.Errorf("EnrollDevice response = %+v", enr)
	}
	if g := srv.got.(*xkmsv1.EnrollDeviceRequest); g.CommonName != "d" || string(g.PackedCsr) != "P" {
		t.Errorf("EnrollDevice request = %+v", g)
	}
	if srv.calls != 4 {
		t.Errorf("KeystoreService calls = %d, want 4", srv.calls)
	}
}

func TestKeystoreCA_TCPRefusesFieldsItCannotCarry(t *testing.T) {
	srv := &recordingKeystoreCAServer{}
	tr := caTestTransport(startKeystoreCAServer(t, srv), transport.AddressTypeTCP)
	ctx := context.Background()
	cases := []struct {
		name, service, verb string
		req                 interface{}
	}{
		{"tenant", "quicbaas.TrustedCA", "GetCABundle", &xkmstransport.GetCABundleRequest{TenantID: "t1"}},
		{"attestation format", "quicbaas.TrustedCA", "SignCSR", &xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM"), AttestationFormat: "tpm2"}},
		{"attestation nonce", "quicbaas.TrustedCA", "SignCSR", &xkmstransport.SignCSRRequest{CSRPEM: []byte("PEM"), AttestationNonce: []byte("n")}},
		{"tcg tenant", "quicbaas.TCGCA", "IssueEKCertificate", &xkmstransport.IssueEKCertificateRequest{
			CommonName: "ek", EKPublicKey: []byte("EK"), TenantID: "t1",
		}},
	}
	for _, tc := range cases {
		_, err := tr.Request(ctx, tc.service, tc.verb, tc.req)
		if !errors.IsInvalidInput(err) {
			t.Errorf("%s: err = %v, want InvalidInput", tc.name, err)
		}
	}
	if srv.calls != 0 {
		t.Errorf("KeystoreService calls = %d, want 0 (refused fields must not reach the node)", srv.calls)
	}
}

func TestKeystoreCA_UnixKeepsTrustedCAService(t *testing.T) {
	srv := &recordingTrustedCAServer{}
	tr := caTestTransport(startTrustedCAServer(t, srv), transport.AddressTypeUnix)
	resp, err := tr.Request(context.Background(), "quicbaas.TrustedCA", "GetCABundle",
		&xkmstransport.GetCABundleRequest{StoreType: "tpm2", TenantID: "t1"})
	if err != nil {
		t.Fatalf("GetCABundle over Unix: %v", err)
	}
	if got := resp.(*xkmstransport.GetCABundleResponse); string(got.BundlePEM) != "B" {
		t.Errorf("GetCABundle response = %+v", got)
	}
	if g, ok := srv.got.(*pb.TrustedCAGetCABundleRequest); !ok || g.TenantId != "t1" {
		t.Errorf("TrustedCAService request = %#v", srv.got)
	}
}

func TestKeystoreCA_UnixRoutesTCGToTrustedCAService(t *testing.T) {
	srv := &recordingTrustedCAServer{}
	tr := caTestTransport(startTrustedCAServer(t, srv), transport.AddressTypeUnix)
	out, err := tr.Request(context.Background(), "quicbaas.TCGCA", "IssueEKCertificate",
		&xkmstransport.IssueEKCertificateRequest{CommonName: "ek", Organization: "org", EKPublicKey: []byte("EK"), TenantID: "t1"})
	if err != nil {
		t.Fatalf("TCGCA over Unix: %v", err)
	}
	got, ok := srv.got.(*pb.TrustedCAIssueEKCertificateRequest)
	if !ok {
		t.Fatalf("TrustedCAService got %#v, want a TrustedCAIssueEKCertificateRequest", srv.got)
	}
	if got.CommonName != "ek" || got.Organization != "org" || string(got.EkPublicKey) != "EK" || got.TenantId != "t1" {
		t.Errorf("TrustedCAService got %+v", got)
	}
	resp, ok := out.(*xkmstransport.IssueEKCertificateResponse)
	if !ok || resp.SerialNumber != "0e" {
		t.Errorf("IssueEKCertificate over Unix = %#v", out)
	}
}
