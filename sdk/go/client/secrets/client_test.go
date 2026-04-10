// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package secrets

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

func TestClient_Store_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &StoreSecretResponse{Version: 1}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *StoreSecretRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty path",
			req:     &StoreSecretRequest{Data: map[string]interface{}{"key": "value"}},
			wantErr: true,
		},
		{
			name:    "empty data",
			req:     &StoreSecretRequest{Path: "test/path"},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &StoreSecretRequest{
				Path: "database/prod",
				Data: map[string]interface{}{"username": "admin", "password": "secret"},
			},
			wantErr: false,
		},
		{
			name: "valid request with metadata",
			req: &StoreSecretRequest{
				Path:     "database/prod",
				Data:     map[string]interface{}{"username": "admin"},
				Metadata: map[string]string{"owner": "dba"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Store(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Store() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Store(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful store",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Store" {
						t.Errorf("expected method %q, got %q", "Store", method)
					}
					return &StoreSecretResponse{Version: 1}, nil
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
	}

	validReq := &StoreSecretRequest{
		Path: "test/secret",
		Data: map[string]interface{}{"key": "value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Store(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Store() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_Get(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		path      string
		opts      []GetOption
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get",
			ctx:  context.Background(),
			path: "database/prod",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Get" {
						t.Errorf("expected method %q, got %q", "Get", method)
					}
					return &Secret{
						Path:      "database/prod",
						Data:      map[string]interface{}{"username": "admin"},
						Version:   1,
						CreatedAt: time.Now(),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "get with version",
			ctx:  context.Background(),
			path: "database/prod",
			opts: []GetOption{WithVersion(2)},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					getReq := req.(*getSecretRequest)
					if getReq.Version != 2 {
						t.Errorf("expected version 2, got %d", getReq.Version)
					}
					return &Secret{Path: "database/prod", Version: 2}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty path",
			ctx:       context.Background(),
			path:      "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			path:      "test/path",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "not found",
			ctx:  context.Background(),
			path: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			path: "test/path",
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
			resp, err := client.Get(tt.ctx, tt.path, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_List(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		prefix    string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		pathCount int
	}{
		{
			name:   "successful list",
			ctx:    context.Background(),
			prefix: "database/",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "List" {
						t.Errorf("expected method %q, got %q", "List", method)
					}
					return &ListSecretsResponse{
						Paths: []string{"database/prod", "database/staging", "database/dev"},
					}, nil
				},
			},
			wantErr:   false,
			pathCount: 3,
		},
		{
			name:   "empty list with nil response",
			ctx:    context.Background(),
			prefix: "empty/",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:   false,
			pathCount: 0,
		},
		{
			name:      "nil context",
			ctx:       nil,
			prefix:    "test/",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:   "invalid response type",
			ctx:    context.Background(),
			prefix: "test/",
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
			resp, err := client.List(tt.ctx, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Paths) != tt.pathCount {
				t.Errorf("expected %d paths, got %d", tt.pathCount, len(resp.Paths))
			}
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		path      string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful delete",
			ctx:  context.Background(),
			path: "test/secret",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Delete" {
						t.Errorf("expected method %q, got %q", "Delete", method)
					}
					return &deleteSecretResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful delete with nil response",
			ctx:  context.Background(),
			path: "test/secret",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "empty path",
			ctx:       context.Background(),
			path:      "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			path:      "test/path",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "not found",
			ctx:  context.Background(),
			path: "nonexistent",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "secret not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			path: "test/path",
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
			err := client.Delete(tt.ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestSecret_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &Secret{
				Path: "database/credentials",
				Data: map[string]interface{}{
					"username": "admin",
					"password": "s3cr3t",
				},
				Metadata: map[string]string{
					"owner":       "dba-team",
					"environment": "production",
				},
				Version:   3,
				CreatedAt: now,
			}, nil
		},
	}

	client := NewClient(mockT)
	secret, err := client.Get(context.Background(), "database/credentials")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if secret.Path != "database/credentials" {
		t.Errorf("expected Path='database/credentials', got %q", secret.Path)
	}
	if secret.Data["username"] != "admin" {
		t.Errorf("expected username='admin', got %v", secret.Data["username"])
	}
	if secret.Metadata["owner"] != "dba-team" {
		t.Errorf("expected owner='dba-team', got %v", secret.Metadata["owner"])
	}
	if secret.Version != 3 {
		t.Errorf("expected Version=3, got %d", secret.Version)
	}
}

func TestGetOption_WithVersion(t *testing.T) {
	opts := &getOptions{}
	WithVersion(5)(opts)
	if opts.version != 5 {
		t.Errorf("expected version=5, got %d", opts.version)
	}
}
