// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package crypto

import (
	"context"
	"testing"

	svc "github.com/jeremyhahn/go-quicbaas/pkg/service"
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

func TestClient_Encrypt_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &EncryptResponse{Ciphertext: []byte("encrypted"), KeyVersion: 1}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *EncryptRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key ID",
			req:     &EncryptRequest{Plaintext: []byte("data")},
			wantErr: true,
		},
		{
			name:    "empty plaintext",
			req:     &EncryptRequest{KeyID: "key-1"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &EncryptRequest{KeyID: "key-1", Plaintext: []byte("data")},
			wantErr: false,
		},
		{
			name:    "valid with context",
			req:     &EncryptRequest{KeyID: "key-1", Plaintext: []byte("data"), Context: []byte("aad")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Encrypt(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Encrypt(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful encrypt",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Encrypt" {
						t.Errorf("expected method %q, got %q", "Encrypt", method)
					}
					return &EncryptResponse{Ciphertext: []byte("encrypted-data"), KeyVersion: 1}, nil
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
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server down")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &EncryptRequest{KeyID: "key-1", Plaintext: []byte("data")}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Encrypt(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Encrypt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_Decrypt_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DecryptResponse{Plaintext: []byte("decrypted")}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DecryptRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key ID",
			req:     &DecryptRequest{Ciphertext: []byte("encrypted")},
			wantErr: true,
		},
		{
			name:    "empty ciphertext",
			req:     &DecryptRequest{KeyID: "key-1"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DecryptRequest{KeyID: "key-1", Ciphertext: []byte("encrypted")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Decrypt(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decrypt() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Decrypt(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful decrypt",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Decrypt" {
						t.Errorf("expected method %q, got %q", "Decrypt", method)
					}
					return &DecryptResponse{Plaintext: []byte("decrypted-data")}, nil
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
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server down")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &DecryptRequest{KeyID: "key-1", Ciphertext: []byte("encrypted")}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Decrypt(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decrypt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_Sign_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SignResponse{Signature: []byte("signature"), KeyVersion: 1}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SignRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key ID",
			req:     &SignRequest{Data: []byte("data"), HashAlg: HashSHA256},
			wantErr: true,
		},
		{
			name:    "empty data",
			req:     &SignRequest{KeyID: "key-1", HashAlg: HashSHA256},
			wantErr: true,
		},
		{
			name:    "invalid hash algorithm",
			req:     &SignRequest{KeyID: "key-1", Data: []byte("data"), HashAlg: HashAlgorithm(99)},
			wantErr: true,
		},
		{
			name:    "valid request SHA256",
			req:     &SignRequest{KeyID: "key-1", Data: []byte("data"), HashAlg: HashSHA256},
			wantErr: false,
		},
		{
			name:    "valid request SHA384",
			req:     &SignRequest{KeyID: "key-1", Data: []byte("data"), HashAlg: HashSHA384},
			wantErr: false,
		},
		{
			name:    "valid request SHA512",
			req:     &SignRequest{KeyID: "key-1", Data: []byte("data"), HashAlg: HashSHA512},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Sign(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Sign() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Sign(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful sign",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Sign" {
						t.Errorf("expected method %q, got %q", "Sign", method)
					}
					return &SignResponse{Signature: []byte("sig"), KeyVersion: 1}, nil
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
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server down")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &SignRequest{KeyID: "key-1", Data: []byte("data"), HashAlg: HashSHA256}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Sign(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Sign() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_Verify_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &VerifyResponse{Valid: true}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *VerifyRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key ID",
			req:     &VerifyRequest{Data: []byte("data"), Signature: []byte("sig"), HashAlg: HashSHA256},
			wantErr: true,
		},
		{
			name:    "empty data",
			req:     &VerifyRequest{KeyID: "key-1", Signature: []byte("sig"), HashAlg: HashSHA256},
			wantErr: true,
		},
		{
			name:    "empty signature",
			req:     &VerifyRequest{KeyID: "key-1", Data: []byte("data"), HashAlg: HashSHA256},
			wantErr: true,
		},
		{
			name:    "invalid hash algorithm",
			req:     &VerifyRequest{KeyID: "key-1", Data: []byte("data"), Signature: []byte("sig"), HashAlg: HashAlgorithm(99)},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &VerifyRequest{KeyID: "key-1", Data: []byte("data"), Signature: []byte("sig"), HashAlg: HashSHA256},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Verify(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Verify(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		valid     bool
	}{
		{
			name: "valid signature",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Verify" {
						t.Errorf("expected method %q, got %q", "Verify", method)
					}
					return &VerifyResponse{Valid: true}, nil
				},
			},
			wantErr: false,
			valid:   true,
		},
		{
			name: "invalid signature",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &VerifyResponse{Valid: false}, nil
				},
			},
			wantErr: false,
			valid:   false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
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
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server down")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &VerifyRequest{KeyID: "key-1", Data: []byte("data"), Signature: []byte("sig"), HashAlg: HashSHA256}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Verify(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp.Valid != tt.valid {
				t.Errorf("expected Valid=%v, got %v", tt.valid, resp.Valid)
			}
		})
	}
}

func TestHashAlgorithm_String(t *testing.T) {
	tests := []struct {
		alg      HashAlgorithm
		expected string
	}{
		{HashSHA256, "SHA-256"},
		{HashSHA384, "SHA-384"},
		{HashSHA512, "SHA-512"},
		{HashAlgorithm(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.alg.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := []byte("sensitive data to encrypt")
	ciphertext := []byte("encrypted-ciphertext")

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, svcName, method string, req interface{}) (interface{}, error) {
			switch method {
			case "Encrypt":
				encReq := req.(*svc.EncryptRequest)
				if string(encReq.Plaintext) != string(plaintext) {
					t.Errorf("expected plaintext %q, got %q", plaintext, encReq.Plaintext)
				}
				return &svc.EncryptResponse{Ciphertext: ciphertext, KeyVersion: 1}, nil
			case "Decrypt":
				decReq := req.(*svc.DecryptRequest)
				if string(decReq.Ciphertext) != string(ciphertext) {
					t.Errorf("expected ciphertext %q, got %q", ciphertext, decReq.Ciphertext)
				}
				return &svc.DecryptResponse{Plaintext: plaintext}, nil
			}
			return nil, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	// Encrypt
	encResp, err := client.Encrypt(ctx, &EncryptRequest{
		KeyID:     "test-key",
		Plaintext: plaintext,
	})
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypt
	decResp, err := client.Decrypt(ctx, &DecryptRequest{
		KeyID:      "test-key",
		Ciphertext: encResp.Ciphertext,
	})
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decResp.Plaintext) != string(plaintext) {
		t.Errorf("roundtrip failed: expected %q, got %q", plaintext, decResp.Plaintext)
	}
}
