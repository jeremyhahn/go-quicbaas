//go:build quic

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

package quic

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Certificate Service
// =============================================================================

type mockCertificateService struct {
	issueCertificateFn  func(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error)
	signCSRFn           func(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error)
	revokeCertificateFn func(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error)
	verifyCertificateFn func(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error)
	getCACertificateFn  func(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error)
	getTLSConfigFn      func(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error)
	isInitialized       bool

	CrossSignFunc           func(ctx context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error)
	ChainFunc               func(ctx context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error)
	IssueDeviceIdentityFunc func(ctx context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error)
}

func (m *mockCertificateService) IssueCertificate(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
	if m.issueCertificateFn != nil {
		return m.issueCertificateFn(ctx, req)
	}
	return &service.IssueCertificateResponse{
		CertificatePEM:   "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		PrivateKeyPEM:    "-----BEGIN EC PRIVATE KEY-----\nMHQ...\n-----END EC PRIVATE KEY-----",
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		ChainPEM:         "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		SerialNumber:     "1234567890",
		NotBefore:        time.Now(),
		NotAfter:         time.Now().Add(365 * 24 * time.Hour),
	}, nil
}

func (m *mockCertificateService) SignCSR(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
	if m.signCSRFn != nil {
		return m.signCSRFn(ctx, req)
	}
	return &service.SignCSRResponse{
		CertificatePEM:   "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		ChainPEM:         "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		SerialNumber:     "9876543210",
		NotBefore:        time.Now(),
		NotAfter:         time.Now().Add(365 * 24 * time.Hour),
	}, nil
}

func (m *mockCertificateService) RevokeCertificate(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
	if m.revokeCertificateFn != nil {
		return m.revokeCertificateFn(ctx, req)
	}
	return &service.RevokeCertificateResponse{
		Success: true,
	}, nil
}

func (m *mockCertificateService) VerifyCertificate(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
	if m.verifyCertificateFn != nil {
		return m.verifyCertificateFn(ctx, req)
	}
	return &service.VerifyCertificateResponse{
		Valid:        true,
		ErrorMessage: "",
	}, nil
}

func (m *mockCertificateService) GetCACertificate(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
	if m.getCACertificateFn != nil {
		return m.getCACertificateFn(ctx, req)
	}
	return &service.GetCACertificateResponse{
		CACertificatePEM: "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		CABundlePEM:      "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		Identity:         "test-ca",
	}, nil
}

func (m *mockCertificateService) GetTLSConfig(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
	if m.getTLSConfigFn != nil {
		return m.getTLSConfigFn(ctx, req)
	}
	return &service.GetTLSConfigResponse{
		Config: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	}, nil
}

func (m *mockCertificateService) IsInitialized() bool {
	return m.isInitialized
}

func (m *mockCertificateService) CAService() xkmstransport.Client {
	return nil
}

func (m *mockCertificateService) GetCABundle(ctx context.Context, req *service.GetCABundleRequest) (*service.GetCABundleResponse, error) {
	return &service.GetCABundleResponse{}, nil
}

func (m *mockCertificateService) GenerateCRL(ctx context.Context, req *service.GenerateCRLRequest) (*service.GenerateCRLResponse, error) {
	return &service.GenerateCRLResponse{}, nil
}

func (m *mockCertificateService) IsRevoked(ctx context.Context, req *service.IsRevokedRequest) (*service.IsRevokedResponse, error) {
	return &service.IsRevokedResponse{}, nil
}

func (m *mockCertificateService) CAInfo(ctx context.Context, req *service.CAInfoRequest) (*service.CAInfoResponse, error) {
	return &service.CAInfoResponse{}, nil
}

func (m *mockCertificateService) ListCertificates(ctx context.Context, req *service.ListCertificatesRequest) (*service.ListCertificatesResponse, error) {
	return &service.ListCertificatesResponse{}, nil
}

// =============================================================================
// Mock Certificate-Enabled Service
// =============================================================================

type mockCertificateEnabledService struct {
	mockService
	certSvc service.CertificateService
}

func (m *mockCertificateEnabledService) Certificate() service.CertificateService {
	return m.certSvc
}

// =============================================================================
// Helper Functions
// =============================================================================

func createCertificateTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-certificate-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createCertificateTestHandler(certSvc service.CertificateService) *Handler {
	svc := &mockCertificateEnabledService{
		certSvc: certSvc,
	}
	return NewHandler(svc, nil, nil, nil, createCertificateTestLogger())
}

// =============================================================================
// handleCertIssue Tests
// =============================================================================

func TestHandleCertIssue_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		issueCertificateFn: func(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
			return &service.IssueCertificateResponse{
				CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----",
				PrivateKeyPEM:    "-----BEGIN EC PRIVATE KEY-----\ntest-key\n-----END EC PRIVATE KEY-----",
				CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
				ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
				SerialNumber:     "123456",
				NotBefore:        time.Now(),
				NotAfter:         time.Now().Add(24 * time.Hour),
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, err := EncodePayload(&service.IssueCertificateRequest{
		CommonName:   "test.example.com",
		Organization: "Test Org",
		ValidityDays: 30,
		Usage:        "server",
		DNSNames:     []string{"test.example.com", "localhost"},
		IPAddresses:  []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCertIssue,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertIssue_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	payload, _ := EncodePayload(&service.IssueCertificateRequest{
		CommonName: "test.example.com",
	})

	req := &Request{
		Opcode:  OpCertIssue,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil certificate service")
	}
}

func TestHandleCertIssue_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	req := &Request{
		Opcode:  OpCertIssue,
		Payload: []byte("invalid json payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCertIssue_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		issueCertificateFn: func(ctx context.Context, req *service.IssueCertificateRequest) (*service.IssueCertificateResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "CertificateService.IssueCertificate",
				Err:  service.ErrCertMissingCommonName,
			}
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.IssueCertificateRequest{
		CommonName: "",
	})

	req := &Request{
		Opcode:  OpCertIssue,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleCertSignCSR Tests
// =============================================================================

func TestHandleCertSignCSR_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		signCSRFn: func(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
			return &service.SignCSRResponse{
				CertificatePEM:   "-----BEGIN CERTIFICATE-----\nsigned-cert\n-----END CERTIFICATE-----",
				CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----",
				ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
				SerialNumber:     "654321",
				NotBefore:        time.Now(),
				NotAfter:         time.Now().Add(365 * 24 * time.Hour),
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, err := EncodePayload(&service.SignCSRRequest{
		CSRPEM:       "-----BEGIN CERTIFICATE REQUEST-----\ntest-csr\n-----END CERTIFICATE REQUEST-----",
		ValidityDays: 365,
		Usage:        "client",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCertSignCSR,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertSignCSR_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	payload, _ := EncodePayload(&service.SignCSRRequest{
		CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
	})

	req := &Request{
		Opcode:  OpCertSignCSR,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil certificate service")
	}
}

func TestHandleCertSignCSR_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	req := &Request{
		Opcode:  OpCertSignCSR,
		Payload: []byte("not valid json at all"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCertSignCSR_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		signCSRFn: func(ctx context.Context, req *service.SignCSRRequest) (*service.SignCSRResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "CertificateService.SignCSR",
				Err:  service.ErrCertMissingCSR,
			}
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.SignCSRRequest{
		CSRPEM: "invalid-csr-data",
	})

	req := &Request{
		Opcode:  OpCertSignCSR,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleCertRevoke Tests
// =============================================================================

func TestHandleCertRevoke_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		revokeCertificateFn: func(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
			return &service.RevokeCertificateResponse{
				Success: true,
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, err := EncodePayload(&service.RevokeCertificateRequest{
		SerialNumber: "1234567890",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCertRevoke,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertRevoke_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	payload, _ := EncodePayload(&service.RevokeCertificateRequest{
		SerialNumber: "123456",
	})

	req := &Request{
		Opcode:  OpCertRevoke,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil certificate service")
	}
}

func TestHandleCertRevoke_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	req := &Request{
		Opcode:  OpCertRevoke,
		Payload: []byte("garbage revoke request"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCertRevoke_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		revokeCertificateFn: func(ctx context.Context, req *service.RevokeCertificateRequest) (*service.RevokeCertificateResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "CertificateService.RevokeCertificate",
				Err:  service.ErrCertMissingSerial,
			}
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.RevokeCertificateRequest{
		SerialNumber: "nonexistent",
	})

	req := &Request{
		Opcode:  OpCertRevoke,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleCertVerify Tests
// =============================================================================

func TestHandleCertVerify_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		verifyCertificateFn: func(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
			return &service.VerifyCertificateResponse{
				Valid:        true,
				ErrorMessage: "",
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, err := EncodePayload(&service.VerifyCertificateRequest{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\nvalid-cert\n-----END CERTIFICATE-----",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCertVerify,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertVerify_Invalid(t *testing.T) {
	certSvc := &mockCertificateService{
		verifyCertificateFn: func(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
			return &service.VerifyCertificateResponse{
				Valid:        false,
				ErrorMessage: "certificate has expired",
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.VerifyCertificateRequest{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\nexpired-cert\n-----END CERTIFICATE-----",
	})

	req := &Request{
		Opcode:  OpCertVerify,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK even for invalid cert (validation result in payload), got %d", resp.Status)
	}
}

func TestHandleCertVerify_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	payload, _ := EncodePayload(&service.VerifyCertificateRequest{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
	})

	req := &Request{
		Opcode:  OpCertVerify,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil certificate service")
	}
}

func TestHandleCertVerify_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	req := &Request{
		Opcode:  OpCertVerify,
		Payload: []byte("broken verify payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCertVerify_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		verifyCertificateFn: func(ctx context.Context, req *service.VerifyCertificateRequest) (*service.VerifyCertificateResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "CertificateService.VerifyCertificate",
				Err:  service.ErrCertNilRequest,
			}
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.VerifyCertificateRequest{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
	})

	req := &Request{
		Opcode:  OpCertVerify,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleCertGetCA Tests
// =============================================================================

func TestHandleCertGetCA_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		getCACertificateFn: func(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
			return &service.GetCACertificateResponse{
				CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca-cert-data\n-----END CERTIFICATE-----",
				CABundlePEM:      "-----BEGIN CERTIFICATE-----\nbundle-data\n-----END CERTIFICATE-----",
				Identity:         "quicbaas-ca",
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, err := EncodePayload(&service.GetCACertificateRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCertGetCA,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertGetCA_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	payload, _ := EncodePayload(&service.GetCACertificateRequest{})

	req := &Request{
		Opcode:  OpCertGetCA,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil certificate service")
	}
}

func TestHandleCertGetCA_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	req := &Request{
		Opcode:  OpCertGetCA,
		Payload: []byte("invalid ca request data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCertGetCA_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		getCACertificateFn: func(ctx context.Context, req *service.GetCACertificateRequest) (*service.GetCACertificateResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "CertificateService.GetCACertificate",
				Err:  service.ErrCertMissingPEM,
			}
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.GetCACertificateRequest{})

	req := &Request{
		Opcode:  OpCertGetCA,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleCertGetTLSConfig Tests
// =============================================================================

func TestHandleCertGetTLSConfig_Success(t *testing.T) {
	certSvc := &mockCertificateService{
		getTLSConfigFn: func(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
			return &service.GetTLSConfigResponse{
				Config: &tls.Config{
					MinVersion: tls.VersionTLS13,
				},
			}, nil
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, err := EncodePayload(&service.GetTLSConfigRequest{
		ServerName:        "test.example.com",
		RequireClientCert: true,
		IsServer:          true,
		MinVersion:        "1.3",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCertGetTLSConfig,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertGetTLSConfig_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	payload, _ := EncodePayload(&service.GetTLSConfigRequest{})

	req := &Request{
		Opcode:  OpCertGetTLSConfig,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil certificate service")
	}
}

func TestHandleCertGetTLSConfig_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	req := &Request{
		Opcode:  OpCertGetTLSConfig,
		Payload: []byte("malformed tls config request"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCertGetTLSConfig_ServiceError(t *testing.T) {
	certSvc := &mockCertificateService{
		getTLSConfigFn: func(ctx context.Context, req *service.GetTLSConfigRequest) (*service.GetTLSConfigResponse, error) {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "CertificateService.GetTLSConfig",
				Err:  service.ErrCertNilRequest,
			}
		},
		isInitialized: true,
	}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.GetTLSConfigRequest{
		MinVersion: "1.2",
	})

	req := &Request{
		Opcode:  OpCertGetTLSConfig,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Table-Driven Tests for All Certificate Operations
// =============================================================================

func TestHandleCertificateOperations_AllOpcodes(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"CertIssue", OpCertIssue, &service.IssueCertificateRequest{CommonName: "test.example.com", Usage: "server"}},
		{"CertSignCSR", OpCertSignCSR, &service.SignCSRRequest{CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----"}},
		{"CertRevoke", OpCertRevoke, &service.RevokeCertificateRequest{SerialNumber: "123456"}},
		{"CertVerify", OpCertVerify, &service.VerifyCertificateRequest{CertificatePEM: "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"}},
		{"CertGetCA", OpCertGetCA, &service.GetCACertificateRequest{}},
		{"CertGetTLSConfig", OpCertGetTLSConfig, &service.GetTLSConfigRequest{MinVersion: "1.2"}},
	}

	if len(tests) != 6 {
		t.Errorf("expected 6 certificate operations, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for %s, got %d: %s", tt.name, resp.Status, string(resp.Payload))
			}
		})
	}
}

// =============================================================================
// Table-Driven Tests for Error Cases
// =============================================================================

func TestHandleCertificateOperations_NilService(t *testing.T) {
	handler := createCertificateTestHandler(nil)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"CertIssue_NilService", OpCertIssue, &service.IssueCertificateRequest{CommonName: "test"}},
		{"CertSignCSR_NilService", OpCertSignCSR, &service.SignCSRRequest{CSRPEM: "test"}},
		{"CertRevoke_NilService", OpCertRevoke, &service.RevokeCertificateRequest{SerialNumber: "123"}},
		{"CertVerify_NilService", OpCertVerify, &service.VerifyCertificateRequest{CertificatePEM: "test"}},
		{"CertGetCA_NilService", OpCertGetCA, &service.GetCACertificateRequest{}},
		{"CertGetTLSConfig_NilService", OpCertGetTLSConfig, &service.GetTLSConfigRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error for %s with nil service", tt.name)
			}
		})
	}
}

func TestHandleCertificateOperations_DecodeError(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	opcodes := []struct {
		name   string
		opcode byte
	}{
		{"CertIssue", OpCertIssue},
		{"CertSignCSR", OpCertSignCSR},
		{"CertRevoke", OpCertRevoke},
		{"CertVerify", OpCertVerify},
		{"CertGetCA", OpCertGetCA},
		{"CertGetTLSConfig", OpCertGetTLSConfig},
	}

	for _, op := range opcodes {
		t.Run(op.name+"_DecodeError", func(t *testing.T) {
			req := &Request{
				Opcode:  op.opcode,
				Payload: []byte("invalid json payload"),
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected decode error for %s", op.name)
			}
		})
	}
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestHandleCertIssue_AllUsageTypes(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	usageTypes := []string{"client", "server", "both", ""}

	for _, usage := range usageTypes {
		t.Run("Usage_"+usage, func(t *testing.T) {
			payload, _ := EncodePayload(&service.IssueCertificateRequest{
				CommonName:   "test.example.com",
				Organization: "Test Org",
				Usage:        usage,
			})

			req := &Request{
				Opcode:  OpCertIssue,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for usage '%s', got %d", usage, resp.Status)
			}
		})
	}
}

func TestHandleCertGetTLSConfig_AllVersions(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	versions := []string{"1.2", "1.3", "unknown", ""}

	for _, version := range versions {
		t.Run("Version_"+version, func(t *testing.T) {
			payload, _ := EncodePayload(&service.GetTLSConfigRequest{
				MinVersion: version,
			})

			req := &Request{
				Opcode:  OpCertGetTLSConfig,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for version '%s', got %d", version, resp.Status)
			}
		})
	}
}

func TestHandleCertIssue_WithAllFields(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.IssueCertificateRequest{
		CommonName:         "test.example.com",
		Organization:       "Test Organization",
		OrganizationalUnit: "Test Unit",
		Country:            "US",
		Province:           "California",
		Locality:           "San Francisco",
		DNSNames:           []string{"test.example.com", "www.test.example.com"},
		IPAddresses:        []string{"192.168.1.1", "10.0.0.1"},
		EmailAddresses:     []string{"admin@example.com"},
		URIs:               []string{"spiffe://example.com/test"},
		ValidityDays:       365,
		Usage:              "both",
		IsCA:               false,
		KeyAlgorithm:       "ecdsa",
		KeySize:            256,
		Metadata:           map[string]string{"env": "test"},
	})

	req := &Request{
		Opcode:  OpCertIssue,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleCertSignCSR_WithAllUsageTypes(t *testing.T) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	usageTypes := []string{"client", "server", "both", ""}

	for _, usage := range usageTypes {
		t.Run("SignCSR_Usage_"+usage, func(t *testing.T) {
			payload, _ := EncodePayload(&service.SignCSRRequest{
				CSRPEM:       "-----BEGIN CERTIFICATE REQUEST-----\ntest\n-----END CERTIFICATE REQUEST-----",
				ValidityDays: 30,
				Usage:        usage,
			})

			req := &Request{
				Opcode:  OpCertSignCSR,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for usage '%s', got %d", usage, resp.Status)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHandler_HandleCertIssue(b *testing.B) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.IssueCertificateRequest{
		CommonName:   "bench.example.com",
		Organization: "Bench Org",
		ValidityDays: 30,
		Usage:        "server",
	})
	req := &Request{
		Opcode:  OpCertIssue,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleCertVerify(b *testing.B) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.VerifyCertificateRequest{
		CertificatePEM: "-----BEGIN CERTIFICATE-----\nbench-cert\n-----END CERTIFICATE-----",
	})
	req := &Request{
		Opcode:  OpCertVerify,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleCertGetCA(b *testing.B) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.GetCACertificateRequest{})
	req := &Request{
		Opcode:  OpCertGetCA,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleCertGetTLSConfig(b *testing.B) {
	certSvc := &mockCertificateService{isInitialized: true}
	handler := createCertificateTestHandler(certSvc)

	payload, _ := EncodePayload(&service.GetTLSConfigRequest{
		MinVersion: "1.3",
		IsServer:   true,
	})
	req := &Request{
		Opcode:  OpCertGetTLSConfig,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

// CrossSign satisfies service.CertificateService. The certificate-extension
// verbs are exercised by the dedicated extension tests; this double records
// the call and returns the configured response.
func (m *mockCertificateService) CrossSign(ctx context.Context, req *service.CrossSignRequest) (*service.CrossSignResponse, error) {
	if m.CrossSignFunc != nil {
		return m.CrossSignFunc(ctx, req)
	}
	return &service.CrossSignResponse{}, nil
}

// Chain satisfies service.CertificateService.
func (m *mockCertificateService) Chain(ctx context.Context, req *service.CertificateChainRequest) (*service.CertificateChainResponse, error) {
	if m.ChainFunc != nil {
		return m.ChainFunc(ctx, req)
	}
	return &service.CertificateChainResponse{}, nil
}

// IssueDeviceIdentity satisfies service.CertificateService.
func (m *mockCertificateService) IssueDeviceIdentity(ctx context.Context, req *service.IssueDeviceIdentityRequest) (*service.IssueDeviceIdentityResponse, error) {
	if m.IssueDeviceIdentityFunc != nil {
		return m.IssueDeviceIdentityFunc(ctx, req)
	}
	return &service.IssueDeviceIdentityResponse{}, nil
}
