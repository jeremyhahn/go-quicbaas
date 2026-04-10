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
	stderrors "errors"
	"math/big"
	"testing"
	"time"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCAService implements xkmstransport.Client for testing the certificate service.
// It embeds the Client interface so we only need to implement the methods used by
// certificateService.
type mockCAService struct {
	xkmstransport.Client

	issueCertificateFunc  func(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error)
	signCSRFunc           func(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error)
	revokeCertificateFunc func(ctx context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error)
	getCACertificateFunc  func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error)
	getCABundleFunc       func(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error)
	generateCRLFunc       func(ctx context.Context, req *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error)
	isRevokedFunc         func(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error)
}

func (m *mockCAService) IssueCertificate(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
	if m.issueCertificateFunc != nil {
		return m.issueCertificateFunc(ctx, req)
	}
	return &xkmstransport.IssueCertificateResponse{
		CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----"),
		PrivateKeyPEM:  []byte("-----BEGIN EC PRIVATE KEY-----\ntest-key\n-----END EC PRIVATE KEY-----"),
		ChainPEM:       []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----"),
		SerialNumber:   "123456",
	}, nil
}

func (m *mockCAService) SignCSR(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
	if m.signCSRFunc != nil {
		return m.signCSRFunc(ctx, req)
	}
	return &xkmstransport.SignCSRResponse{
		CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nsigned-cert\n-----END CERTIFICATE-----"),
		ChainPEM:       []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----"),
		SerialNumber:   "789012",
	}, nil
}

func (m *mockCAService) RevokeCertificate(ctx context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
	if m.revokeCertificateFunc != nil {
		return m.revokeCertificateFunc(ctx, req)
	}
	return &xkmstransport.RevokeCertificateResponse{
		Success: true,
	}, nil
}

func (m *mockCAService) GetCACertificate(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
	if m.getCACertificateFunc != nil {
		return m.getCACertificateFunc(ctx, req)
	}
	return &xkmstransport.GetCACertificateResponse{
		CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----"),
		Subject:        "CN=Test CA,O=Test Org",
	}, nil
}

func (m *mockCAService) GetCABundle(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
	if m.getCABundleFunc != nil {
		return m.getCABundleFunc(ctx, req)
	}
	return &xkmstransport.GetCABundleResponse{
		BundlePEM: []byte("-----BEGIN CERTIFICATE-----\nca-bundle\n-----END CERTIFICATE-----"),
	}, nil
}

func (m *mockCAService) GenerateCRL(ctx context.Context, req *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
	if m.generateCRLFunc != nil {
		return m.generateCRLFunc(ctx, req)
	}
	return &xkmstransport.GenerateCRLResponse{
		CRLPEM: []byte("-----BEGIN X509 CRL-----\ncrl-data\n-----END X509 CRL-----"),
	}, nil
}

func (m *mockCAService) IsRevoked(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
	if m.isRevokedFunc != nil {
		return m.isRevokedFunc(ctx, req)
	}
	return &xkmstransport.IsRevokedResponse{
		Revoked: false,
	}, nil
}

// newTestMockCAService creates a mockCAService with a real CA certificate
// that can be used for VerifyCertificate tests.
func newTestMockCAService(t *testing.T) (*mockCAService, *ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA certificate: %v", err)
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("failed to parse CA certificate: %v", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})

	mock := &mockCAService{
		getCACertificateFunc: func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
			return &xkmstransport.GetCACertificateResponse{
				CertificatePEM: caCertPEM,
				Subject:        "CN=Test CA,O=Test Org",
			}, nil
		},
		getCABundleFunc: func(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
			return &xkmstransport.GetCABundleResponse{
				BundlePEM: caCertPEM,
			}, nil
		},
		isRevokedFunc: func(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
			return &xkmstransport.IsRevokedResponse{Revoked: false}, nil
		},
	}

	return mock, caKey, caCert
}

// Test NewCertificateService
func TestNewCertificateService(t *testing.T) {
	t.Run("with valid ca service", func(t *testing.T) {
		mock := &mockCAService{}
		svc := NewCertificateService(mock)
		if svc == nil {
			t.Fatal("expected non-nil service")
		}
	})

	t.Run("with nil ca service", func(t *testing.T) {
		svc := NewCertificateService(nil)
		if svc != nil {
			t.Error("expected nil service for nil ca service")
		}
	})
}

// Test IssueCertificate
func TestCertificateService_IssueCertificate(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)
	ctx := context.Background()

	t.Run("success with client usage", func(t *testing.T) {
		resp, err := svc.IssueCertificate(ctx, &IssueCertificateRequest{
			CommonName:   "test-client",
			Organization: "Test Org",
			ValidityDays: 30,
			Usage:        "client",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CertificatePEM == "" {
			t.Error("expected certificate PEM")
		}
		if resp.SerialNumber == "" {
			t.Error("expected serial number")
		}
	})

	t.Run("success with server usage", func(t *testing.T) {
		resp, err := svc.IssueCertificate(ctx, &IssueCertificateRequest{
			CommonName:   "test-server",
			Organization: "Test Org",
			ValidityDays: 30,
			Usage:        "server",
			DNSNames:     []string{"localhost"},
			IPAddresses:  []string{"127.0.0.1"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CertificatePEM == "" {
			t.Error("expected certificate PEM")
		}
	})

	t.Run("success with both usage", func(t *testing.T) {
		resp, err := svc.IssueCertificate(ctx, &IssueCertificateRequest{
			CommonName:   "test-both",
			Organization: "Test Org",
			ValidityDays: 30,
			Usage:        "both",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CertificatePEM == "" {
			t.Error("expected certificate PEM")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.IssueCertificate(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("missing common name", func(t *testing.T) {
		_, err := svc.IssueCertificate(ctx, &IssueCertificateRequest{
			Organization: "Test Org",
		})
		if err == nil {
			t.Error("expected error for missing common name")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			issueCertificateFunc: func(ctx context.Context, req *xkmstransport.IssueCertificateRequest) (*xkmstransport.IssueCertificateResponse, error) {
				return nil, stderrors.New("ca service unavailable")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.IssueCertificate(ctx, &IssueCertificateRequest{
			CommonName: "test",
		})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

// Test SignCSR
func TestCertificateService_SignCSR(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		resp, err := svc.SignCSR(ctx, &SignCSRRequest{
			CSRPEM:       "-----BEGIN CERTIFICATE REQUEST-----\ntest-csr\n-----END CERTIFICATE REQUEST-----",
			ValidityDays: 30,
			Usage:        "client",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CertificatePEM == "" {
			t.Error("expected certificate PEM")
		}
		if resp.SerialNumber == "" {
			t.Error("expected serial number")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.SignCSR(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty CSR", func(t *testing.T) {
		_, err := svc.SignCSR(ctx, &SignCSRRequest{
			CSRPEM: "",
		})
		if err == nil {
			t.Error("expected error for empty CSR")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			signCSRFunc: func(ctx context.Context, req *xkmstransport.SignCSRRequest) (*xkmstransport.SignCSRResponse, error) {
				return nil, stderrors.New("invalid CSR")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.SignCSR(ctx, &SignCSRRequest{
			CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
		})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

// Test RevokeCertificate
func TestCertificateService_RevokeCertificate(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		resp, err := svc.RevokeCertificate(ctx, &RevokeCertificateRequest{
			SerialNumber: "12345",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Success {
			t.Error("expected success")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.RevokeCertificate(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty serial number", func(t *testing.T) {
		_, err := svc.RevokeCertificate(ctx, &RevokeCertificateRequest{
			SerialNumber: "",
		})
		if err == nil {
			t.Error("expected error for empty serial number")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			revokeCertificateFunc: func(ctx context.Context, req *xkmstransport.RevokeCertificateRequest) (*xkmstransport.RevokeCertificateResponse, error) {
				return nil, stderrors.New("certificate already revoked")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.RevokeCertificate(ctx, &RevokeCertificateRequest{
			SerialNumber: "12345",
		})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

// Test VerifyCertificate
func TestCertificateService_VerifyCertificate(t *testing.T) {
	caServiceMock, caKey, caCert := newTestMockCAService(t)
	svc := NewCertificateService(caServiceMock)
	ctx := context.Background()

	// Generate a test certificate signed by the mock CA
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(123),
		Subject: pkix.Name{
			CommonName: "test",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

	t.Run("valid certificate", func(t *testing.T) {
		resp, err := svc.VerifyCertificate(ctx, &VerifyCertificateRequest{
			CertificatePEM: certPEM,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Valid {
			t.Errorf("expected valid certificate, got error: %s", resp.ErrorMessage)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.VerifyCertificate(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty certificate PEM", func(t *testing.T) {
		_, err := svc.VerifyCertificate(ctx, &VerifyCertificateRequest{
			CertificatePEM: "",
		})
		if err == nil {
			t.Error("expected error for empty certificate PEM")
		}
	})

	t.Run("invalid PEM format", func(t *testing.T) {
		resp, err := svc.VerifyCertificate(ctx, &VerifyCertificateRequest{
			CertificatePEM: "not a valid PEM",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Valid {
			t.Error("expected invalid result for bad PEM")
		}
		if resp.ErrorMessage == "" {
			t.Error("expected error message")
		}
	})

	t.Run("wrong PEM type", func(t *testing.T) {
		keyDER, _ := x509.MarshalECPrivateKey(key)
		keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))

		resp, err := svc.VerifyCertificate(ctx, &VerifyCertificateRequest{
			CertificatePEM: keyPEM,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Valid {
			t.Error("expected invalid result for wrong PEM type")
		}
	})

	t.Run("revoked certificate", func(t *testing.T) {
		revokedMock, revokedCAKey, revokedCACert := newTestMockCAService(t)
		revokedMock.isRevokedFunc = func(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
			return &xkmstransport.IsRevokedResponse{Revoked: true, Reason: 1, Message: "key compromise"}, nil
		}
		revokedSvc := NewCertificateService(revokedMock)

		// Generate a cert signed by the revoked mock's CA
		revokedCertDER, _ := x509.CreateCertificate(rand.Reader, template, revokedCACert, &key.PublicKey, revokedCAKey)
		revokedCertPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: revokedCertDER}))

		resp, err := revokedSvc.VerifyCertificate(ctx, &VerifyCertificateRequest{
			CertificatePEM: revokedCertPEM,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Valid {
			t.Error("expected invalid result from revoked certificate")
		}
	})
}

// Test GetCACertificate
func TestCertificateService_GetCACertificate(t *testing.T) {
	caServiceMock, _, _ := newTestMockCAService(t)
	svc := NewCertificateService(caServiceMock)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		resp, err := svc.GetCACertificate(ctx, &GetCACertificateRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CACertificatePEM == "" {
			t.Error("expected CA certificate PEM")
		}
		if resp.Identity == "" {
			t.Error("expected identity")
		}
	})

	t.Run("ca service GetCACertificate error", func(t *testing.T) {
		errMock := &mockCAService{
			getCACertificateFunc: func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
				return nil, stderrors.New("CA not available")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.GetCACertificate(ctx, &GetCACertificateRequest{})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})

	t.Run("ca service GetCABundle error", func(t *testing.T) {
		errMock := &mockCAService{
			getCACertificateFunc: func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
				return &xkmstransport.GetCACertificateResponse{
					CertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----"),
					Subject:        "CN=Test CA",
				}, nil
			},
			getCABundleFunc: func(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
				return nil, stderrors.New("bundle not available")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.GetCACertificate(ctx, &GetCACertificateRequest{})
		if err == nil {
			t.Error("expected error from ca service bundle")
		}
	})
}

// Test GetTLSConfig
func TestCertificateService_GetTLSConfig(t *testing.T) {
	caServiceMock, _, _ := newTestMockCAService(t)
	svc := NewCertificateService(caServiceMock)
	ctx := context.Background()

	t.Run("success with nil request", func(t *testing.T) {
		resp, err := svc.GetTLSConfig(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Config == nil {
			t.Error("expected TLS config")
		}
	})

	t.Run("success with TLS 1.3", func(t *testing.T) {
		resp, err := svc.GetTLSConfig(ctx, &GetTLSConfigRequest{
			ServerName: "test.example.com",
			MinVersion: "1.3",
			IsServer:   true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Config == nil {
			t.Error("expected TLS config")
		}
	})

	t.Run("success with TLS 1.2", func(t *testing.T) {
		resp, err := svc.GetTLSConfig(ctx, &GetTLSConfigRequest{
			MinVersion:        "1.2",
			RequireClientCert: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Config == nil {
			t.Error("expected TLS config")
		}
	})

	t.Run("success with unknown version", func(t *testing.T) {
		resp, err := svc.GetTLSConfig(ctx, &GetTLSConfigRequest{
			MinVersion: "unknown",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Config == nil {
			t.Error("expected TLS config")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			getCACertificateFunc: func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
				return nil, stderrors.New("CA not available")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.GetTLSConfig(ctx, &GetTLSConfigRequest{})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

// Test CAService accessor
func TestCertificateService_CAService(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)

	got := svc.CAService()
	if got != mock {
		t.Error("expected same ca service instance")
	}
}

// Test IsInitialized
func TestCertificateService_IsInitialized(t *testing.T) {
	t.Run("initialized ca service", func(t *testing.T) {
		mock := &mockCAService{}
		svc := NewCertificateService(mock)
		// IsInitialized calls GetCACertificate, default mock returns success
		if !svc.IsInitialized() {
			t.Error("expected initialized")
		}
	})

	t.Run("uninitialized ca service", func(t *testing.T) {
		errMock := &mockCAService{
			getCACertificateFunc: func(ctx context.Context, req *xkmstransport.GetCACertificateRequest) (*xkmstransport.GetCACertificateResponse, error) {
				return nil, stderrors.New("not initialized")
			},
		}
		svc := NewCertificateService(errMock)
		if svc.IsInitialized() {
			t.Error("expected not initialized")
		}
	})
}

// Test GetCABundle
func TestCertificateService_GetCABundle(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)
	ctx := context.Background()

	t.Run("success with nil request", func(t *testing.T) {
		resp, err := svc.GetCABundle(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.BundlePEM == "" {
			t.Error("expected bundle PEM")
		}
	})

	t.Run("success with request", func(t *testing.T) {
		resp, err := svc.GetCABundle(ctx, &GetCABundleRequest{
			StoreType: "software",
			Algorithm: "ECDSA",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.BundlePEM == "" {
			t.Error("expected bundle PEM")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			getCABundleFunc: func(ctx context.Context, req *xkmstransport.GetCABundleRequest) (*xkmstransport.GetCABundleResponse, error) {
				return nil, stderrors.New("bundle not available")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.GetCABundle(ctx, &GetCABundleRequest{})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

// Test GenerateCRL
func TestCertificateService_GenerateCRL(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		resp, err := svc.GenerateCRL(ctx, &GenerateCRLRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CRLPEM == "" {
			t.Error("expected CRL PEM")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			generateCRLFunc: func(ctx context.Context, req *xkmstransport.GenerateCRLRequest) (*xkmstransport.GenerateCRLResponse, error) {
				return nil, stderrors.New("CRL generation failed")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.GenerateCRL(ctx, &GenerateCRLRequest{})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

// Test IsRevoked
func TestCertificateService_IsRevoked(t *testing.T) {
	mock := &mockCAService{}
	svc := NewCertificateService(mock)
	ctx := context.Background()

	t.Run("success not revoked", func(t *testing.T) {
		resp, err := svc.IsRevoked(ctx, &IsRevokedRequest{
			SerialNumber: "12345",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Revoked {
			t.Error("expected not revoked")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.IsRevoked(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty serial number", func(t *testing.T) {
		_, err := svc.IsRevoked(ctx, &IsRevokedRequest{
			SerialNumber: "",
		})
		if err == nil {
			t.Error("expected error for empty serial number")
		}
	})

	t.Run("ca service error", func(t *testing.T) {
		errMock := &mockCAService{
			isRevokedFunc: func(ctx context.Context, req *xkmstransport.IsRevokedRequest) (*xkmstransport.IsRevokedResponse, error) {
				return nil, stderrors.New("revocation check failed")
			},
		}
		errSvc := NewCertificateService(errMock)

		_, err := errSvc.IsRevoked(ctx, &IsRevokedRequest{
			SerialNumber: "12345",
		})
		if err == nil {
			t.Error("expected error from ca service")
		}
	})
}

func TestCertificateService_SignCSR_ServerUsage(t *testing.T) {
	caService := &mockCAService{}
	svc := NewCertificateService(caService)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "server.example.com",
			Organization: []string{"Test Org"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	require.NoError(t, err)
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	req := &SignCSRRequest{
		CSRPEM:       csrPEM,
		ValidityDays: 365,
		Usage:        "server",
	}

	resp, err := svc.SignCSR(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.CertificatePEM)
	assert.NotEmpty(t, resp.SerialNumber)
}

func TestCertificateService_SignCSR_BothUsage(t *testing.T) {
	caService := &mockCAService{}
	svc := NewCertificateService(caService)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "dual.example.com",
			Organization: []string{"Test Org"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	require.NoError(t, err)
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	req := &SignCSRRequest{
		CSRPEM:       csrPEM,
		ValidityDays: 90,
		Usage:        "both",
	}

	resp, err := svc.SignCSR(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.CertificatePEM)
	assert.NotEmpty(t, resp.SerialNumber)
}

func TestCertificateService_SignCSR_NoUsage(t *testing.T) {
	caService := &mockCAService{}
	svc := NewCertificateService(caService)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "generic.example.com",
			Organization: []string{"Test Org"},
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	require.NoError(t, err)
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	req := &SignCSRRequest{
		CSRPEM:       csrPEM,
		ValidityDays: 30,
		Usage:        "", // No usage specified
	}

	resp, err := svc.SignCSR(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.CertificatePEM)
}

func TestCertificateService_VerifyCertificate_InvalidDERInValidPEM(t *testing.T) {
	caService := newCovMockCAService(t)
	svc := NewCertificateService(caService)
	require.NotNil(t, svc)

	// Craft a PEM block with CERTIFICATE type but garbage DER bytes
	invalidPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("this is not valid DER data"),
	}))

	resp, err := svc.VerifyCertificate(context.Background(), &VerifyCertificateRequest{
		CertificatePEM: invalidPEM,
	})
	require.NoError(t, err) // Returns nil error, error is in response
	require.NotNil(t, resp)
	assert.False(t, resp.Valid)
	assert.NotEmpty(t, resp.ErrorMessage)
}

func TestCertificateService_IssueCertificate_RSAKeyAlgorithm(t *testing.T) {
	caService := newCovMockCAService(t)
	svc := NewCertificateService(caService)
	require.NotNil(t, svc)

	resp, err := svc.IssueCertificate(context.Background(), &IssueCertificateRequest{
		CommonName:   "test.example.com",
		KeyAlgorithm: "rsa",
		ValidityDays: 365,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.CertificatePEM)
}
