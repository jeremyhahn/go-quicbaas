// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package backup

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

func TestClient_Create_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BackupResponse{
				BackupID:  "backup-123",
				Size:      1024,
				Duration:  time.Second,
				CreatedAt: time.Now(),
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
			name:    "empty destination",
			req:     &BackupRequest{Destination: ""},
			wantErr: true,
		},
		{
			name:    "valid request with local path",
			req:     &BackupRequest{Destination: "/backups/"},
			wantErr: false,
		},
		{
			name:    "valid request with s3",
			req:     &BackupRequest{Destination: "s3://bucket/path"},
			wantErr: false,
		},
		{
			name:    "valid request with compression",
			req:     &BackupRequest{Destination: "/backups/", Compress: true},
			wantErr: false,
		},
		{
			name:    "valid request with encryption",
			req:     &BackupRequest{Destination: "/backups/", Encrypt: true},
			wantErr: false,
		},
		{
			name:    "valid request with both options",
			req:     &BackupRequest{Destination: "/backups/", Compress: true, Encrypt: true},
			wantErr: false,
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
					backupReq, ok := req.(*BackupRequest)
					if !ok {
						t.Error("expected *BackupRequest")
					}
					if backupReq.Destination != "/backups/" {
						t.Errorf("expected destination /backups/, got %q", backupReq.Destination)
					}
					return &BackupResponse{
						BackupID:  "backup-abc123",
						Size:      1048576,
						Duration:  5 * time.Second,
						CreatedAt: now,
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

	validReq := &BackupRequest{Destination: "/backups/"}

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
			return &RestoreResponse{
				Restored: true,
				Duration: time.Second,
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
			name:    "valid request with source only",
			req:     &RestoreRequest{Source: "s3://bucket/backup"},
			wantErr: false,
		},
		{
			name:    "valid request with both",
			req:     &RestoreRequest{BackupID: "backup-123", Source: "s3://bucket/backup"},
			wantErr: false,
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
					return &RestoreResponse{
						Restored: true,
						Duration: 10 * time.Second,
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
					return &RestoreResponse{
						Restored: false,
						Duration: 0,
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
					return &ListBackupsResponse{
						Backups: []BackupInfo{
							{ID: "backup-1", Size: 1024, CreatedAt: now, Encrypted: false},
							{ID: "backup-2", Size: 2048, CreatedAt: now, Encrypted: true},
							{ID: "backup-3", Size: 4096, CreatedAt: now, Encrypted: true},
						},
					}, nil
				},
			},
			wantErr:     false,
			backupCount: 3,
		},
		{
			name: "list with empty result",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &ListBackupsResponse{
						Backups: []BackupInfo{},
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
			}
		})
	}
}

func TestBackupResponse_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BackupResponse{
				BackupID:  "backup-xyz789",
				Size:      10485760,
				Duration:  30 * time.Second,
				CreatedAt: now,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Create(context.Background(), &BackupRequest{Destination: "/backups/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.BackupID != "backup-xyz789" {
		t.Errorf("expected BackupID='backup-xyz789', got %q", resp.BackupID)
	}
	if resp.Size != 10485760 {
		t.Errorf("expected Size=10485760, got %d", resp.Size)
	}
	if resp.Duration != 30*time.Second {
		t.Errorf("expected Duration=30s, got %v", resp.Duration)
	}
	if !resp.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt=%v, got %v", now, resp.CreatedAt)
	}
}

func TestRestoreResponse_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RestoreResponse{
				Restored: true,
				Duration: 45 * time.Second,
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
	if resp.Duration != 45*time.Second {
		t.Errorf("expected Duration=45s, got %v", resp.Duration)
	}
}

func TestBackupInfo_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListBackupsResponse{
				Backups: []BackupInfo{
					{
						ID:        "backup-abc",
						Size:      5242880,
						CreatedAt: now,
						Encrypted: true,
					},
				},
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
	if !backup.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt=%v, got %v", now, backup.CreatedAt)
	}
	if !backup.Encrypted {
		t.Error("expected Encrypted to be true")
	}
}

func TestClient_Create_RequestValidation(t *testing.T) {
	var capturedReq *BackupRequest
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedReq = req.(*BackupRequest)
			return &BackupResponse{
				BackupID:  "backup-123",
				Size:      1024,
				Duration:  time.Second,
				CreatedAt: time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	originalReq := &BackupRequest{
		Destination: "s3://my-bucket/backups/",
		Compress:    true,
		Encrypt:     true,
	}

	_, err := client.Create(ctx, originalReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.Destination != originalReq.Destination {
		t.Errorf("expected Destination=%q, got %q", originalReq.Destination, capturedReq.Destination)
	}
	if capturedReq.Compress != originalReq.Compress {
		t.Errorf("expected Compress=%v, got %v", originalReq.Compress, capturedReq.Compress)
	}
	if capturedReq.Encrypt != originalReq.Encrypt {
		t.Errorf("expected Encrypt=%v, got %v", originalReq.Encrypt, capturedReq.Encrypt)
	}
}

func TestClient_Restore_RequestValidation(t *testing.T) {
	var capturedReq *RestoreRequest
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedReq = req.(*RestoreRequest)
			return &RestoreResponse{
				Restored: true,
				Duration: time.Second,
			}, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	originalReq := &RestoreRequest{
		BackupID: "backup-456",
		Source:   "s3://my-bucket/backups/backup-456",
	}

	_, err := client.Restore(ctx, originalReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.BackupID != originalReq.BackupID {
		t.Errorf("expected BackupID=%q, got %q", originalReq.BackupID, capturedReq.BackupID)
	}
	if capturedReq.Source != originalReq.Source {
		t.Errorf("expected Source=%q, got %q", originalReq.Source, capturedReq.Source)
	}
}

func TestClient_List_ServiceAndMethod(t *testing.T) {
	var capturedService, capturedMethod string
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			capturedService = service
			capturedMethod = method
			return &ListBackupsResponse{Backups: []BackupInfo{}}, nil
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
}

func TestValidateBackupRequest(t *testing.T) {
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
			name:    "empty destination",
			req:     &BackupRequest{Destination: ""},
			wantErr: true,
		},
		{
			name:    "valid destination",
			req:     &BackupRequest{Destination: "/path/to/backup"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBackupRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBackupRequest() error = %v, wantErr %v", err, tt.wantErr)
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
			name:    "source only",
			req:     &RestoreRequest{BackupID: "", Source: "/path/to/backup"},
			wantErr: false,
		},
		{
			name:    "both backup ID and source",
			req:     &RestoreRequest{BackupID: "backup-123", Source: "/path/to/backup"},
			wantErr: false,
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
			return &ListBackupsResponse{
				Backups: []BackupInfo{
					{ID: "backup-1", Size: 100, CreatedAt: now.Add(-2 * time.Hour), Encrypted: false},
					{ID: "backup-2", Size: 200, CreatedAt: now.Add(-1 * time.Hour), Encrypted: true},
					{ID: "backup-3", Size: 300, CreatedAt: now, Encrypted: true},
				},
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
	expectedEncrypted := []bool{false, true, true}

	for i, backup := range resp.Backups {
		if backup.ID != expectedIDs[i] {
			t.Errorf("backup[%d]: expected ID=%q, got %q", i, expectedIDs[i], backup.ID)
		}
		if backup.Size != expectedSizes[i] {
			t.Errorf("backup[%d]: expected Size=%d, got %d", i, expectedSizes[i], backup.Size)
		}
		if backup.Encrypted != expectedEncrypted[i] {
			t.Errorf("backup[%d]: expected Encrypted=%v, got %v", i, expectedEncrypted[i], backup.Encrypted)
		}
	}
}
