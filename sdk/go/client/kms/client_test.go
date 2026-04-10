// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package kms

import (
	"context"
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

func TestClient_GenerateKey_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GenerateKeyResponse{KeyID: "key-123", Version: 1, Algorithm: AlgorithmAES256GCM}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GenerateKeyRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &GenerateKeyRequest{Algorithm: AlgorithmAES256GCM},
			wantErr: true,
		},
		{
			name:    "invalid algorithm",
			req:     &GenerateKeyRequest{Name: "test-key", Algorithm: KeyAlgorithm(99)},
			wantErr: true,
		},
		{
			name: "invalid usage",
			req: &GenerateKeyRequest{
				Name:      "test-key",
				Algorithm: AlgorithmAES256GCM,
				Usage:     []KeyUsage{KeyUsage(99)},
			},
			wantErr: true,
		},
		{
			name:    "valid AES key",
			req:     &GenerateKeyRequest{Name: "test-key", Algorithm: AlgorithmAES256GCM},
			wantErr: false,
		},
		{
			name: "valid key with usage",
			req: &GenerateKeyRequest{
				Name:      "encrypt-key",
				Algorithm: AlgorithmAES256GCM,
				Usage:     []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
			},
			wantErr: false,
		},
		{
			name:    "valid ED25519 key",
			req:     &GenerateKeyRequest{Name: "sign-key", Algorithm: AlgorithmED25519},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateKey(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GenerateKey(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful generation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "GenerateKey" {
						t.Errorf("expected method %q, got %q", "GenerateKey", method)
					}
					return &GenerateKeyResponse{
						KeyID:     "key-abc123",
						Version:   1,
						Algorithm: AlgorithmAES256GCM,
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

	validReq := &GenerateKeyRequest{Name: "test-key", Algorithm: AlgorithmAES256GCM}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GenerateKey(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateKey() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_GetKey(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful get",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetKey" {
						t.Errorf("expected method %q, got %q", "GetKey", method)
					}
					return &KeyInfo{
						KeyID:     "key-123",
						Name:      "test-key",
						Algorithm: AlgorithmAES256GCM,
						Version:   1,
						Usage:     []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
						CreatedAt: time.Now(),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-123",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "key not found",
			ctx:   context.Background(),
			keyID: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetKey(tt.ctx, tt.keyID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetKey() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_ListKeys(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		keyCount  int
	}{
		{
			name: "successful list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListKeys" {
						t.Errorf("expected method %q, got %q", "ListKeys", method)
					}
					return &ListKeysResponse{
						Keys: []KeyInfo{
							{KeyID: "key-1", Name: "key-one", Algorithm: AlgorithmAES256GCM},
							{KeyID: "key-2", Name: "key-two", Algorithm: AlgorithmED25519},
						},
					}, nil
				},
			},
			wantErr:  false,
			keyCount: 2,
		},
		{
			name: "empty list with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:  false,
			keyCount: 0,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListKeys(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListKeys() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Keys) != tt.keyCount {
				t.Errorf("expected %d keys, got %d", tt.keyCount, len(resp.Keys))
			}
		})
	}
}

func TestClient_RotateKey(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		keyID      string
		transport  *mockTransport
		wantErr    bool
		errCode    errors.ErrorCode
		newVersion int
	}{
		{
			name:  "successful rotation",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RotateKey" {
						t.Errorf("expected method %q, got %q", "RotateKey", method)
					}
					return &RotateKeyResponse{KeyID: "key-123", NewVersion: 2}, nil
				},
			},
			wantErr:    false,
			newVersion: 2,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-123",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "nil response",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RotateKey(tt.ctx, tt.keyID)
			if (err != nil) != tt.wantErr {
				t.Errorf("RotateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp.NewVersion != tt.newVersion {
				t.Errorf("expected new version %d, got %d", tt.newVersion, resp.NewVersion)
			}
		})
	}
}

func TestClient_DeleteKey(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:  "successful delete",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "DeleteKey" {
						t.Errorf("expected method %q, got %q", "DeleteKey", method)
					}
					return &deleteKeyResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name:  "successful delete with nil response",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-123",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:  "key not found",
			ctx:   context.Background(),
			keyID: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "key not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:  "invalid response type",
			ctx:   context.Background(),
			keyID: "key-123",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.DeleteKey(tt.ctx, tt.keyID)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestKeyAlgorithm_String(t *testing.T) {
	tests := []struct {
		alg      KeyAlgorithm
		expected string
	}{
		{AlgorithmAES256GCM, "AES-256-GCM"},
		{AlgorithmChaCha20Poly1305, "CHACHA20-POLY1305"},
		{AlgorithmRSA2048, "RSA-2048"},
		{AlgorithmRSA4096, "RSA-4096"},
		{AlgorithmECDSAP256, "ECDSA-P256"},
		{AlgorithmECDSAP384, "ECDSA-P384"},
		{AlgorithmED25519, "ED25519"},
		{KeyAlgorithm(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.alg.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestKeyUsage_String(t *testing.T) {
	tests := []struct {
		usage    KeyUsage
		expected string
	}{
		{KeyUsageEncrypt, "ENCRYPT"},
		{KeyUsageDecrypt, "DECRYPT"},
		{KeyUsageSign, "SIGN"},
		{KeyUsageVerify, "VERIFY"},
		{KeyUsageWrap, "WRAP"},
		{KeyUsageUnwrap, "UNWRAP"},
		{KeyUsage(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.usage.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestKeyInfo_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &KeyInfo{
				KeyID:     "key-abc",
				Name:      "my-key",
				Algorithm: AlgorithmED25519,
				Version:   3,
				Usage:     []KeyUsage{KeyUsageSign, KeyUsageVerify},
				CreatedAt: now.Add(-24 * time.Hour),
				RotatedAt: now,
			}, nil
		},
	}

	client := NewClient(mockT)
	info, err := client.GetKey(context.Background(), "key-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.KeyID != "key-abc" {
		t.Errorf("expected KeyID='key-abc', got %q", info.KeyID)
	}
	if info.Name != "my-key" {
		t.Errorf("expected Name='my-key', got %q", info.Name)
	}
	if info.Algorithm != AlgorithmED25519 {
		t.Errorf("expected Algorithm=ED25519, got %v", info.Algorithm)
	}
	if info.Version != 3 {
		t.Errorf("expected Version=3, got %d", info.Version)
	}
	if len(info.Usage) != 2 {
		t.Errorf("expected 2 usages, got %d", len(info.Usage))
	}
}
