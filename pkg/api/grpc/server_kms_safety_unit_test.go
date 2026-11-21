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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockKMSServiceForSafety is a more specific mock for safety operations.
// Uses func fields to allow per-test customization.
type mockKMSServiceForSafety struct {
	mockKMSService
	setKeySafetyFn   func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error)
	clearKeySafetyFn func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error)
	safetyCheckFn    func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error)
	safetyReportFn   func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error)
	exportKeyFn      func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error)
	unwrapKeyFn      func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error)
	generateKeyFn    func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error)
}

func (m *mockKMSServiceForSafety) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	if m.generateKeyFn != nil {
		return m.generateKeyFn(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSServiceForSafety) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	if m.setKeySafetyFn != nil {
		return m.setKeySafetyFn(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSServiceForSafety) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	if m.clearKeySafetyFn != nil {
		return m.clearKeySafetyFn(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSServiceForSafety) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	if m.safetyCheckFn != nil {
		return m.safetyCheckFn(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSServiceForSafety) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	if m.safetyReportFn != nil {
		return m.safetyReportFn(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSServiceForSafety) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	if m.exportKeyFn != nil {
		return m.exportKeyFn(ctx, req)
	}
	return nil, nil
}

func (m *mockKMSServiceForSafety) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	if m.unwrapKeyFn != nil {
		return m.unwrapKeyFn(ctx, req)
	}
	return nil, nil
}

// createTestKMSServerWithSafetyService creates a KMS server with a safety mock service.
func createTestKMSServerWithSafetyService(t *testing.T) (*KMSServer, *mockKMSServiceForSafety, *mockKMSManager) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockMgr := &mockKMSManager{}
	mockSvc := &mockKMSServiceForSafety{}
	srv := NewKMSServerWithService(mockMgr, mockSvc, log)
	return srv, mockSvc, mockMgr
}

// =============================================================================
// SetKeySafety Tests
// =============================================================================

func TestKMSServer_SetKeySafety(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.SetKeySafetyRequest
		mockSetup   func(m *mockKMSServiceForSafety)
		noService   bool
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.SetKeySafetyResponse)
	}{
		{
			name: "successful_set_safety",
			request: &pb.SetKeySafetyRequest{
				KeyId:          "test-key-123",
				MaxOperations:  1000000,
				MaxBytes:       1073741824, // 1GB
				WarningPercent: 80,
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.setKeySafetyFn = func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
					assert.Equal(t, "test-key-123", req.KeyID)
					assert.Equal(t, uint64(1000000), req.MaxOperations)
					assert.Equal(t, uint64(1073741824), req.MaxBytes)
					assert.Equal(t, 80, req.WarningPercent)
					return &service.SetKeySafetyResponse{KeyID: "test-key-123"}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SetKeySafetyResponse) {
				assert.Equal(t, "test-key-123", resp.KeyId)
				assert.Greater(t, resp.Timestamp, int64(0))
			},
		},
		{
			name: "missing_key_id",
			request: &pb.SetKeySafetyRequest{
				KeyId:         "",
				MaxOperations: 1000,
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_not_configured",
			request: &pb.SetKeySafetyRequest{
				KeyId:         "test-key",
				MaxOperations: 1000,
			},
			noService:   true,
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name: "service_error_not_found",
			request: &pb.SetKeySafetyRequest{
				KeyId:         "missing-key",
				MaxOperations: 1000,
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.setKeySafetyFn = func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
					return nil, errors.New(errors.ErrNotFound, "SetKeySafety", "key not found")
				}
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			if tt.noService {
				srv.kmsSvc = nil
			} else if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			resp, err := srv.SetKeySafety(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// ClearKeySafety Tests
// =============================================================================

func TestKMSServer_ClearKeySafety(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.ClearKeySafetyRequest
		mockSetup   func(m *mockKMSServiceForSafety)
		noService   bool
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ClearKeySafetyResponse)
	}{
		{
			name: "successful_clear_safety",
			request: &pb.ClearKeySafetyRequest{
				KeyId: "test-key-123",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.clearKeySafetyFn = func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
					assert.Equal(t, "test-key-123", req.KeyID)
					return &service.ClearKeySafetyResponse{KeyID: "test-key-123"}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ClearKeySafetyResponse) {
				assert.Equal(t, "test-key-123", resp.KeyId)
				assert.Greater(t, resp.Timestamp, int64(0))
			},
		},
		{
			name: "missing_key_id",
			request: &pb.ClearKeySafetyRequest{
				KeyId: "",
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_not_configured",
			request: &pb.ClearKeySafetyRequest{
				KeyId: "test-key",
			},
			noService:   true,
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name: "service_error_forbidden",
			request: &pb.ClearKeySafetyRequest{
				KeyId: "protected-key",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.clearKeySafetyFn = func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
					return nil, errors.New(errors.ErrForbidden, "ClearKeySafety", "operation not permitted")
				}
			},
			expectError: true,
			errorCode:   codes.PermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			if tt.noService {
				srv.kmsSvc = nil
			} else if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			resp, err := srv.ClearKeySafety(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// SafetyCheck Tests
// =============================================================================

func TestKMSServer_SafetyCheck(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.SafetyCheckRequest
		mockSetup   func(m *mockKMSServiceForSafety)
		noService   bool
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.SafetyCheckResponse)
	}{
		{
			name:    "successful_safety_check_no_warnings",
			request: &pb.SafetyCheckRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyCheckFn = func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
					return &service.SafetyCheckResponse{Items: []service.SafetyCheckItem{}}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SafetyCheckResponse) {
				assert.Empty(t, resp.Warnings)
				assert.Greater(t, resp.Timestamp, int64(0))
			},
		},
		{
			name:    "safety_check_with_warnings",
			request: &pb.SafetyCheckRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyCheckFn = func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
					return &service.SafetyCheckResponse{
						Items: []service.SafetyCheckItem{
							{
								Type:           "key",
								ID:             "key-1",
								UsagePercent:   85.5,
								OperationCount: 855000,
								BytesProcessed: 8550000000,
								MaxOperations:  1000000,
								MaxBytes:       10000000000,
							},
							{
								Type:           "dek",
								ID:             "dek-1",
								UsagePercent:   92.0,
								OperationCount: 920000,
								BytesProcessed: 9200000000,
								MaxOperations:  1000000,
								MaxBytes:       10000000000,
							},
						},
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SafetyCheckResponse) {
				require.Len(t, resp.Warnings, 2)

				// First warning - key at 85.5%
				assert.Equal(t, "key", resp.Warnings[0].Type)
				assert.Equal(t, "key-1", resp.Warnings[0].Path)
				assert.InDelta(t, 85.5, resp.Warnings[0].UsagePercent, 0.01)
				assert.Equal(t, "warning", resp.Warnings[0].Status)
				assert.Equal(t, uint64(855000), resp.Warnings[0].OperationsUsed)
				assert.Equal(t, uint64(8550000000), resp.Warnings[0].BytesUsed)

				// Second warning - dek at 92%
				assert.Equal(t, "dek", resp.Warnings[1].Type)
				assert.Equal(t, "dek-1", resp.Warnings[1].Path)
				assert.InDelta(t, 92.0, resp.Warnings[1].UsagePercent, 0.01)
				assert.Equal(t, "warning", resp.Warnings[1].Status)
			},
		},
		{
			name:    "safety_check_critical_status",
			request: &pb.SafetyCheckRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyCheckFn = func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
					return &service.SafetyCheckResponse{
						Items: []service.SafetyCheckItem{
							{Type: "key", ID: "critical-key", UsagePercent: 97.5},
						},
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SafetyCheckResponse) {
				require.Len(t, resp.Warnings, 1)
				assert.Equal(t, "critical", resp.Warnings[0].Status)
			},
		},
		{
			name:    "safety_check_exceeded_status",
			request: &pb.SafetyCheckRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyCheckFn = func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
					return &service.SafetyCheckResponse{
						Items: []service.SafetyCheckItem{
							{Type: "key", ID: "exceeded-key", UsagePercent: 100.5},
						},
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SafetyCheckResponse) {
				require.Len(t, resp.Warnings, 1)
				assert.Equal(t, "exceeded", resp.Warnings[0].Status)
			},
		},
		{
			name:        "service_not_configured",
			request:     &pb.SafetyCheckRequest{},
			noService:   true,
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name:    "service_error",
			request: &pb.SafetyCheckRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyCheckFn = func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
					return nil, errors.New(errors.ErrInternal, "SafetyCheck", "internal error")
				}
			},
			expectError: true,
			errorCode:   codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			if tt.noService {
				srv.kmsSvc = nil
			} else if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			resp, err := srv.SafetyCheck(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// SafetyReport Tests
// =============================================================================

func TestKMSServer_SafetyReport(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.SafetyReportRequest
		mockSetup   func(m *mockKMSServiceForSafety)
		noService   bool
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.SafetyReportResponse)
	}{
		{
			name:    "successful_safety_report_empty",
			request: &pb.SafetyReportRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyReportFn = func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
					return &service.SafetyReportResponse{Items: []service.SafetyCheckItem{}}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SafetyReportResponse) {
				assert.Empty(t, resp.AllItems)
				assert.Equal(t, int32(0), resp.TotalKeys)
				assert.Equal(t, int32(0), resp.TotalDeks)
				assert.Equal(t, int32(0), resp.KeysAtWarning)
				assert.Equal(t, int32(0), resp.KeysAtCritical)
				assert.Equal(t, int32(0), resp.DeksAtWarning)
				assert.Equal(t, int32(0), resp.DeksAtCritical)
				assert.Greater(t, resp.Timestamp, int64(0))
			},
		},
		{
			name:    "safety_report_with_all_statuses",
			request: &pb.SafetyReportRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyReportFn = func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
					return &service.SafetyReportResponse{
						Items: []service.SafetyCheckItem{
							// Keys
							{Type: "key", ID: "key-normal", UsagePercent: 50.0},    // normal
							{Type: "key", ID: "key-warning", UsagePercent: 85.0},   // warning
							{Type: "key", ID: "key-critical", UsagePercent: 96.0},  // critical
							{Type: "key", ID: "key-exceeded", UsagePercent: 101.0}, // exceeded (critical)
							// DEKs
							{Type: "dek", ID: "dek-normal", UsagePercent: 30.0},     // normal
							{Type: "dek", ID: "dek-warning", UsagePercent: 82.0},    // warning
							{Type: "dek", ID: "dek-critical-1", UsagePercent: 97.0}, // critical
							{Type: "dek", ID: "dek-critical-2", UsagePercent: 99.0}, // critical
						},
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.SafetyReportResponse) {
				assert.Len(t, resp.AllItems, 8)
				assert.Equal(t, int32(4), resp.TotalKeys)
				assert.Equal(t, int32(4), resp.TotalDeks)
				assert.Equal(t, int32(1), resp.KeysAtWarning)
				assert.Equal(t, int32(2), resp.KeysAtCritical) // >= 95%: critical + exceeded
				assert.Equal(t, int32(1), resp.DeksAtWarning)
				assert.Equal(t, int32(2), resp.DeksAtCritical) // Both >= 95%

				// Verify status strings
				for _, item := range resp.AllItems {
					if item.UsagePercent >= 100.0 {
						assert.Equal(t, "exceeded", item.Status)
					} else if item.UsagePercent >= 95.0 {
						assert.Equal(t, "critical", item.Status)
					} else if item.UsagePercent >= 80.0 {
						assert.Equal(t, "warning", item.Status)
					} else {
						assert.Equal(t, "ok", item.Status)
					}
				}
			},
		},
		{
			name:        "service_not_configured",
			request:     &pb.SafetyReportRequest{},
			noService:   true,
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name:    "service_error_unavailable",
			request: &pb.SafetyReportRequest{},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.safetyReportFn = func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
					return nil, errors.New(errors.ErrUnavailable, "SafetyReport", "service unavailable")
				}
			},
			expectError: true,
			errorCode:   codes.Unavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			if tt.noService {
				srv.kmsSvc = nil
			} else if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			resp, err := srv.SafetyReport(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// safetyStatusFromPercent Tests
// =============================================================================

func TestSafetyStatusFromPercent(t *testing.T) {
	tests := []struct {
		name     string
		percent  float64
		expected string
	}{
		{"normal_0", 0.0, "ok"},
		{"normal_50", 50.0, "ok"},
		{"normal_79.9", 79.9, "ok"},
		{"warning_80", 80.0, "warning"},
		{"warning_85", 85.0, "warning"},
		{"warning_94.9", 94.9, "warning"},
		{"critical_95", 95.0, "critical"},
		{"critical_97", 97.0, "critical"},
		{"critical_99.9", 99.9, "critical"},
		{"exceeded_100", 100.0, "exceeded"},
		{"exceeded_105", 105.0, "exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safetyStatusFromPercent(tt.percent)
			assert.Equal(t, tt.expected, result)
		})
	}
}
