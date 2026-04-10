//go:build mcp && iam

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

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Mock Audit Logger
// =============================================================================

type mockAuditLogger struct {
	queryFn       func(ctx context.Context, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error)
	getEventFn    func(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error)
	deleteOlderFn func(ctx context.Context, tenantID string, before time.Time) (int, error)
}

func (m *mockAuditLogger) Log(_ context.Context, _ *iam.AuditEvent) error { return nil }
func (m *mockAuditLogger) LogAsync(_ *iam.AuditEvent)                     {}
func (m *mockAuditLogger) Close() error                                   { return nil }

func (m *mockAuditLogger) Query(ctx context.Context, filter *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, filter)
	}
	return []*iam.AuditEvent{
		{ID: "evt-1", TenantID: filter.TenantID, Category: "auth"},
	}, 1, nil
}

func (m *mockAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
	if m.getEventFn != nil {
		return m.getEventFn(ctx, tenantID, eventID)
	}
	return &iam.AuditEvent{ID: eventID, TenantID: tenantID}, nil
}

func (m *mockAuditLogger) DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error) {
	if m.deleteOlderFn != nil {
		return m.deleteOlderFn(ctx, tenantID, before)
	}
	return 5, nil
}

var _ iam.AuditLogger = (*mockAuditLogger)(nil)

// =============================================================================
// Audit Mock Service
// =============================================================================

type auditMockService struct {
	mockService
	auditLogger iam.AuditLogger
}

func (m *auditMockService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return m.auditLogger, nil
}

func newAuditRegistry(t *testing.T, logger iam.AuditLogger) *ToolRegistry {
	t.Helper()
	log := newTestLogger(t)
	svc := &auditMockService{auditLogger: logger}
	return &ToolRegistry{service: svc, logger: log}
}

// =============================================================================
// Admin Error Type Tests
// =============================================================================

func TestAdminServiceUnavailableError(t *testing.T) {
	err := &AdminServiceUnavailableError{Reason: "not configured"}
	assert.Contains(t, err.Error(), "admin service unavailable")
	assert.Contains(t, err.Error(), "not configured")
}

// =============================================================================
// getAuditLogger Tests
// =============================================================================

func TestGetAuditLogger_ServiceDoesNotSupport(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.getAuditLogger(context.Background())
	require.Error(t, err)
}

func TestGetAuditLogger_NilLogger(t *testing.T) {
	reg := newAuditRegistry(t, nil)

	_, err := reg.getAuditLogger(context.Background())
	require.Error(t, err)
}

func TestGetAuditLogger_Success(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	al, err := reg.getAuditLogger(context.Background())
	require.NoError(t, err)
	require.NotNil(t, al)
}

// =============================================================================
// Audit Query Tests
// =============================================================================

func TestExecuteIAMAuditQuery_Success(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	result, err := reg.executeIAMAuditQuery(context.Background(), map[string]interface{}{
		"tenant_id":   "t1",
		"category":    "auth",
		"outcome":     "success",
		"actor_id":    "user-1",
		"resource_id": "res-1",
		"limit":       float64(50),
		"offset":      float64(10),
		"start_time":  "2024-01-01T00:00:00Z",
		"end_time":    "2024-12-31T23:59:59Z",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["events"])
}

func TestExecuteIAMAuditQuery_DefaultLimit(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	result, err := reg.executeIAMAuditQuery(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMAuditQuery_Error(t *testing.T) {
	auditLog := &mockAuditLogger{
		queryFn: func(_ context.Context, _ *iam.AuditFilter) ([]*iam.AuditEvent, int, error) {
			return nil, 0, errors.New("query failed")
		},
	}
	reg := newAuditRegistry(t, auditLog)

	_, err := reg.executeIAMAuditQuery(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
	})
	require.Error(t, err)
}

func TestExecuteIAMAuditQuery_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAuditQuery(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
	})
	require.Error(t, err)
}

// =============================================================================
// Audit Get Tests
// =============================================================================

func TestExecuteIAMAuditGet_Success(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	result, err := reg.executeIAMAuditGet(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"event_id":  "evt-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMAuditGet_Error(t *testing.T) {
	auditLog := &mockAuditLogger{
		getEventFn: func(_ context.Context, _, _ string) (*iam.AuditEvent, error) {
			return nil, errors.New("event not found")
		},
	}
	reg := newAuditRegistry(t, auditLog)

	_, err := reg.executeIAMAuditGet(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"event_id":  "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMAuditGet_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAuditGet(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"event_id":  "evt-1",
	})
	require.Error(t, err)
}

// =============================================================================
// Audit Delete Tests
// =============================================================================

func TestExecuteIAMAuditDelete_Success(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	result, err := reg.executeIAMAuditDelete(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"before":    "2024-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), resultMap["deleted"])
}

func TestExecuteIAMAuditDelete_InvalidBefore(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	_, err := reg.executeIAMAuditDelete(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"before":    "not-a-date",
	})
	require.Error(t, err)
}

func TestExecuteIAMAuditDelete_Error(t *testing.T) {
	auditLog := &mockAuditLogger{
		deleteOlderFn: func(_ context.Context, _ string, _ time.Time) (int, error) {
			return 0, errors.New("delete failed")
		},
	}
	reg := newAuditRegistry(t, auditLog)

	_, err := reg.executeIAMAuditDelete(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"before":    "2024-01-01T00:00:00Z",
	})
	require.Error(t, err)
}

func TestExecuteIAMAuditDelete_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executeIAMAuditDelete(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"before":    "2024-01-01T00:00:00Z",
	})
	require.Error(t, err)
}

// =============================================================================
// PEP Service Unavailable Tests
// =============================================================================

func TestExecutePEPCheck_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"principal": "user-1",
		"action":    "read",
		"resource":  "documents",
	})
	require.Error(t, err)
}

func TestExecutePEPCheckBatch_ServiceUnavailable(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.executePEPCheckBatch(context.Background(), map[string]interface{}{
		"checks": []interface{}{
			map[string]interface{}{
				"tenant_id": "t1", "principal": "user-1",
				"action": "read", "resource": "documents",
			},
		},
	})
	require.Error(t, err)
}

// =============================================================================
// PEP Input Validation Tests
// =============================================================================

type pepMockService struct {
	mockService
	engine *iam.PolicyEngine
}

func (m *pepMockService) GetPolicyEngine(_ context.Context) (*iam.PolicyEngine, error) {
	return m.engine, nil
}

func TestExecutePEPCheck_NilEngine(t *testing.T) {
	log := newTestLogger(t)
	svc := &pepMockService{engine: nil}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "principal": "user-1",
		"action": "read", "resource": "docs",
	})
	require.Error(t, err)
	var pepErr *PEPServiceUnavailableError
	assert.ErrorAs(t, err, &pepErr)
}

func TestExecutePEPCheckBatch_NilEngine(t *testing.T) {
	log := newTestLogger(t)
	svc := &pepMockService{engine: nil}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheckBatch(context.Background(), map[string]interface{}{
		"checks": []interface{}{
			map[string]interface{}{
				"tenant_id": "t1", "principal": "user-1",
				"action": "read", "resource": "docs",
			},
		},
	})
	require.Error(t, err)
	var pepErr *PEPServiceUnavailableError
	assert.ErrorAs(t, err, &pepErr)
}

func TestExecutePEPCheckBatch_MissingChecksArrayInput(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheckBatch(context.Background(), map[string]interface{}{
		"not_checks": "invalid",
	})
	require.Error(t, err)
	var pepErr *PEPInvalidInputError
	assert.ErrorAs(t, err, &pepErr)
}

func TestExecutePEPCheck_MissingTenantID(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"principal": "user-1",
		"action":    "read",
		"resource":  "docs",
	})
	require.Error(t, err)
	var pepErr *PEPInvalidInputError
	assert.ErrorAs(t, err, &pepErr)
	assert.Equal(t, "tenant_id", pepErr.Field)
}

func TestExecutePEPCheck_MissingPrincipal(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"action":    "read",
		"resource":  "docs",
	})
	require.Error(t, err)
	var pepErr *PEPInvalidInputError
	assert.ErrorAs(t, err, &pepErr)
	assert.Equal(t, "principal", pepErr.Field)
}

func TestExecutePEPCheck_MissingAction(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"principal": "user-1",
		"resource":  "docs",
	})
	require.Error(t, err)
	var pepErr *PEPInvalidInputError
	assert.ErrorAs(t, err, &pepErr)
	assert.Equal(t, "action", pepErr.Field)
}

func TestExecutePEPCheck_MissingResource(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	_, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"principal": "user-1",
		"action":    "read",
	})
	require.Error(t, err)
	var pepErr *PEPInvalidInputError
	assert.ErrorAs(t, err, &pepErr)
	assert.Equal(t, "resource", pepErr.Field)
}

func TestExecutePEPCheck_SuccessWithContext(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	// Policy engine with no policies defaults to deny
	result, err := reg.executePEPCheck(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"principal": "user-1",
		"action":    "read",
		"resource":  "documents",
		"context":   map[string]interface{}{"ip": "192.168.1.1"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecutePEPCheckBatch_SuccessWithMultipleChecks(t *testing.T) {
	log := newTestLogger(t)
	engine := iam.NewPolicyEngine(iam.PolicyEngineOptions{})
	svc := &pepMockService{engine: engine}
	reg := &ToolRegistry{service: svc, logger: log}

	result, err := reg.executePEPCheckBatch(context.Background(), map[string]interface{}{
		"checks": []interface{}{
			map[string]interface{}{
				"tenant_id": "t1", "principal": "user-1",
				"action": "read", "resource": "docs",
				"context": map[string]interface{}{"ip": "10.0.0.1"},
			},
			map[string]interface{}{
				"tenant_id": "t1", "principal": "user-2",
				"action": "write", "resource": "docs",
			},
			"invalid-entry", // non-map entries should be skipped
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), resultMap["count"])
}
