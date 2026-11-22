// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package backup

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	svc "github.com/jeremyhahn/go-quicbaas/sdk/go/service"
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

func TestClient_Create_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.CreateBackupResponse{
				BackupID:  "backup-123",
				Size:      1024,
				Timestamp: time.Now().UnixNano(),
				Status:    "completed",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BackupRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty request is valid",
			req:     &BackupRequest{},
			wantErr: false,
		},
		{
			name:    "local destination refused",
			req:     &BackupRequest{Destination: "/backups/"},
			wantErr: true,
		},
		{
			name:    "s3 destination refused",
			req:     &BackupRequest{Destination: "s3://bucket/path"},
			wantErr: true,
		},
		{
			name:    "compression refused",
			req:     &BackupRequest{Compress: true},
			wantErr: true,
		},
		{
			name:    "encryption refused",
			req:     &BackupRequest{Encrypt: true},
			wantErr: true,
		},
		{
			name:    "compression and encryption refused",
			req:     &BackupRequest{Compress: true, Encrypt: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Create(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Create(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful backup",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Create" {
						t.Errorf("expected method %q, got %q", "Create", method)
					}
					if _, ok := req.(*svc.CreateBackupRequest); !ok {
						t.Errorf("expected *svc.CreateBackupRequest, got %T", req)
					}
					return &svc.CreateBackupResponse{
						BackupID:  "backup-abc123",
						Size:      1048576,
						Timestamp: now.UnixNano(),
						Status:    "completed",
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
		{
			name: "typed nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return (*svc.CreateBackupResponse)(nil), nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &BackupRequest{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Create(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_Restore_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.RestoreBackupResponse{
				Status: "completed",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RestoreRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty backup ID and source",
			req:     &RestoreRequest{BackupID: "", Source: ""},
			wantErr: true,
		},
		{
			name:    "valid request with backup ID only",
			req:     &RestoreRequest{BackupID: "backup-123"},
			wantErr: false,
		},
		{
			name:    "source only is refused",
			req:     &RestoreRequest{Source: "s3://bucket/backup"},
			wantErr: true,
		},
		{
			name:    "backup ID with source is refused",
			req:     &RestoreRequest{BackupID: "backup-123", Source: "s3://bucket/backup"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Restore(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Restore() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Restore(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		restored  bool
	}{
		{
			name: "successful restore",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Restore" {
						t.Errorf("expected method %q, got %q", "Restore", method)
					}
					return &svc.RestoreBackupResponse{
						Status: "completed",
					}, nil
				},
			},
			wantErr:  false,
			restored: true,
		},
		{
			name: "restore in progress",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &svc.RestoreBackupResponse{
						Status: "running",
					}, nil
				},
			},
			wantErr:  false,
			restored: false,
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
			name: "backup not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "backup not found")
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
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &RestoreRequest{BackupID: "backup-123"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Restore(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Restore() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp.Restored != tt.restored {
					t.Errorf("expected restored=%v, got %v", tt.restored, resp.Restored)
				}
			}
		})
	}
}

func TestClient_List(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		ctx         context.Context
		transport   *mockTransport
		wantErr     bool
		errCode     errors.ErrorCode
		backupCount int
		wantFirst   *BackupInfo
	}{
		{
			name: "list with backups",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "List" {
						t.Errorf("expected method %q, got %q", "List", method)
					}
					if _, ok := req.(*svc.ListBackupsRequest); !ok {
						t.Errorf("expected *svc.ListBackupsRequest, got %T", req)
					}
					return &svc.ListBackupsResponse{
						Backups: []*svc.BackupMetadata{
							{BackupID: "backup-1", Size: 1024, Timestamp: now.UnixNano()},
							nil,
							{BackupID: "backup-2", Size: 2048, Timestamp: now.UnixNano()},
							{BackupID: "backup-3", Size: 4096, Timestamp: now.UnixNano()},
						},
						TotalCount: 3,
					}, nil
				},
			},
			wantErr:     false,
			backupCount: 3,
			wantFirst:   &BackupInfo{ID: "backup-1", Size: 1024, CreatedAt: now},
		},
		{
			name: "typed nil response returns empty list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return (*svc.ListBackupsResponse)(nil), nil
				},
			},
			wantErr:     false,
			backupCount: 0,
		},
		{
			name: "list with empty result",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &svc.ListBackupsResponse{
						Backups: []*svc.BackupMetadata{},
					}, nil
				},
			},
			wantErr:     false,
			backupCount: 0,
		},
		{
			name: "nil response returns empty list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:     false,
			backupCount: 0,
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
			resp, err := client.List(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp == nil {
					t.Fatal("expected non-nil response")
				}
				if len(resp.Backups) != tt.backupCount {
					t.Errorf("expected %d backups, got %d", tt.backupCount, len(resp.Backups))
				}
				if tt.wantFirst != nil && len(resp.Backups) > 0 {
					got := resp.Backups[0]
					if got.ID != tt.wantFirst.ID || got.Size != tt.wantFirst.Size || !got.CreatedAt.Equal(tt.wantFirst.CreatedAt) {
						t.Errorf("first backup = %+v, want %+v", got, *tt.wantFirst)
					}
				}
			}
		})
	}
}

func TestBackupResponse_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.CreateBackupResponse{
				BackupID:  "backup-xyz789",
				Size:      10485760,
				Timestamp: now.UnixNano(),
				Status:    "completed",
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Create(context.Background(), &BackupRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.BackupID != "backup-xyz789" {
		t.Errorf("expected BackupID='backup-xyz789', got %q", resp.BackupID)
	}
	if resp.Size != 10485760 {
		t.Errorf("expected Size=10485760, got %d", resp.Size)
	}
	if resp.Duration != 0 {
		t.Errorf("expected Duration=0, got %v", resp.Duration)
	}
	if want := time.Unix(0, now.UnixNano()); !resp.CreatedAt.Equal(want) {
		t.Errorf("expected CreatedAt=%v, got %v", want, resp.CreatedAt)
	}
	if resp.Status != "completed" {
		t.Errorf("expected Status='completed', got %q", resp.Status)
	}
}

func TestClient_Create_SizeClampsAndZeroTimestamp(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.CreateBackupResponse{
				BackupID:  "backup-big",
				Size:      math.MaxUint64,
				Timestamp: 0,
				Status:    "completed",
			}, nil
		},
	}

	resp, err := NewClient(mockT).Create(context.Background(), &BackupRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Size != math.MaxInt64 {
		t.Errorf("expected Size=%d, got %d", int64(math.MaxInt64), resp.Size)
	}
	if !resp.CreatedAt.IsZero() {
		t.Errorf("expected zero CreatedAt, got %v", resp.CreatedAt)
	}
}

func TestRestoreResponse_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.RestoreBackupResponse{
				JobID:  "restore-backup-123",
				Status: "completed",
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Restore(context.Background(), &RestoreRequest{BackupID: "backup-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Restored {
		t.Error("expected Restored to be true")
	}
	if resp.JobID != "restore-backup-123" {
		t.Errorf("expected JobID=restore-backup-123, got %q", resp.JobID)
	}
	if resp.Status != "completed" {
		t.Errorf("expected Status=completed, got %q", resp.Status)
	}
}

func TestRestoreResponse_RunningRestoreIsNotAnError(t *testing.T) {
	client := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.RestoreBackupResponse{JobID: "restore-backup-123", Status: "running"}, nil
		},
	})

	resp, err := client.Restore(context.Background(), &RestoreRequest{BackupID: "backup-123"})
	if err != nil {
		t.Fatalf("an accepted, running restore is not an error: %v", err)
	}
	if resp.Restored {
		t.Error("a running restore must not report Restored")
	}
	if resp.Status != "running" {
		t.Errorf("expected Status=running, got %q", resp.Status)
	}
}

func TestBackupInfo_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.ListBackupsResponse{
				Backups: []*svc.BackupMetadata{
					{
						BackupID:  "backup-abc",
						Size:      5242880,
						Timestamp: now.UnixNano(),
						Status:    "completed",
					},
				},
				TotalCount: 1,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(resp.Backups))
	}

	backup := resp.Backups[0]
	if backup.ID != "backup-abc" {
		t.Errorf("expected ID='backup-abc', got %q", backup.ID)
	}
	if backup.Size != 5242880 {
		t.Errorf("expected Size=5242880, got %d", backup.Size)
	}
	if want := time.Unix(0, now.UnixNano()); !backup.CreatedAt.Equal(want) {
		t.Errorf("expected CreatedAt=%v, got %v", want, backup.CreatedAt)
	}
	if backup.Encrypted {
		t.Error("expected Encrypted to be false")
	}
}

func TestClient_Create_RequestValidation(t *testing.T) {
	var captured interface{}
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			captured = req
			return &svc.CreateBackupResponse{
				BackupID:  "backup-123",
				Size:      1024,
				Timestamp: time.Now().UnixNano(),
				Status:    "completed",
			}, nil
		},
	}

	client := NewClient(mockT)
	if _, err := client.Create(context.Background(), &BackupRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	capturedReq, ok := captured.(*svc.CreateBackupRequest)
	if !ok {
		t.Fatalf("expected *svc.CreateBackupRequest, got %T", captured)
	}
	if !reflect.DeepEqual(capturedReq, &svc.CreateBackupRequest{}) {
		t.Errorf("expected an empty CreateBackupRequest, got %+v", capturedReq)
	}
}

func TestClient_Restore_RequestValidation(t *testing.T) {
	var capturedReq *svc.RestoreBackupRequest
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedReq = req.(*svc.RestoreBackupRequest)
			return &svc.RestoreBackupResponse{
				Status: "completed",
			}, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	originalReq := &RestoreRequest{BackupID: "backup-456"}

	_, err := client.Restore(ctx, originalReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.BackupID != originalReq.BackupID {
		t.Errorf("expected BackupID=%q, got %q", originalReq.BackupID, capturedReq.BackupID)
	}
}

func TestClient_Restore_SourceRefusedBeforeCall(t *testing.T) {
	called := false
	client := NewClient(&mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			called = true
			return &svc.RestoreBackupResponse{Status: "completed"}, nil
		},
	})

	_, err := client.Restore(context.Background(), &RestoreRequest{BackupID: "backup-456", Source: "nightly"})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected an invalid-input error, got %v", err)
	}
	if called {
		t.Error("a source restore must be refused before any call")
	}
}

func TestClient_List_ServiceAndMethod(t *testing.T) {
	var capturedService, capturedMethod string
	var capturedReq interface{}
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedService = service
			capturedMethod = method
			capturedReq = req
			return &svc.ListBackupsResponse{Backups: []*svc.BackupMetadata{}}, nil
		},
	}

	client := NewClient(mockT)
	_, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedService != serviceName {
		t.Errorf("expected service=%q, got %q", serviceName, capturedService)
	}
	if capturedMethod != "List" {
		t.Errorf("expected method='List', got %q", capturedMethod)
	}
	if _, ok := capturedReq.(*svc.ListBackupsRequest); !ok {
		t.Errorf("expected *svc.ListBackupsRequest, got %T", capturedReq)
	}
}

func TestValidateBackupRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *BackupRequest
		wantMsg string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantMsg: "request is required",
		},
		{
			name: "empty request",
			req:  &BackupRequest{},
		},
		{
			name:    "destination",
			req:     &BackupRequest{Destination: "/path/to/backup"},
			wantMsg: "destination is not supported: the backup service writes to its configured backup storage",
		},
		{
			name:    "compress",
			req:     &BackupRequest{Compress: true},
			wantMsg: "compression is not supported: the backup service applies its configured settings",
		},
		{
			name:    "encrypt",
			req:     &BackupRequest{Encrypt: true},
			wantMsg: "encryption is not supported: the backup service applies its configured settings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBackupRequest(tt.req)
			if tt.wantMsg == "" {
				if err != nil {
					t.Errorf("validateBackupRequest() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateBackupRequest() = nil, want %q", tt.wantMsg)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("validateBackupRequest() error = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestValidateRestoreRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *RestoreRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty backup ID and source",
			req:     &RestoreRequest{BackupID: "", Source: ""},
			wantErr: true,
		},
		{
			name:    "backup ID only",
			req:     &RestoreRequest{BackupID: "backup-123", Source: ""},
			wantErr: false,
		},
		{
			name:    "source only is refused",
			req:     &RestoreRequest{BackupID: "", Source: "/path/to/backup"},
			wantErr: true,
		},
		{
			name:    "backup ID with source is refused",
			req:     &RestoreRequest{BackupID: "backup-123", Source: "/path/to/backup"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRestoreRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRestoreRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_MultipleBackups(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &svc.ListBackupsResponse{
				Backups: []*svc.BackupMetadata{
					{BackupID: "backup-1", Size: 100, Timestamp: now.Add(-2 * time.Hour).UnixNano()},
					{BackupID: "backup-2", Size: 200, Timestamp: now.Add(-1 * time.Hour).UnixNano()},
					{BackupID: "backup-3", Size: 300, Timestamp: now.UnixNano()},
				},
				TotalCount: 3,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(resp.Backups))
	}

	// Verify order and content
	expectedIDs := []string{"backup-1", "backup-2", "backup-3"}
	expectedSizes := []int64{100, 200, 300}

	for i, backup := range resp.Backups {
		if backup.ID != expectedIDs[i] {
			t.Errorf("backup[%d]: expected ID=%q, got %q", i, expectedIDs[i], backup.ID)
		}
		if backup.Size != expectedSizes[i] {
			t.Errorf("backup[%d]: expected Size=%d, got %d", i, expectedSizes[i], backup.Size)
		}
	}
}
