// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package cache

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
		c := NewClient(&mockTransport{})
		if c == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		c := NewClient(nil)
		if c != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_GetCacheStats(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		req       *GetCacheStatsRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		validate  func(*testing.T, *GetCacheStatsResponse)
	}{
		{
			name: "successful stats retrieval",
			ctx:  context.Background(),
			req:  &GetCacheStatsRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "GetCacheStats" {
						t.Errorf("expected method %q, got %q", "GetCacheStats", method)
					}
					return &GetCacheStatsResponse{
						HitRate:   0.85,
						Size:      1000,
						MaxSize:   10000,
						Evictions: 50,
						Hits:      8500,
						Misses:    1500,
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *GetCacheStatsResponse) {
				if resp.HitRate != 0.85 {
					t.Errorf("expected HitRate 0.85, got %f", resp.HitRate)
				}
				if resp.Size != 1000 {
					t.Errorf("expected Size 1000, got %d", resp.Size)
				}
				if resp.MaxSize != 10000 {
					t.Errorf("expected MaxSize 10000, got %d", resp.MaxSize)
				}
				if resp.Evictions != 50 {
					t.Errorf("expected Evictions 50, got %d", resp.Evictions)
				}
				if resp.Hits != 8500 {
					t.Errorf("expected Hits 8500, got %d", resp.Hits)
				}
				if resp.Misses != 1500 {
					t.Errorf("expected Misses 1500, got %d", resp.Misses)
				}
			},
		},
		{
			name: "stats with entity type filter",
			ctx:  context.Background(),
			req:  &GetCacheStatsRequest{EntityType: "users"},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					statsReq, ok := req.(*GetCacheStatsRequest)
					if !ok {
						t.Error("expected GetCacheStatsRequest")
					}
					if statsReq.EntityType != "users" {
						t.Errorf("expected entity type 'users', got %q", statsReq.EntityType)
					}
					return &GetCacheStatsResponse{
						HitRate: 0.90,
						Size:    500,
						MaxSize: 10000,
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *GetCacheStatsResponse) {
				if resp.HitRate != 0.90 {
					t.Errorf("expected HitRate 0.90, got %f", resp.HitRate)
				}
			},
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &GetCacheStatsRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			req:  &GetCacheStatsRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response from server",
			ctx:  context.Background(),
			req:  &GetCacheStatsRequest{},
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
			req:  &GetCacheStatsRequest{},
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
			c := NewClient(tt.transport)
			resp, err := c.GetCacheStats(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCacheStats() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, resp)
			}
		})
	}
}

func TestClient_ClearCache(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		req       *ClearCacheRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		validate  func(*testing.T, *ClearCacheResponse)
	}{
		{
			name: "clear all cache entries",
			ctx:  context.Background(),
			req:  &ClearCacheRequest{All: true},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ClearCache" {
						t.Errorf("expected method %q, got %q", "ClearCache", method)
					}
					clearReq, ok := req.(*ClearCacheRequest)
					if !ok {
						t.Error("expected ClearCacheRequest")
					}
					if !clearReq.All {
						t.Error("expected All to be true")
					}
					return &ClearCacheResponse{EntriesCleared: 1000}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *ClearCacheResponse) {
				if resp.EntriesCleared != 1000 {
					t.Errorf("expected EntriesCleared 1000, got %d", resp.EntriesCleared)
				}
			},
		},
		{
			name: "clear cache by entity type",
			ctx:  context.Background(),
			req:  &ClearCacheRequest{EntityType: "users"},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					clearReq, ok := req.(*ClearCacheRequest)
					if !ok {
						t.Error("expected ClearCacheRequest")
					}
					if clearReq.EntityType != "users" {
						t.Errorf("expected entity type 'users', got %q", clearReq.EntityType)
					}
					return &ClearCacheResponse{EntriesCleared: 250}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *ClearCacheResponse) {
				if resp.EntriesCleared != 250 {
					t.Errorf("expected EntriesCleared 250, got %d", resp.EntriesCleared)
				}
			},
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &ClearCacheRequest{All: true},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "missing entity type and all flag",
			ctx:       context.Background(),
			req:       &ClearCacheRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			req:  &ClearCacheRequest{All: true},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response from server",
			ctx:  context.Background(),
			req:  &ClearCacheRequest{All: true},
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
			req:  &ClearCacheRequest{All: true},
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
			c := NewClient(tt.transport)
			resp, err := c.ClearCache(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ClearCache() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, resp)
			}
		})
	}
}

func TestClient_GetCacheConfig(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		req       *GetCacheConfigRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		validate  func(*testing.T, *GetCacheConfigResponse)
	}{
		{
			name: "successful config retrieval",
			ctx:  context.Background(),
			req:  &GetCacheConfigRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetCacheConfig" {
						t.Errorf("expected method %q, got %q", "GetCacheConfig", method)
					}
					return &GetCacheConfigResponse{
						L1Size:          10000,
						L1TTL:           5 * time.Minute,
						CleanupInterval: 1 * time.Minute,
						Enabled:         true,
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *GetCacheConfigResponse) {
				if resp.L1Size != 10000 {
					t.Errorf("expected L1Size 10000, got %d", resp.L1Size)
				}
				if resp.L1TTL != 5*time.Minute {
					t.Errorf("expected L1TTL 5m, got %v", resp.L1TTL)
				}
				if resp.CleanupInterval != 1*time.Minute {
					t.Errorf("expected CleanupInterval 1m, got %v", resp.CleanupInterval)
				}
				if !resp.Enabled {
					t.Error("expected Enabled true")
				}
			},
		},
		{
			name: "disabled cache config",
			ctx:  context.Background(),
			req:  &GetCacheConfigRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &GetCacheConfigResponse{
						L1Size:          0,
						L1TTL:           0,
						CleanupInterval: 0,
						Enabled:         false,
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *GetCacheConfigResponse) {
				if resp.Enabled {
					t.Error("expected Enabled false")
				}
			},
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &GetCacheConfigRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			req:  &GetCacheConfigRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewTimeout("test", "request timed out")
				},
			},
			wantErr: true,
			errCode: errors.CodeTimeout,
		},
		{
			name: "nil response from server",
			ctx:  context.Background(),
			req:  &GetCacheConfigRequest{},
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
			req:  &GetCacheConfigRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return struct{}{}, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.transport)
			resp, err := c.GetCacheConfig(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCacheConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, resp)
			}
		})
	}
}

func TestClient_UpdateCacheConfig(t *testing.T) {
	intPtr := func(v int) *int { return &v }
	durPtr := func(v time.Duration) *time.Duration { return &v }

	tests := []struct {
		name      string
		ctx       context.Context
		req       *UpdateCacheConfigRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		validate  func(*testing.T, *UpdateCacheConfigResponse)
	}{
		{
			name: "update L1 size",
			ctx:  context.Background(),
			req:  &UpdateCacheConfigRequest{L1Size: intPtr(20000)},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "UpdateCacheConfig" {
						t.Errorf("expected method %q, got %q", "UpdateCacheConfig", method)
					}
					updateReq, ok := req.(*UpdateCacheConfigRequest)
					if !ok {
						t.Error("expected UpdateCacheConfigRequest")
					}
					if updateReq.L1Size == nil || *updateReq.L1Size != 20000 {
						t.Error("expected L1Size to be 20000")
					}
					return &UpdateCacheConfigResponse{
						Updated: true,
						NewConfig: &GetCacheConfigResponse{
							L1Size:          20000,
							L1TTL:           5 * time.Minute,
							CleanupInterval: 1 * time.Minute,
							Enabled:         true,
						},
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *UpdateCacheConfigResponse) {
				if !resp.Updated {
					t.Error("expected Updated true")
				}
				if resp.NewConfig == nil {
					t.Fatal("expected non-nil NewConfig")
				}
				if resp.NewConfig.L1Size != 20000 {
					t.Errorf("expected L1Size 20000, got %d", resp.NewConfig.L1Size)
				}
			},
		},
		{
			name: "update L1 TTL",
			ctx:  context.Background(),
			req:  &UpdateCacheConfigRequest{L1TTL: durPtr(10 * time.Minute)},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					updateReq, ok := req.(*UpdateCacheConfigRequest)
					if !ok {
						t.Error("expected UpdateCacheConfigRequest")
					}
					if updateReq.L1TTL == nil || *updateReq.L1TTL != 10*time.Minute {
						t.Error("expected L1TTL to be 10m")
					}
					return &UpdateCacheConfigResponse{
						Updated: true,
						NewConfig: &GetCacheConfigResponse{
							L1Size:          10000,
							L1TTL:           10 * time.Minute,
							CleanupInterval: 1 * time.Minute,
							Enabled:         true,
						},
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *UpdateCacheConfigResponse) {
				if resp.NewConfig.L1TTL != 10*time.Minute {
					t.Errorf("expected L1TTL 10m, got %v", resp.NewConfig.L1TTL)
				}
			},
		},
		{
			name: "update cleanup interval",
			ctx:  context.Background(),
			req:  &UpdateCacheConfigRequest{CleanupInterval: durPtr(2 * time.Minute)},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &UpdateCacheConfigResponse{
						Updated: true,
						NewConfig: &GetCacheConfigResponse{
							L1Size:          10000,
							L1TTL:           5 * time.Minute,
							CleanupInterval: 2 * time.Minute,
							Enabled:         true,
						},
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *UpdateCacheConfigResponse) {
				if resp.NewConfig.CleanupInterval != 2*time.Minute {
					t.Errorf("expected CleanupInterval 2m, got %v", resp.NewConfig.CleanupInterval)
				}
			},
		},
		{
			name: "update multiple fields",
			ctx:  context.Background(),
			req: &UpdateCacheConfigRequest{
				L1Size:          intPtr(15000),
				L1TTL:           durPtr(7 * time.Minute),
				CleanupInterval: durPtr(30 * time.Second),
			},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &UpdateCacheConfigResponse{
						Updated: true,
						NewConfig: &GetCacheConfigResponse{
							L1Size:          15000,
							L1TTL:           7 * time.Minute,
							CleanupInterval: 30 * time.Second,
							Enabled:         true,
						},
					}, nil
				},
			},
			wantErr: false,
			validate: func(t *testing.T, resp *UpdateCacheConfigResponse) {
				if resp.NewConfig.L1Size != 15000 {
					t.Errorf("expected L1Size 15000, got %d", resp.NewConfig.L1Size)
				}
				if resp.NewConfig.L1TTL != 7*time.Minute {
					t.Errorf("expected L1TTL 7m, got %v", resp.NewConfig.L1TTL)
				}
				if resp.NewConfig.CleanupInterval != 30*time.Second {
					t.Errorf("expected CleanupInterval 30s, got %v", resp.NewConfig.CleanupInterval)
				}
			},
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &UpdateCacheConfigRequest{L1Size: intPtr(10000)},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "no fields specified",
			ctx:       context.Background(),
			req:       &UpdateCacheConfigRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "negative L1 size",
			ctx:       context.Background(),
			req:       &UpdateCacheConfigRequest{L1Size: intPtr(-100)},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "negative L1 TTL",
			ctx:       context.Background(),
			req:       &UpdateCacheConfigRequest{L1TTL: durPtr(-1 * time.Second)},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "negative cleanup interval",
			ctx:       context.Background(),
			req:       &UpdateCacheConfigRequest{CleanupInterval: durPtr(-1 * time.Second)},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			req:  &UpdateCacheConfigRequest{L1Size: intPtr(10000)},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewInvalidInput("test", "invalid configuration")
				},
			},
			wantErr: true,
			errCode: errors.CodeInvalidInput,
		},
		{
			name: "nil response from server",
			ctx:  context.Background(),
			req:  &UpdateCacheConfigRequest{L1Size: intPtr(10000)},
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
			req:  &UpdateCacheConfigRequest{L1Size: intPtr(10000)},
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
			c := NewClient(tt.transport)
			resp, err := c.UpdateCacheConfig(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateCacheConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, resp)
			}
		})
	}
}

func TestClearCacheRequest_Validation(t *testing.T) {
	// Test that both EntityType and All can be set together
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			clearReq, ok := req.(*ClearCacheRequest)
			if !ok {
				t.Error("expected ClearCacheRequest")
			}
			if clearReq.EntityType != "users" {
				t.Errorf("expected entity type 'users', got %q", clearReq.EntityType)
			}
			if !clearReq.All {
				t.Error("expected All to be true")
			}
			return &ClearCacheResponse{EntriesCleared: 500}, nil
		},
	})

	resp, err := c.ClearCache(context.Background(), &ClearCacheRequest{
		EntityType: "users",
		All:        true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EntriesCleared != 500 {
		t.Errorf("expected EntriesCleared 500, got %d", resp.EntriesCleared)
	}
}

func TestGetCacheStatsResponse_ZeroValues(t *testing.T) {
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetCacheStatsResponse{
				HitRate:   0.0,
				Size:      0,
				MaxSize:   0,
				Evictions: 0,
				Hits:      0,
				Misses:    0,
			}, nil
		},
	})

	resp, err := c.GetCacheStats(context.Background(), &GetCacheStatsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.HitRate != 0.0 {
		t.Errorf("expected HitRate 0.0, got %f", resp.HitRate)
	}
	if resp.Size != 0 {
		t.Errorf("expected Size 0, got %d", resp.Size)
	}
}

func TestUpdateCacheConfig_ZeroValues(t *testing.T) {
	intPtr := func(v int) *int { return &v }
	durPtr := func(v time.Duration) *time.Duration { return &v }

	// Zero is a valid value for L1Size (disables cache)
	c := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &UpdateCacheConfigResponse{
				Updated: true,
				NewConfig: &GetCacheConfigResponse{
					L1Size:  0,
					Enabled: false,
				},
			}, nil
		},
	})

	resp, err := c.UpdateCacheConfig(context.Background(), &UpdateCacheConfigRequest{
		L1Size: intPtr(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NewConfig.L1Size != 0 {
		t.Errorf("expected L1Size 0, got %d", resp.NewConfig.L1Size)
	}

	// Zero duration is valid
	c2 := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &UpdateCacheConfigResponse{
				Updated: true,
				NewConfig: &GetCacheConfigResponse{
					L1TTL:           0,
					CleanupInterval: 0,
				},
			}, nil
		},
	})

	resp2, err := c2.UpdateCacheConfig(context.Background(), &UpdateCacheConfigRequest{
		L1TTL:           durPtr(0),
		CleanupInterval: durPtr(0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp2.NewConfig.L1TTL != 0 {
		t.Errorf("expected L1TTL 0, got %v", resp2.NewConfig.L1TTL)
	}
}
