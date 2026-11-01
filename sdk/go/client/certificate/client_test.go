// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package certificate

import (
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_IssueCertificate_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &IssueCertificateResponse{
				CertificatePEM: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
				SerialNumber:   "12345",
				NotBefore:      time.Now(),
				NotAfter:       time.Now().Add(365 * 24 * time.Hour),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *IssueCertificateRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty common name",
			req:     &IssueCertificateRequest{},
			wantErr: true,
		},
		{
			name:    "invalid usage",
			req:     &IssueCertificateRequest{CommonName: "test.example.com", Usage: "invalid"},
			wantErr: true,
		},
		{
			name:    "invalid key algorithm",
			req:     &IssueCertificateRequest{CommonName: "test.example.com", KeyAlgorithm: "invalid"},
			wantErr: true,
		},
		{
			name:    "negative validity days",
			req:     &IssueCertificateRequest{CommonName: "test.example.com", ValidityDays: -1},
			wantErr: true,
		},
		{
			name:    "negative max path len",
			req:     &IssueCertificateRequest{CommonName: "test.example.com", MaxPathLen: -1},
			wantErr: true,
		},
		{
			name:    "negative key size",
			req:     &IssueCertificateRequest{CommonName: "test.example.com", KeySize: -1},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &IssueCertificateRequest{CommonName: "test.example.com"},
			wantErr: false,
		},
		{
			name: "valid full request with client usage",
			req: &IssueCertificateRequest{
				CommonName:         "test.example.com",
				Organization:       "Example Corp",
				OrganizationalUnit: "Engineering",
				Country:            "US",
				Province:           "California",
				Locality:           "San Francisco",
				DNSNames:           []string{"test.example.com", "*.test.example.com"},
				IPAddresses:        []string{"192.168.1.1"},
				EmailAddresses:     []string{"admin@example.com"},
				URIs:               []string{"spiffe://example.com/test"},
				ValidityDays:       365,
				Usage:              "client",
				KeyAlgorithm:       "ecdsa",
				KeySize:            256,
			},
			wantErr: false,
		},
		{
			name: "valid server usage",
			req: &IssueCertificateRequest{
				CommonName: "server.example.com",
				Usage:      "server",
			},
			wantErr: false,
		},
		{
			name: "valid both usage",
			req: &IssueCertificateRequest{
				CommonName: "both.example.com",
				Usage:      "both",
			},
			wantErr: false,
		},
		{
			name: "valid rsa algorithm",
			req: &IssueCertificateRequest{
				CommonName:   "rsa.example.com",
				KeyAlgorithm: "rsa",
				KeySize:      4096,
			},
			wantErr: false,
		},
		{
			name: "valid CA certificate",
			req: &IssueCertificateRequest{
				CommonName: "CA.example.com",
				IsCA:       true,
				MaxPathLen: 1,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.IssueCertificate(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("IssueCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_IssueCertificate(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful issuance",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "IssueCertificate" {
						t.Errorf("expected method %q, got %q", "IssueCertificate", method)
					}
					return &IssueCertificateResponse{
						CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
						PrivateKeyPEM:    "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----",
						CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
						ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
						SerialNumber:     "12345",
						NotBefore:        now,
						NotAfter:         now.Add(365 * 24 * time.Hour),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &IssueCertificateRequest{CommonName: "test.example.com"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.IssueCertificate(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("IssueCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && (resp == nil || resp.SerialNumber != "12345" || resp.PrivateKeyPEM == "") {
				t.Errorf("expected the transport response, got %#v", resp)
			}
		})
	}
}

func TestClient_SignCSR_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SignCSRResponse{
				CertificatePEM: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
				SerialNumber:   "12345",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SignCSRRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty CSR PEM",
			req:     &SignCSRRequest{},
			wantErr: true,
		},
		{
			name:    "invalid usage",
			req:     &SignCSRRequest{CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----", Usage: "invalid"},
			wantErr: true,
		},
		{
			name:    "negative validity days",
			req:     &SignCSRRequest{CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----", ValidityDays: -1},
			wantErr: true,
		},
		{
			name:    "valid minimal request",
			req:     &SignCSRRequest{CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &SignCSRRequest{
				CSRPEM:       "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----",
				ValidityDays: 365,
				Usage:        "client",
			},
			wantErr: false,
		},
		{
			name: "valid server usage",
			req: &SignCSRRequest{
				CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----",
				Usage:  "server",
			},
			wantErr: false,
		},
		{
			name: "valid both usage",
			req: &SignCSRRequest{
				CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----",
				Usage:  "both",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SignCSR(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SignCSR() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_SignCSR(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful signing",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "SignCSR" {
						t.Errorf("expected method %q, got %q", "SignCSR", method)
					}
					return &SignCSRResponse{
						CertificatePEM:   "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
						CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
						ChainPEM:         "-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----",
						SerialNumber:     "67890",
						NotBefore:        now,
						NotAfter:         now.Add(365 * 24 * time.Hour),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &SignCSRRequest{CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n...\n-----END CERTIFICATE REQUEST-----"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.SignCSR(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("SignCSR() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && (resp == nil || resp.SerialNumber != "67890") {
				t.Errorf("expected the transport response, got %#v", resp)
			}
		})
	}
}

func TestClient_RevokeCertificate_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RevokeCertificateResponse{Success: true}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RevokeCertificateRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty serial number",
			req:     &RevokeCertificateRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RevokeCertificateRequest{SerialNumber: "12345"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RevokeCertificate(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RevokeCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RevokeCertificate(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful revocation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RevokeCertificate" {
						t.Errorf("expected method %q, got %q", "RevokeCertificate", method)
					}
					return &RevokeCertificateResponse{Success: true}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "certificate not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &RevokeCertificateRequest{SerialNumber: "12345"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RevokeCertificate(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("RevokeCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
			if !tt.wantErr && !resp.Success {
				t.Error("expected success to be true")
			}
		})
	}
}

func TestClient_VerifyCertificate_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &VerifyCertificateResponse{Valid: true}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *VerifyCertificateRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty certificate PEM",
			req:     &VerifyCertificateRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &VerifyCertificateRequest{CertificatePEM: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.VerifyCertificate(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_VerifyCertificate(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		transport    *mockTransport
		wantErr      bool
		errCode      errors.ErrorCode
		expectValid  bool
		expectErrMsg string
	}{
		{
			name: "valid certificate",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "VerifyCertificate" {
						t.Errorf("expected method %q, got %q", "VerifyCertificate", method)
					}
					return &VerifyCertificateResponse{Valid: true}, nil
				},
			},
			wantErr:     false,
			expectValid: true,
		},
		{
			name: "invalid certificate",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &VerifyCertificateResponse{Valid: false, ErrorMessage: "certificate expired"}, nil
				},
			},
			wantErr:      false,
			expectValid:  false,
			expectErrMsg: "certificate expired",
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &VerifyCertificateRequest{CertificatePEM: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.VerifyCertificate(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyCertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp == nil {
					t.Error("expected non-nil response")
				} else {
					if resp.Valid != tt.expectValid {
						t.Errorf("expected Valid=%v, got %v", tt.expectValid, resp.Valid)
					}
					if resp.ErrorMessage != tt.expectErrMsg {
						t.Errorf("expected ErrorMessage=%q, got %q", tt.expectErrMsg, resp.ErrorMessage)
					}
				}
			}
		})
	}
}

func TestClient_GetCACertificate(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		req       *GetCACertificateRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful retrieval",
			ctx:  context.Background(),
			req:  &GetCACertificateRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetCACertificate" {
						t.Errorf("expected method %q, got %q", "GetCACertificate", method)
					}
					return &GetCACertificateResponse{
						CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
						CABundlePEM:      "-----BEGIN CERTIFICATE-----\nbundle\n-----END CERTIFICATE-----",
						Identity:         "QuicBaaS CA",
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful retrieval with nil request",
			ctx:  context.Background(),
			req:  nil,
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &GetCACertificateResponse{
						CACertificatePEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----",
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &GetCACertificateRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			req:  &GetCACertificateRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotInitialized("test", "certificate service not initialized")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotInitialized,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			req:  &GetCACertificateRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			req:  &GetCACertificateRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return []byte("invalid"), nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetCACertificate(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCACertificate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && (resp == nil || resp.CACertificatePEM != "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----") {
				t.Errorf("expected the transport response, got %#v", resp)
			}
		})
	}
}

func TestClient_GetTLSConfig_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetTLSConfigResponse{Config: &tls.Config{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetTLSConfigRequest
		wantErr bool
	}{
		{
			name:    "nil request uses defaults",
			req:     nil,
			wantErr: false,
		},
		{
			name:    "empty request",
			req:     &GetTLSConfigRequest{},
			wantErr: false,
		},
		{
			name:    "invalid min version",
			req:     &GetTLSConfigRequest{MinVersion: "1.1"},
			wantErr: true,
		},
		{
			name:    "valid TLS 1.2",
			req:     &GetTLSConfigRequest{MinVersion: "1.2"},
			wantErr: false,
		},
		{
			name:    "valid TLS 1.3",
			req:     &GetTLSConfigRequest{MinVersion: "1.3"},
			wantErr: false,
		},
		{
			name: "valid full request",
			req: &GetTLSConfigRequest{
				ServerName:        "api.example.com",
				RequireClientCert: true,
				IsServer:          true,
				MinVersion:        "1.3",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetTLSConfig(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTLSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetTLSConfig(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful retrieval",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetTLSConfig" {
						t.Errorf("expected method %q, got %q", "GetTLSConfig", method)
					}
					return &GetTLSConfigResponse{
						Config: &tls.Config{
							MinVersion: tls.VersionTLS13,
							ServerName: "api.example.com",
						},
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &tls.Config{}, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &GetTLSConfigRequest{MinVersion: "1.3"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetTLSConfig(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTLSConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
			if !tt.wantErr && resp.Config == nil {
				t.Error("expected non-nil TLS config")
			}
		})
	}
}

func TestClient_IsInitialized(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		transport   *mockTransport
		wantErr     bool
		errCode     errors.ErrorCode
		initialized bool
	}{
		{
			name: "initialized",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "IsInitialized" {
						t.Errorf("expected method %q, got %q", "IsInitialized", method)
					}
					return &IsInitializedResponse{Initialized: true}, nil
				},
			},
			wantErr:     false,
			initialized: true,
		},
		{
			name: "not initialized",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &IsInitializedResponse{Initialized: false}, nil
				},
			},
			wantErr:     false,
			initialized: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return true, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			initialized, err := client.IsInitialized(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsInitialized() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && initialized != tt.initialized {
				t.Errorf("expected initialized=%v, got %v", tt.initialized, initialized)
			}
		})
	}
}

func TestValidationErrorContext(t *testing.T) {
	mockT := &mockTransport{}
	client := NewClient(mockT)
	ctx := context.Background()

	t.Run("invalid usage context", func(t *testing.T) {
		_, err := client.IssueCertificate(ctx, &IssueCertificateRequest{
			CommonName: "test.example.com",
			Usage:      "invalid",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatal("expected QBError")
		}
		if qbErr.Context == nil {
			t.Fatal("expected error context")
		}
		if qbErr.Context["usage"] != "invalid" {
			t.Errorf("expected usage='invalid' in context, got %v", qbErr.Context["usage"])
		}
	})

	t.Run("invalid key algorithm context", func(t *testing.T) {
		_, err := client.IssueCertificate(ctx, &IssueCertificateRequest{
			CommonName:   "test.example.com",
			KeyAlgorithm: "invalid",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatal("expected QBError")
		}
		if qbErr.Context == nil {
			t.Fatal("expected error context")
		}
		if qbErr.Context["key_algorithm"] != "invalid" {
			t.Errorf("expected key_algorithm='invalid' in context, got %v", qbErr.Context["key_algorithm"])
		}
	})

	t.Run("invalid TLS version context", func(t *testing.T) {
		_, err := client.GetTLSConfig(ctx, &GetTLSConfigRequest{
			MinVersion: "1.0",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatal("expected QBError")
		}
		if qbErr.Context == nil {
			t.Fatal("expected error context")
		}
		if qbErr.Context["min_version"] != "1.0" {
			t.Errorf("expected min_version='1.0' in context, got %v", qbErr.Context["min_version"])
		}
	})
}

// =============================================================================
// CA Administration Surface Tests
// =============================================================================

func TestClient_CAInfo(t *testing.T) {
	now := time.Now().UTC()
	full := &CAInfoResponse{
		CommonName:         "Root",
		Subject:            "CN=Root",
		Issuer:             "CN=Root",
		SerialNumber:       "ff",
		NotBefore:          now,
		NotAfter:           now.Add(24 * time.Hour),
		IsCA:               true,
		KeyType:            "RSA",
		KeyBits:            2048,
		SignatureAlgorithm: "SHA256-RSA",
		CertificatePEM:     "-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n",
		CABundlePEM:        "-----BEGIN CERTIFICATE-----\nb\n-----END CERTIFICATE-----\n",
	}
	tests := []struct {
		name             string
		ctx              context.Context
		req              *CAInfoRequest
		resp             interface{}
		respErr          error
		wantErr          bool
		wantInvalidInput bool
		want             *CAInfoResponse
	}{
		{name: "nil context", req: &CAInfoRequest{}, wantErr: true, wantInvalidInput: true},
		{name: "nil request is allowed", ctx: context.Background(), resp: &CAInfoResponse{CommonName: "root"}, want: &CAInfoResponse{CommonName: "root"}},
		{name: "transport error", ctx: context.Background(), req: &CAInfoRequest{}, respErr: errors.NewInternal("transport.test", "boom"), wantErr: true},
		{name: "nil response", ctx: context.Background(), req: &CAInfoRequest{}, wantErr: true},
		{name: "wrong response type", ctx: context.Background(), req: &CAInfoRequest{}, resp: "not a *CAInfoResponse", wantErr: true},
		{name: "full response", ctx: context.Background(), req: &CAInfoRequest{}, resp: full, want: full},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockT := &mockTransport{
				requestFn: func(_ context.Context, svc string, method string, _ interface{}) (interface{}, error) {
					if svc != "quicbaas.Certificate" || method != "CAInfo" {
						t.Errorf("unexpected dispatch svc=%s method=%s", svc, method)
					}
					return tt.resp, tt.respErr
				},
			}
			resp, err := NewClient(mockT).CAInfo(tt.ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.wantInvalidInput && errors.Code(err) != errors.CodeInvalidInput {
					t.Errorf("expected CodeInvalidInput, got %v", errors.Code(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if resp == nil || resp.CommonName != tt.want.CommonName || resp.IsCA != tt.want.IsCA ||
				resp.KeyBits != tt.want.KeyBits || resp.SerialNumber != tt.want.SerialNumber {
				t.Errorf("response fields wrong: %#v", resp)
			}
		})
	}
}

func TestClient_ListCertificates(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name             string
		ctx              context.Context
		req              *ListCertificatesRequest
		resp             interface{}
		respErr          error
		wantErr          bool
		wantInvalidInput bool
		wantTotal        int
		wantDNSName      string
	}{
		{name: "negative limit", ctx: context.Background(), req: &ListCertificatesRequest{Limit: -1}, wantErr: true, wantInvalidInput: true},
		{name: "nil context", wantErr: true},
		{name: "transport error", ctx: context.Background(), req: &ListCertificatesRequest{}, respErr: errors.NewInternal("test", "fail"), wantErr: true},
		{name: "nil response", ctx: context.Background(), req: &ListCertificatesRequest{}, wantErr: true},
		{name: "wrong response type", ctx: context.Background(), req: &ListCertificatesRequest{}, resp: 42, wantErr: true},
		{name: "nil request defaults", ctx: context.Background(), resp: &ListCertificatesResponse{}, wantTotal: 0},
		{
			name: "full response",
			ctx:  context.Background(),
			req:  &ListCertificatesRequest{CommonNameFilter: "api", IncludeRevoked: true, Limit: 10},
			resp: &ListCertificatesResponse{
				Certificates: []CertificateSummary{{
					CommonName: "api.example.com", SerialNumber: "ab", NotBefore: now, NotAfter: now.Add(time.Hour),
					KeyUsage: "DigitalSignature, KeyEncipherment", DNSNames: []string{"api.example.com"},
				}},
				TotalCount: 1,
			},
			wantTotal:   1,
			wantDNSName: "api.example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockT := &mockTransport{
				requestFn: func(_ context.Context, _ string, method string, req interface{}) (interface{}, error) {
					if method != "ListCertificates" {
						t.Errorf("unexpected method %s", method)
					}
					r, ok := req.(*ListCertificatesRequest)
					if !ok || r == nil {
						t.Fatalf("expected non-nil *ListCertificatesRequest, got %T", req)
					}
					if tt.req != nil && *r != *tt.req {
						t.Errorf("request not preserved: %#v", r)
					}
					return tt.resp, tt.respErr
				},
			}
			resp, err := NewClient(mockT).ListCertificates(tt.ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.wantInvalidInput && errors.Code(err) != errors.CodeInvalidInput {
					t.Errorf("expected CodeInvalidInput, got %v", errors.Code(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if resp.TotalCount != tt.wantTotal || len(resp.Certificates) != tt.wantTotal {
				t.Fatalf("unexpected response: %#v", resp)
			}
			if tt.wantDNSName != "" && resp.Certificates[0].DNSNames[0] != tt.wantDNSName {
				t.Errorf("DNSNames not propagated")
			}
		})
	}
}

// =============================================================================
// GetCABundle
// =============================================================================

func TestClient_GetCABundle(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
				if method != "GetCABundle" {
					t.Errorf("expected method GetCABundle, got %q", method)
				}
				return &GetCABundleResponse{BundlePEM: "-----BEGIN CERTIFICATE-----"}, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.GetCABundle(context.Background(), &GetCABundleRequest{StoreType: "tls"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.BundlePEM == "" {
			t.Error("expected non-empty bundle PEM")
		}
	})

	t.Run("nil request defaults", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
				if req == nil {
					t.Error("expected non-nil defaulted request")
				}
				return &GetCABundleResponse{BundlePEM: "x"}, nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.GetCABundle(context.Background(), nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if _, err := client.GetCABundle(nil, &GetCABundleRequest{}); err == nil {
			t.Fatal("expected nil-context error")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, errors.NewInternal("test", "boom")
			},
		}
		client := NewClient(mockT)
		if _, err := client.GetCABundle(context.Background(), &GetCABundleRequest{}); err == nil {
			t.Fatal("expected transport error")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.GetCABundle(context.Background(), &GetCABundleRequest{}); err == nil {
			t.Fatal("expected nil-response error")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return "not-a-bundle", nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.GetCABundle(context.Background(), &GetCABundleRequest{}); err == nil {
			t.Fatal("expected invalid-response-type error")
		}
	})
}

// =============================================================================
// GenerateCRL
// =============================================================================

func TestClient_GenerateCRL(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
				if method != "GenerateCRL" {
					t.Errorf("expected method GenerateCRL, got %q", method)
				}
				return &GenerateCRLResponse{CRLPEM: "-----BEGIN X509 CRL-----"}, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.GenerateCRL(context.Background(), &GenerateCRLRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CRLPEM == "" {
			t.Error("expected non-empty CRL PEM")
		}
	})

	t.Run("nil request defaults", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
				if req == nil {
					t.Error("expected non-nil defaulted request")
				}
				return &GenerateCRLResponse{CRLPEM: "x"}, nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.GenerateCRL(context.Background(), nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if _, err := client.GenerateCRL(nil, &GenerateCRLRequest{}); err == nil {
			t.Fatal("expected nil-context error")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, errors.NewInternal("test", "boom")
			},
		}
		client := NewClient(mockT)
		if _, err := client.GenerateCRL(context.Background(), &GenerateCRLRequest{}); err == nil {
			t.Fatal("expected transport error")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.GenerateCRL(context.Background(), &GenerateCRLRequest{}); err == nil {
			t.Fatal("expected nil-response error")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return 99, nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.GenerateCRL(context.Background(), &GenerateCRLRequest{}); err == nil {
			t.Fatal("expected invalid-response-type error")
		}
	})
}

// =============================================================================
// IsRevoked
// =============================================================================

func TestClient_IsRevoked_Validation(t *testing.T) {
	client := NewClient(&mockTransport{})

	t.Run("nil request", func(t *testing.T) {
		if _, err := client.IsRevoked(context.Background(), nil); err == nil {
			t.Fatal("expected validation error for nil request")
		}
	})

	t.Run("empty serial number", func(t *testing.T) {
		if _, err := client.IsRevoked(context.Background(), &IsRevokedRequest{}); err == nil {
			t.Fatal("expected validation error for empty serial number")
		}
	})
}

func TestClient_IsRevoked(t *testing.T) {
	t.Run("happy path revoked", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
				if method != "IsRevoked" {
					t.Errorf("expected method IsRevoked, got %q", method)
				}
				return &IsRevokedResponse{Revoked: true, Reason: 1, Message: "keyCompromise"}, nil
			},
		}
		client := NewClient(mockT)
		resp, err := client.IsRevoked(context.Background(), &IsRevokedRequest{SerialNumber: "abc123"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Revoked {
			t.Error("expected revoked=true")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if _, err := client.IsRevoked(nil, &IsRevokedRequest{SerialNumber: "abc"}); err == nil {
			t.Fatal("expected nil-context error")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, errors.NewInternal("test", "boom")
			},
		}
		client := NewClient(mockT)
		if _, err := client.IsRevoked(context.Background(), &IsRevokedRequest{SerialNumber: "abc"}); err == nil {
			t.Fatal("expected transport error")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.IsRevoked(context.Background(), &IsRevokedRequest{SerialNumber: "abc"}); err == nil {
			t.Fatal("expected nil-response error")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return "nope", nil
			},
		}
		client := NewClient(mockT)
		if _, err := client.IsRevoked(context.Background(), &IsRevokedRequest{SerialNumber: "abc"}); err == nil {
			t.Fatal("expected invalid-response-type error")
		}
	})
}
