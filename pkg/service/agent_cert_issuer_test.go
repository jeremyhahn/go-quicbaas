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

package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// stubCAService implements the minimum xkmstransport.CAService surface
// SDKAgentCertIssuer actually exercises (IssueCertificate + GetCABundle).
// Other methods satisfy the interface contract but are not expected to be
// invoked; they panic to surface unintended calls in tests.
type stubCAService struct {
	issueResp *xkmstransport.IssueCertificateResponse
	issueErr  error
	bundleErr error
	bundle    []byte
}

func (s *stubCAService) IssueCertificate(_ context.Context, _ *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	return s.issueResp, s.issueErr
}

func (s *stubCAService) GetCABundle(_ context.Context, _ *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	if s.bundleErr != nil {
		return nil, s.bundleErr
	}
	return &xkmstransport.GetCABundleResponse{BundlePEM: s.bundle}, nil
}

func (s *stubCAService) GetCACertificate(_ context.Context, _ *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	panic("unexpected GetCACertificate call")
}

func (s *stubCAService) SignCSR(_ context.Context, _ *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	panic("unexpected SignCSR call")
}

func (s *stubCAService) RevokeCertificate(_ context.Context, _ *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	panic("unexpected RevokeCertificate call")
}

func (s *stubCAService) GenerateCRL(_ context.Context, _ *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	panic("unexpected GenerateCRL call")
}

func (s *stubCAService) IsRevoked(_ context.Context, _ *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	panic("unexpected IsRevoked call")
}

// newTestCertPEMs returns a self-signed leaf certificate plus its private
// key and a trivial single-cert "CA bundle" built from the same cert.
// Sufficient for exercising the issuer's PEM-handling code paths without
// requiring a real CA infrastructure.
func newTestCertPEMs(t *testing.T) (certPEM, keyPEM, bundlePEM []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "agent-test-host"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	bundlePEM = append([]byte(nil), certPEM...)
	return
}

// TestSDKAgentCertIssuer_IssueAgentCertificate_Happy verifies the issuer
// drives the CAService correctly and returns a fully populated result.
func TestSDKAgentCertIssuer_IssueAgentCertificate_Happy(t *testing.T) {
	certPEM, keyPEM, bundlePEM := newTestCertPEMs(t)

	stub := &stubCAService{
		issueResp: &xkmstransport.IssueCertificateResponse{
			CertificatePEM: certPEM,
			PrivateKeyPEM:  keyPEM,
			ChainPEM:       bundlePEM,
			SerialNumber:   "2a",
		},
		bundle: bundlePEM,
	}

	issuer, err := NewSDKAgentCertIssuer(stub)
	if err != nil {
		t.Fatalf("NewSDKAgentCertIssuer: %v", err)
	}

	req := &AgentCertificateRequest{
		TenantID: "tenant-a",
		Hostname: "agent-1.lan",
		Subject: AgentCertificateSubject{
			OrganizationalUnit: "agents",
		},
		ValidityDays: 30,
	}

	result, err := issuer.IssueAgentCertificate(context.Background(), req)
	if err != nil {
		t.Fatalf("IssueAgentCertificate: %v", err)
	}
	defer result.PrivateKey.Destroy()

	if len(result.CertPEM) == 0 {
		t.Fatal("empty CertPEM")
	}
	if len(result.CAPEM) == 0 {
		t.Fatal("empty CAPEM")
	}
	if result.ParsedCert == nil {
		t.Fatal("nil ParsedCert")
	}
	if result.ParsedCert.Subject.CommonName != "agent-test-host" {
		t.Fatalf("unexpected CN: %q", result.ParsedCert.Subject.CommonName)
	}

	// The response's private-key slice must be zeroed after the issuer
	// has transferred it to the secure buffer (defense-in-depth against
	// the key lingering on the heap).
	for _, b := range stub.issueResp.PrivateKeyPEM {
		if b != 0 {
			t.Fatalf("response private key not wiped")
		}
	}
}

// TestSDKAgentCertIssuer_IssueAgentCertificate_Errors exercises the error
// branches: nil client at construction, invalid request, CA error, and
// empty PEM response.
func TestSDKAgentCertIssuer_IssueAgentCertificate_Errors(t *testing.T) {
	t.Run("nil ca returns ErrAgentCertIssuerNilCA", func(t *testing.T) {
		if _, err := NewSDKAgentCertIssuer(nil); !errors.Is(err, ErrAgentCertIssuerNilCA) {
			t.Fatalf("expected ErrAgentCertIssuerNilCA, got %v", err)
		}
	})

	t.Run("missing tenant id returns AgentCertRequestFieldError", func(t *testing.T) {
		issuer, _ := NewSDKAgentCertIssuer(&stubCAService{})
		_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
			Hostname: "agent-x",
		})
		var fieldErr *AgentCertRequestFieldError
		if !errors.As(err, &fieldErr) || fieldErr.Field != "tenant_id" {
			t.Fatalf("expected tenant_id field error, got %v", err)
		}
	})

	t.Run("ca returns error wrapped in AgentCertIssueError", func(t *testing.T) {
		stub := &stubCAService{issueErr: errors.New("ca down")}
		issuer, _ := NewSDKAgentCertIssuer(stub)
		_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
			TenantID: "t", Hostname: "h",
		})
		var issErr *AgentCertIssueError
		if !errors.As(err, &issErr) {
			t.Fatalf("expected AgentCertIssueError, got %v", err)
		}
		if issErr.Op != "issue agent certificate" {
			t.Fatalf("unexpected op %q", issErr.Op)
		}
	})

	t.Run("empty certificate pem is rejected", func(t *testing.T) {
		stub := &stubCAService{
			issueResp: &xkmstransport.IssueCertificateResponse{PrivateKeyPEM: []byte("k")},
		}
		issuer, _ := NewSDKAgentCertIssuer(stub)
		_, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
			TenantID: "t", Hostname: "h",
		})
		if err == nil {
			t.Fatal("expected error for empty cert pem")
		}
	})

	t.Run("empty bundle falls back to ChainPEM", func(t *testing.T) {
		certPEM, keyPEM, bundlePEM := newTestCertPEMs(t)
		stub := &stubCAService{
			issueResp: &xkmstransport.IssueCertificateResponse{
				CertificatePEM: certPEM,
				PrivateKeyPEM:  keyPEM,
				ChainPEM:       bundlePEM,
				SerialNumber:   "1",
			},
			// bundleErr forces fetchCABundle to fail, so the fallback
			// must use ChainPEM from the issue response.
			bundleErr: errors.New("no bundle"),
		}
		issuer, _ := NewSDKAgentCertIssuer(stub)
		result, err := issuer.IssueAgentCertificate(context.Background(), &AgentCertificateRequest{
			TenantID: "t", Hostname: "h",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer result.PrivateKey.Destroy()
		if len(result.CAPEM) == 0 {
			t.Fatal("expected CAPEM from ChainPEM fallback")
		}
	})
}
