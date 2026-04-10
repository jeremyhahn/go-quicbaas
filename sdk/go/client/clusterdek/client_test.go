// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package clusterdek

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

// =============================================================================
// InitializeClusterDEK Tests
// =============================================================================

func TestClient_InitializeClusterDEK_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &InitClusterDEKResponse{
				Version:     1,
				CreatedAt:   time.Now(),
				NodesSynced: 3,
				Algorithm:   "AES-256-GCM",
				KeySize:     256,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *InitClusterDEKRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty algorithm",
			req:     &InitClusterDEKRequest{Algorithm: "", KeySize: 256},
			wantErr: true,
		},
		{
			name:    "zero key size",
			req:     &InitClusterDEKRequest{Algorithm: "AES-256-GCM", KeySize: 0},
			wantErr: true,
		},
		{
			name:    "negative key size",
			req:     &InitClusterDEKRequest{Algorithm: "AES-256-GCM", KeySize: -128},
			wantErr: true,
		},
		{
			name:    "invalid key size",
			req:     &InitClusterDEKRequest{Algorithm: "AES-256-GCM", KeySize: 100},
			wantErr: true,
		},
		{
			name: "rotation policy with zero interval",
			req: &InitClusterDEKRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
				RotationPolicy: &DEKRotationPolicy{
					Enabled:          true,
					RotationInterval: 0,
				},
			},
			wantErr: true,
		},
		{
			name:    "valid request 128-bit",
			req:     &InitClusterDEKRequest{Algorithm: "AES-128-GCM", KeySize: 128},
			wantErr: false,
		},
		{
			name:    "valid request 256-bit",
			req:     &InitClusterDEKRequest{Algorithm: "AES-256-GCM", KeySize: 256},
			wantErr: false,
		},
		{
			name: "valid request with rotation policy",
			req: &InitClusterDEKRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
				RotationPolicy: &DEKRotationPolicy{
					Enabled:          true,
					RotationInterval: 24 * time.Hour,
					AutoReencrypt:    true,
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with disabled rotation policy",
			req: &InitClusterDEKRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
				RotationPolicy: &DEKRotationPolicy{
					Enabled:          false,
					RotationInterval: 0,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.InitializeClusterDEK(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitializeClusterDEK() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_InitializeClusterDEK(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful init",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "InitializeClusterDEK" {
						t.Errorf("expected method %q, got %q", "InitializeClusterDEK", method)
					}
					return &InitClusterDEKResponse{
						Version:     1,
						CreatedAt:   time.Now(),
						NodesSynced: 3,
						Algorithm:   "AES-256-GCM",
						KeySize:     256,
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
			name: "already initialized",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewAlreadyExists("test", "already initialized")
				},
			},
			wantErr: true,
			errCode: errors.CodeAlreadyExists,
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
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &InitClusterDEKRequest{Algorithm: "AES-256-GCM", KeySize: 256}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.InitializeClusterDEK(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("InitializeClusterDEK() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// GetClusterDEKStatus Tests
// =============================================================================

func TestClient_GetClusterDEKStatus(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful status",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetClusterDEKStatus" {
						t.Errorf("expected method %q, got %q", "GetClusterDEKStatus", method)
					}
					now := time.Now()
					return &ClusterDEKStatusResponse{
						Initialized:            true,
						CurrentVersion:         2,
						CreatedAt:              now.Add(-24 * time.Hour),
						RotatedAt:              &now,
						NodesSynced:            3,
						TotalNodes:             3,
						Algorithm:              "AES-256-GCM",
						KeySize:                256,
						ReencryptionInProgress: false,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "not initialized",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &ClusterDEKStatusResponse{
						Initialized: false,
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
			resp, err := client.GetClusterDEKStatus(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetClusterDEKStatus() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// RotateClusterDEK Tests
// =============================================================================

func TestClient_RotateClusterDEK_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RotateClusterDEKResponse{
				NewVersion:  2,
				OldVersion:  1,
				RotatedAt:   time.Now(),
				RotationID:  "rotation-123",
				NodesSynced: 3,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RotateClusterDEKRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty reason",
			req:     &RotateClusterDEKRequest{Reason: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RotateClusterDEKRequest{Reason: "Scheduled rotation"},
			wantErr: false,
		},
		{
			name: "valid request with auto reencryption",
			req: &RotateClusterDEKRequest{
				Reason:                "Security policy",
				AutoStartReencryption: true,
			},
			wantErr: false,
		},
		{
			name: "valid request with scheduled time",
			req: &RotateClusterDEKRequest{
				Reason:        "Scheduled rotation",
				ScheduledTime: func() *time.Time { t := time.Now().Add(24 * time.Hour); return &t }(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RotateClusterDEK(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RotateClusterDEK() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RotateClusterDEK(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful rotation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RotateClusterDEK" {
						t.Errorf("expected method %q, got %q", "RotateClusterDEK", method)
					}
					return &RotateClusterDEKResponse{
						NewVersion:                2,
						OldVersion:                1,
						RotatedAt:                 time.Now(),
						RotationID:                "rotation-456",
						NodesSynced:               3,
						ReencryptionStarted:       true,
						EstimatedReencryptionTime: 30 * time.Minute,
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
			name: "rotation in progress",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewConflict("test", "rotation already in progress")
				},
			},
			wantErr: true,
			errCode: errors.CodeConflict,
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

	validReq := &RotateClusterDEKRequest{Reason: "Test rotation"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RotateClusterDEK(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("RotateClusterDEK() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// SyncNodeDEK Tests
// =============================================================================

func TestClient_SyncNodeDEK_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SyncNodeDEKResponse{
				NodeID:         1,
				VersionsSynced: []uint64{1, 2},
				CurrentVersion: 2,
				SyncedAt:       time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SyncNodeDEKRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "zero node ID",
			req:     &SyncNodeDEKRequest{NodeID: 0},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &SyncNodeDEKRequest{NodeID: 1},
			wantErr: false,
		},
		{
			name:    "valid request with current version",
			req:     &SyncNodeDEKRequest{NodeID: 1, CurrentVersion: 1},
			wantErr: false,
		},
		{
			name:    "valid request with force resync",
			req:     &SyncNodeDEKRequest{NodeID: 1, ForceResync: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SyncNodeDEK(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SyncNodeDEK() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_SyncNodeDEK(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful sync",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "SyncNodeDEK" {
						t.Errorf("expected method %q, got %q", "SyncNodeDEK", method)
					}
					return &SyncNodeDEKResponse{
						NodeID:         1,
						VersionsSynced: []uint64{1, 2},
						CurrentVersion: 2,
						SyncedAt:       time.Now(),
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
			name: "node not in cluster",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "node not in cluster")
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
					return []byte{}, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &SyncNodeDEKRequest{NodeID: 1}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.SyncNodeDEK(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("SyncNodeDEK() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// GetNodeDEKStatus Tests
// =============================================================================

func TestClient_GetNodeDEKStatus_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetNodeDEKStatusResponse{
				NodeID:            1,
				NodeName:          "node-1",
				CurrentVersion:    2,
				LastSyncedAt:      time.Now(),
				Status:            NodeDEKSynced,
				VersionsAvailable: []uint64{1, 2},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetNodeDEKStatusRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "zero node ID",
			req:     &GetNodeDEKStatusRequest{NodeID: 0},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetNodeDEKStatusRequest{NodeID: 1},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetNodeDEKStatus(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNodeDEKStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetNodeDEKStatus(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful status",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetNodeDEKStatus" {
						t.Errorf("expected method %q, got %q", "GetNodeDEKStatus", method)
					}
					return &GetNodeDEKStatusResponse{
						NodeID:            1,
						NodeName:          "node-1",
						CurrentVersion:    2,
						LastSyncedAt:      time.Now(),
						Status:            NodeDEKSynced,
						VersionsAvailable: []uint64{1, 2},
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
			name: "node not found",
			ctx:  context.Background(),
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
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &GetNodeDEKStatusRequest{NodeID: 1}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetNodeDEKStatus(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNodeDEKStatus() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// StartReencryption Tests
// =============================================================================

func TestClient_StartReencryption_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &StartReencryptionResponse{
				OperationID:   "reencrypt-123",
				Started:       true,
				TotalEntities: 1000,
				StartedAt:     time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *StartReencryptionRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "zero from version",
			req:     &StartReencryptionRequest{FromVersion: 0, ToVersion: 2},
			wantErr: true,
		},
		{
			name:    "zero to version",
			req:     &StartReencryptionRequest{FromVersion: 1, ToVersion: 0},
			wantErr: true,
		},
		{
			name:    "to version not greater than from version",
			req:     &StartReencryptionRequest{FromVersion: 2, ToVersion: 1},
			wantErr: true,
		},
		{
			name:    "equal versions",
			req:     &StartReencryptionRequest{FromVersion: 2, ToVersion: 2},
			wantErr: true,
		},
		{
			name:    "negative batch size",
			req:     &StartReencryptionRequest{FromVersion: 1, ToVersion: 2, BatchSize: -1},
			wantErr: true,
		},
		{
			name:    "negative concurrency",
			req:     &StartReencryptionRequest{FromVersion: 1, ToVersion: 2, Concurrency: -1},
			wantErr: true,
		},
		{
			name:    "negative throttle rate",
			req:     &StartReencryptionRequest{FromVersion: 1, ToVersion: 2, ThrottleRate: -1},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &StartReencryptionRequest{FromVersion: 1, ToVersion: 2},
			wantErr: false,
		},
		{
			name: "valid request with options",
			req: &StartReencryptionRequest{
				FromVersion:  1,
				ToVersion:    2,
				BatchSize:    100,
				Concurrency:  4,
				ThrottleRate: 1000,
			},
			wantErr: false,
		},
		{
			name:    "valid request with path",
			req:     &StartReencryptionRequest{FromVersion: 1, ToVersion: 2, Path: "/data/keys"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.StartReencryption(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("StartReencryption() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_StartReencryption(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful start",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "StartReencryption" {
						t.Errorf("expected method %q, got %q", "StartReencryption", method)
					}
					now := time.Now()
					return &StartReencryptionResponse{
						OperationID:         "reencrypt-456",
						Started:             true,
						TotalEntities:       5000,
						StartedAt:           now,
						EstimatedCompletion: now.Add(1 * time.Hour),
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
			name: "already running",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewConflict("test", "reencryption already running")
				},
			},
			wantErr: true,
			errCode: errors.CodeConflict,
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
					return map[string]string{}, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &StartReencryptionRequest{FromVersion: 1, ToVersion: 2}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.StartReencryption(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("StartReencryption() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// GetReencryptionStatus Tests
// =============================================================================

func TestClient_GetReencryptionStatus(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful status",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetReencryptionStatus" {
						t.Errorf("expected method %q, got %q", "GetReencryptionStatus", method)
					}
					return &ReencryptionStatusResponse{
						OperationID:       "reencrypt-789",
						State:             ReencryptionRunning,
						StartedAt:         time.Now().Add(-10 * time.Minute),
						Progress:          45.5,
						TotalEntities:     1000,
						ProcessedEntities: 455,
						FailedEntities:    2,
						ThroughputRate:    45.5,
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
			name: "no reencryption in progress",
			ctx:  context.Background(),
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
					return nil, errors.NewTimeout("test", "request timeout")
				},
			},
			wantErr: true,
			errCode: errors.CodeTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetReencryptionStatus(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetReencryptionStatus() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// CancelReencryption Tests
// =============================================================================

func TestClient_CancelReencryption(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful cancel",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "CancelReencryption" {
						t.Errorf("expected method %q, got %q", "CancelReencryption", method)
					}
					return &cancelReencryptionResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful cancel with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
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
			name: "no reencryption to cancel",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "no reencryption in progress")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.CancelReencryption(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("CancelReencryption() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

// =============================================================================
// RecoverDEK Tests
// =============================================================================

func TestClient_RecoverDEK_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RecoverDEKResponse{
				Success:          true,
				RecoveredVersion: 2,
				RecoveredAt:      time.Now(),
				NodesSynced:      3,
				AuditID:          "audit-123",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RecoverDEKRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty reason",
			req:     &RecoverDEKRequest{Reason: "", RecoveryMethod: RecoveryMethodKeys, RecoveryKeys: [][]byte{{1, 2, 3}}},
			wantErr: true,
		},
		{
			name:    "invalid recovery method",
			req:     &RecoverDEKRequest{Reason: "test", RecoveryMethod: "invalid"},
			wantErr: true,
		},
		{
			name:    "recovery_keys without keys",
			req:     &RecoverDEKRequest{Reason: "test", RecoveryMethod: RecoveryMethodKeys},
			wantErr: true,
		},
		{
			name:    "recovery_keys with empty key",
			req:     &RecoverDEKRequest{Reason: "test", RecoveryMethod: RecoveryMethodKeys, RecoveryKeys: [][]byte{{1, 2}, {}}},
			wantErr: true,
		},
		{
			name:    "backup without location",
			req:     &RecoverDEKRequest{Reason: "test", RecoveryMethod: RecoveryMethodBackup},
			wantErr: true,
		},
		{
			name:    "manual without key",
			req:     &RecoverDEKRequest{Reason: "test", RecoveryMethod: RecoveryMethodManual},
			wantErr: true,
		},
		{
			name: "valid recovery_keys request",
			req: &RecoverDEKRequest{
				Reason:         "Disaster recovery",
				RecoveryMethod: RecoveryMethodKeys,
				RecoveryKeys:   [][]byte{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
			},
			wantErr: false,
		},
		{
			name: "valid backup request",
			req: &RecoverDEKRequest{
				Reason:         "Disaster recovery",
				RecoveryMethod: RecoveryMethodBackup,
				BackupLocation: "s3://bucket/backup.enc",
			},
			wantErr: false,
		},
		{
			name: "valid manual request",
			req: &RecoverDEKRequest{
				Reason:         "Break-glass procedure",
				RecoveryMethod: RecoveryMethodManual,
				ManualKey:      []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RecoverDEK(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RecoverDEK() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RecoverDEK(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful recovery",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RecoverDEK" {
						t.Errorf("expected method %q, got %q", "RecoverDEK", method)
					}
					return &RecoverDEKResponse{
						Success:          true,
						RecoveredVersion: 2,
						RecoveredAt:      time.Now(),
						NodesSynced:      3,
						AuditID:          "audit-456",
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "recovery with warnings",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &RecoverDEKResponse{
						Success:          true,
						RecoveredVersion: 2,
						RecoveredAt:      time.Now(),
						NodesSynced:      2,
						AuditID:          "audit-789",
						Warnings:         []string{"Node 3 sync pending"},
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
			name: "invalid recovery keys",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnauthorized("test", "invalid recovery keys")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnauthorized,
		},
		{
			name: "threshold not met",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewForbidden("test", "recovery threshold not met")
				},
			},
			wantErr: true,
			errCode: errors.CodeForbidden,
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
					return struct{}{}, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &RecoverDEKRequest{
		Reason:         "Disaster recovery",
		RecoveryMethod: RecoveryMethodKeys,
		RecoveryKeys:   [][]byte{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RecoverDEK(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("RecoverDEK() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// Response Field Tests
// =============================================================================

func TestInitClusterDEKResponse_Fields(t *testing.T) {
	expectedTime := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &InitClusterDEKResponse{
				Version:     1,
				CreatedAt:   expectedTime,
				NodesSynced: 3,
				Algorithm:   "AES-256-GCM",
				KeySize:     256,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.InitializeClusterDEK(context.Background(), &InitClusterDEKRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Version != 1 {
		t.Errorf("expected Version=1, got %d", resp.Version)
	}
	if !resp.CreatedAt.Equal(expectedTime) {
		t.Errorf("expected CreatedAt=%v, got %v", expectedTime, resp.CreatedAt)
	}
	if resp.NodesSynced != 3 {
		t.Errorf("expected NodesSynced=3, got %d", resp.NodesSynced)
	}
	if resp.Algorithm != "AES-256-GCM" {
		t.Errorf("expected Algorithm='AES-256-GCM', got %q", resp.Algorithm)
	}
	if resp.KeySize != 256 {
		t.Errorf("expected KeySize=256, got %d", resp.KeySize)
	}
}

func TestClusterDEKStatusResponse_Fields(t *testing.T) {
	createdAt := time.Now().Add(-24 * time.Hour)
	rotatedAt := time.Now()
	nextRotation := time.Now().Add(24 * time.Hour)

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ClusterDEKStatusResponse{
				Initialized:    true,
				CurrentVersion: 2,
				CreatedAt:      createdAt,
				RotatedAt:      &rotatedAt,
				NextRotation:   &nextRotation,
				NodesSynced:    3,
				TotalNodes:     3,
				NodesOutOfSync: []NodeDEKInfo{},
				Algorithm:      "AES-256-GCM",
				KeySize:        256,
				PreviousVersions: []DEKVersionInfo{
					{Version: 1, CreatedAt: createdAt, DeprecatedAt: &rotatedAt, EntitiesEncrypted: 100, CanDelete: false},
				},
				ReencryptionInProgress: true,
				ReencryptionProgress:   45.5,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GetClusterDEKStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Initialized {
		t.Error("expected Initialized=true")
	}
	if resp.CurrentVersion != 2 {
		t.Errorf("expected CurrentVersion=2, got %d", resp.CurrentVersion)
	}
	if !resp.CreatedAt.Equal(createdAt) {
		t.Errorf("expected CreatedAt=%v, got %v", createdAt, resp.CreatedAt)
	}
	if resp.RotatedAt == nil || !resp.RotatedAt.Equal(rotatedAt) {
		t.Errorf("expected RotatedAt=%v, got %v", rotatedAt, resp.RotatedAt)
	}
	if resp.NextRotation == nil || !resp.NextRotation.Equal(nextRotation) {
		t.Errorf("expected NextRotation=%v, got %v", nextRotation, resp.NextRotation)
	}
	if resp.NodesSynced != 3 {
		t.Errorf("expected NodesSynced=3, got %d", resp.NodesSynced)
	}
	if resp.TotalNodes != 3 {
		t.Errorf("expected TotalNodes=3, got %d", resp.TotalNodes)
	}
	if len(resp.NodesOutOfSync) != 0 {
		t.Errorf("expected empty NodesOutOfSync, got %d items", len(resp.NodesOutOfSync))
	}
	if len(resp.PreviousVersions) != 1 {
		t.Errorf("expected 1 PreviousVersion, got %d", len(resp.PreviousVersions))
	}
	if resp.PreviousVersions[0].Version != 1 {
		t.Errorf("expected PreviousVersion[0].Version=1, got %d", resp.PreviousVersions[0].Version)
	}
	if !resp.ReencryptionInProgress {
		t.Error("expected ReencryptionInProgress=true")
	}
	if resp.ReencryptionProgress != 45.5 {
		t.Errorf("expected ReencryptionProgress=45.5, got %f", resp.ReencryptionProgress)
	}
}

func TestReencryptionStatusResponse_Fields(t *testing.T) {
	startedAt := time.Now().Add(-10 * time.Minute)
	estimatedCompletion := time.Now().Add(20 * time.Minute)

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ReencryptionStatusResponse{
				OperationID:         "reencrypt-test",
				State:               ReencryptionRunning,
				StartedAt:           startedAt,
				EstimatedCompletion: &estimatedCompletion,
				Progress:            33.33,
				TotalEntities:       3000,
				ProcessedEntities:   1000,
				FailedEntities:      5,
				ThroughputRate:      100.0,
				NodeProgress: []NodeReencryptionProgress{
					{
						NodeID:            1,
						NodeName:          "node-1",
						Progress:          50.0,
						ProcessedEntities: 500,
						TotalEntities:     1000,
						ThroughputRate:    50.0,
						LastUpdatedAt:     time.Now(),
					},
				},
				Errors: []ReencryptionError{
					{
						Timestamp: time.Now().Add(-5 * time.Minute),
						NodeID:    1,
						EntityID:  "entity-123",
						Error:     "temporary read error",
						Retried:   true,
					},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GetReencryptionStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.OperationID != "reencrypt-test" {
		t.Errorf("expected OperationID='reencrypt-test', got %q", resp.OperationID)
	}
	if resp.State != ReencryptionRunning {
		t.Errorf("expected State=ReencryptionRunning, got %v", resp.State)
	}
	if !resp.StartedAt.Equal(startedAt) {
		t.Errorf("expected StartedAt=%v, got %v", startedAt, resp.StartedAt)
	}
	if resp.Progress != 33.33 {
		t.Errorf("expected Progress=33.33, got %f", resp.Progress)
	}
	if resp.TotalEntities != 3000 {
		t.Errorf("expected TotalEntities=3000, got %d", resp.TotalEntities)
	}
	if resp.ProcessedEntities != 1000 {
		t.Errorf("expected ProcessedEntities=1000, got %d", resp.ProcessedEntities)
	}
	if resp.FailedEntities != 5 {
		t.Errorf("expected FailedEntities=5, got %d", resp.FailedEntities)
	}
	if resp.ThroughputRate != 100.0 {
		t.Errorf("expected ThroughputRate=100.0, got %f", resp.ThroughputRate)
	}
	if len(resp.NodeProgress) != 1 {
		t.Errorf("expected 1 NodeProgress, got %d", len(resp.NodeProgress))
	}
	if len(resp.Errors) != 1 {
		t.Errorf("expected 1 Error, got %d", len(resp.Errors))
	}
}

// =============================================================================
// Constant Value Tests
// =============================================================================

func TestNodeDEKSyncStatus_Values(t *testing.T) {
	tests := []struct {
		status   NodeDEKSyncStatus
		expected string
	}{
		{NodeDEKSynced, "synced"},
		{NodeDEKOutOfDate, "out_of_date"},
		{NodeDEKMissing, "missing"},
		{NodeDEKSyncFailed, "sync_failed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, string(tt.status))
		}
	}
}

func TestReencryptionState_Values(t *testing.T) {
	tests := []struct {
		state    ReencryptionState
		expected string
	}{
		{ReencryptionRunning, "running"},
		{ReencryptionPaused, "paused"},
		{ReencryptionCompleted, "completed"},
		{ReencryptionFailed, "failed"},
		{ReencryptionCancelled, "cancelled"},
	}

	for _, tt := range tests {
		if string(tt.state) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, string(tt.state))
		}
	}
}

func TestRecoveryMethod_Values(t *testing.T) {
	tests := []struct {
		method   RecoveryMethod
		expected string
	}{
		{RecoveryMethodKeys, "recovery_keys"},
		{RecoveryMethodBackup, "backup"},
		{RecoveryMethodManual, "manual"},
	}

	for _, tt := range tests {
		if string(tt.method) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, string(tt.method))
		}
	}
}
