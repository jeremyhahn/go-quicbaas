// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package backup

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

// respondWith returns a transport that answers every request with resp.
func respondWith(resp interface{}) *mockTransport {
	return &mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return resp, nil
		},
	}
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
	client := NewClient(respondWith(&service.CreateBackupResponse{BackupID: "backup-123", Size: 1024}))
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BackupRequest
		wantErr bool
	}{
		{name: "nil request", req: nil, wantErr: true},
		{name: "empty destination", req: &BackupRequest{Destination: ""}, wantErr: true},
		{name: "valid request with local path", req: &BackupRequest{Destination: "/backups/"}},
		{name: "valid request with s3", req: &BackupRequest{Destination: "s3://bucket/path"}},
		{name: "valid request with compression", req: &BackupRequest{Destination: "/backups/", Compress: true}},
		{name: "valid request with encryption", req: &BackupRequest{Destination: "/backups/", Encrypt: true}},
		{name: "valid request with both options", req: &BackupRequest{Destination: "/backups/", Compress: true, Encrypt: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Create(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_Create(t *testing.T) {
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
				requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
					if svc != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, svc)
					}
					if method != "Create" {
						t.Errorf("expected method %q, got %q", "Create", method)
					}
					if _, ok := req.(*service.CreateBackupRequest); !ok {
						t.Errorf("expected *service.CreateBackupRequest, got %T", req)
					}
					return &service.CreateBackupResponse{BackupID: "backup-abc123", Size: 1048576}, nil
				},
			},
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
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			transport: respondWith(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			transport: respondWith("invalid"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
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
			if tt.wantErr && err != nil && errors.Code(err) != tt.errCode {
				t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_Restore_Validation(t *testing.T) {
	client := NewClient(respondWith(&service.RestoreBackupResponse{Status: "completed"}))
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RestoreRequest
		wantErr bool
	}{
		{name: "nil request", req: nil, wantErr: true},
		{name: "empty backup ID and source", req: &RestoreRequest{}, wantErr: true},
		{name: "valid request with backup ID only", req: &RestoreRequest{BackupID: "backup-123"}},
		{name: "valid request with source only", req: &RestoreRequest{Source: "s3://bucket/backup"}},
		{name: "valid request with both", req: &RestoreRequest{BackupID: "backup-123", Source: "s3://bucket/backup"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Restore(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Restore() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
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
				requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
					if svc != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, svc)
					}
					if method != "Restore" {
						t.Errorf("expected method %q, got %q", "Restore", method)
					}
					return &service.RestoreBackupResponse{JobID: "restore-backup-123", Status: "completed", Progress: 100}, nil
				},
			},
			restored: true,
		},
		{
			name:      "restore in progress",
			ctx:       context.Background(),
			transport: respondWith(&service.RestoreBackupResponse{Status: "running", Progress: 40}),
			restored:  false,
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
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
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
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "backup not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			transport: respondWith(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			transport: respondWith(123),
			wantErr:   true,
			errCode:   errors.CodeInternal,
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
			if tt.wantErr && err != nil && errors.Code(err) != tt.errCode {
				t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
			}
			if !tt.wantErr && resp.Restored != tt.restored {
				t.Errorf("expected restored=%v, got %v", tt.restored, resp.Restored)
			}
		})
	}
}

func TestClient_List(t *testing.T) {
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
				requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
					if svc != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, svc)
					}
					if method != "List" {
						t.Errorf("expected method %q, got %q", "List", method)
					}
					if _, ok := req.(*service.ListBackupsRequest); !ok {
						t.Errorf("expected *service.ListBackupsRequest, got %T", req)
					}
					return &service.ListBackupsResponse{
						Backups: []*service.BackupMetadata{
							{BackupID: "backup-1", Size: 1024},
							{BackupID: "backup-2", Size: 2048},
							{BackupID: "backup-3", Size: 4096},
						},
						TotalCount: 3,
					}, nil
				},
			},
			backupCount: 3,
		},
		{
			name:        "list with empty result",
			ctx:         context.Background(),
			transport:   respondWith(&service.ListBackupsResponse{}),
			backupCount: 0,
		},
		{
			name:        "nil entries are skipped",
			ctx:         context.Background(),
			transport:   respondWith(&service.ListBackupsResponse{Backups: []*service.BackupMetadata{nil, {BackupID: "backup-1"}}}),
			backupCount: 1,
		},
		{
			name:        "nil response returns empty list",
			ctx:         context.Background(),
			transport:   respondWith(nil),
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
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			transport: respondWith("invalid"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.List(tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && errors.Code(err) != tt.errCode {
				t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
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
	createdAt := time.Unix(1700000000, 500).UTC()
	client := NewClient(respondWith(&service.CreateBackupResponse{
		BackupID:  "backup-xyz789",
		Size:      10485760,
		Timestamp: createdAt.UnixNano(),
		Status:    "completed",
	}))

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
	if !resp.CreatedAt.Equal(createdAt) {
		t.Errorf("expected CreatedAt=%v, got %v", createdAt, resp.CreatedAt)
	}
}

func TestBackupResponse_SizeClampsOverflow(t *testing.T) {
	client := NewClient(respondWith(&service.CreateBackupResponse{BackupID: "backup-big", Size: ^uint64(0)}))

	resp, err := client.Create(context.Background(), &BackupRequest{Destination: "/backups/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Size <= 0 {
		t.Errorf("expected a size clamped to the int64 maximum, got %d", resp.Size)
	}
}

func TestBackupInfo_Fields(t *testing.T) {
	createdAt := time.Unix(1700000000, 0).UTC()
	client := NewClient(respondWith(&service.ListBackupsResponse{
		Backups: []*service.BackupMetadata{
			{BackupID: "backup-abc", Size: 5242880, Timestamp: createdAt.UnixNano(), Status: "completed"},
		},
		TotalCount: 1,
	}))

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
	if !backup.CreatedAt.Equal(createdAt) {
		t.Errorf("expected CreatedAt=%v, got %v", createdAt, backup.CreatedAt)
	}
}

func TestClient_Create_RequestMapping(t *testing.T) {
	tests := []struct {
		name            string
		req             *BackupRequest
		wantCompression string
		wantEncryption  string
	}{
		{
			name:            "compressed and encrypted",
			req:             &BackupRequest{Destination: "s3://my-bucket/backups/", Compress: true, Encrypt: true},
			wantCompression: "gzip",
			wantEncryption:  "aes-256-gcm",
		},
		{
			name:            "plain",
			req:             &BackupRequest{Destination: "/backups/"},
			wantCompression: "none",
			wantEncryption:  "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured *service.CreateBackupRequest
			client := NewClient(&mockTransport{
				requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
					captured, _ = req.(*service.CreateBackupRequest)
					return &service.CreateBackupResponse{BackupID: "backup-123"}, nil
				},
			})

			if _, err := client.Create(context.Background(), tt.req); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if captured == nil {
				t.Fatal("expected a *service.CreateBackupRequest on the wire")
			}
			if captured.Description != tt.req.Destination {
				t.Errorf("expected Description=%q, got %q", tt.req.Destination, captured.Description)
			}
			if captured.Compression != tt.wantCompression {
				t.Errorf("expected Compression=%q, got %q", tt.wantCompression, captured.Compression)
			}
			if captured.Encryption != tt.wantEncryption {
				t.Errorf("expected Encryption=%q, got %q", tt.wantEncryption, captured.Encryption)
			}
		})
	}
}

func TestClient_Restore_RequestMapping(t *testing.T) {
	var captured *service.RestoreBackupRequest
	client := NewClient(&mockTransport{
		requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
			captured, _ = req.(*service.RestoreBackupRequest)
			return &service.RestoreBackupResponse{Status: "completed"}, nil
		},
	})

	_, err := client.Restore(context.Background(), &RestoreRequest{
		BackupID: "backup-456",
		Source:   "s3://my-bucket/backups/backup-456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured == nil {
		t.Fatal("expected a *service.RestoreBackupRequest on the wire")
	}
	if captured.BackupID != "backup-456" {
		t.Errorf("expected BackupID=%q, got %q", "backup-456", captured.BackupID)
	}
}

func TestClient_List_ServiceAndMethod(t *testing.T) {
	var capturedService, capturedMethod string
	client := NewClient(&mockTransport{
		requestFn: func(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
			capturedService = svc
			capturedMethod = method
			return &service.ListBackupsResponse{}, nil
		},
	})

	if _, err := client.List(context.Background()); err != nil {
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
		{name: "nil request", req: nil, wantErr: true},
		{name: "empty destination", req: &BackupRequest{Destination: ""}, wantErr: true},
		{name: "valid destination", req: &BackupRequest{Destination: "/path/to/backup"}},
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
		{name: "nil request", req: nil, wantErr: true},
		{name: "empty backup ID and source", req: &RestoreRequest{}, wantErr: true},
		{name: "backup ID only", req: &RestoreRequest{BackupID: "backup-123"}},
		{name: "source only", req: &RestoreRequest{Source: "/path/to/backup"}},
		{name: "both backup ID and source", req: &RestoreRequest{BackupID: "backup-123", Source: "/path/to/backup"}},
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
	client := NewClient(respondWith(&service.ListBackupsResponse{
		Backups: []*service.BackupMetadata{
			{BackupID: "backup-1", Size: 100},
			{BackupID: "backup-2", Size: 200},
			{BackupID: "backup-3", Size: 300},
		},
		TotalCount: 3,
	}))

	resp, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(resp.Backups))
	}

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
