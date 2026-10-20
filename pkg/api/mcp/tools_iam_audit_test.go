//go:build mcp

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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
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
	adminMgr    *iam.AdminManager
}

func (m *auditMockService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return m.auditLogger, nil
}

func (m *auditMockService) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return m.adminMgr, nil
}

func newAuditRegistry(t *testing.T, logger iam.AuditLogger) *ToolRegistry {
	t.Helper()
	log := newTestLogger(t)
	svc := &auditMockService{auditLogger: logger, adminMgr: newAuditAdminManager(t)}
	return &ToolRegistry{service: svc, logger: log}
}

const auditDeleteTenant = "t1"

func newAuditAdminManager(t *testing.T) *iam.AdminManager {
	t.Helper()
	mgr, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: iam.NewMemoryImpersonationStorage(),
		TargetResolver:       mcpAllowAllTargetResolver{},
	})
	require.NoError(t, err)
	return mgr
}

// auditDeleteCtx binds an authenticated subject (user 42) to the context and,
// when grant is true, gives that user audit:delete on auditDeleteTenant.
func auditDeleteCtx(t *testing.T, reg *ToolRegistry, grant bool) context.Context {
	t.Helper()
	const userID = uint64(42)
	if grant {
		mgr, err := reg.getAdminManager(context.Background())
		require.NoError(t, err)
		_, err = mgr.CreateAssignment(context.Background(), iam.CreateAdminAssignmentOptions{
			UserID:      userID,
			AdminRole:   iam.AdminRoleAuditAdmin,
			TenantIDs:   []string{auditDeleteTenant},
			Permissions: []iam.AdminPermission{iam.PermAuditDelete},
			GrantedBy:   "test",
		})
		require.NoError(t, err)
	}
	return mcpCtxWithActor(userID)
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

	result, err := reg.executeIAMAuditDelete(auditDeleteCtx(t, reg, true), map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), resultMap["deleted"])
}

// TestExecuteIAMAuditDelete_WithoutAuditDeleteRefused proves the route
// permission (audit:query) is not enough to prune audit history: the
// executor refuses a subject lacking audit:delete, as REST and embedded do,
// and never reaches DeleteOlderThan.
func TestExecuteIAMAuditDelete_WithoutAuditDeleteRefused(t *testing.T) {
	deleteCalled := false
	auditLog := &mockAuditLogger{
		deleteOlderFn: func(_ context.Context, _ string, _ time.Time) (int, error) {
			deleteCalled = true
			return 5, nil
		},
	}
	reg := newAuditRegistry(t, auditLog)

	_, err := reg.executeIAMAuditDelete(auditDeleteCtx(t, reg, false), map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	var denied *api.InsufficientPermissionsError
	require.ErrorAs(t, err, &denied)
	assert.Equal(t, mcptransport.MCPPermissionDenied, toolErrorCode(err))
	assert.False(t, deleteCalled, "DeleteOlderThan must not run for a denied subject")
}

// TestExecuteIAMAuditDelete_OtherTenantGrantRefused proves the check is made
// against the target tenant: audit:delete on t1 does not reach t2.
func TestExecuteIAMAuditDelete_OtherTenantGrantRefused(t *testing.T) {
	reg := newAuditRegistry(t, &mockAuditLogger{})

	_, err := reg.executeIAMAuditDelete(auditDeleteCtx(t, reg, true), map[string]interface{}{
		"tenant_id": "t2",
		"before":    "2024-01-01T00:00:00Z",
	})
	var denied *api.InsufficientPermissionsError
	require.ErrorAs(t, err, &denied)
}

func TestExecuteIAMAuditDelete_NoSubjectFailsClosed(t *testing.T) {
	reg := newAuditRegistry(t, &mockAuditLogger{})

	_, err := reg.executeIAMAuditDelete(context.Background(), map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	var denied *api.InsufficientPermissionsError
	require.ErrorAs(t, err, &denied)
}

func TestExecuteIAMAuditDelete_UnauthenticatedSubjectFailsClosed(t *testing.T) {
	reg := newAuditRegistry(t, &mockAuditLogger{})
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "42", UserID: 42})

	_, err := reg.executeIAMAuditDelete(ctx, map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	var denied *api.InsufficientPermissionsError
	require.ErrorAs(t, err, &denied)
}

// TestExecuteIAMAuditDelete_CheckPermissionErrorReturned proves a failing
// CheckPermission (here: a subject with no numeric user ID) is returned as
// is rather than reported as a denial or ignored.
func TestExecuteIAMAuditDelete_CheckPermissionErrorReturned(t *testing.T) {
	reg := newAuditRegistry(t, &mockAuditLogger{})
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "svc", Authenticated: true})

	_, err := reg.executeIAMAuditDelete(ctx, map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	var required *api.RequiredFieldError
	require.ErrorAs(t, err, &required)
	assert.Equal(t, "user_id", required.Field)
}

func TestExecuteIAMAuditDelete_AdminManagerUnavailable(t *testing.T) {
	reg := &ToolRegistry{
		service: &auditMockService{auditLogger: &mockAuditLogger{}},
		logger:  newTestLogger(t),
	}

	_, err := reg.executeIAMAuditDelete(mcpCtxWithActor(42), map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	require.Error(t, err)
	var denied *api.InsufficientPermissionsError
	assert.NotErrorAs(t, err, &denied)
}

func TestExecuteIAMAuditDelete_InvalidBefore(t *testing.T) {
	auditLog := &mockAuditLogger{}
	reg := newAuditRegistry(t, auditLog)

	_, err := reg.executeIAMAuditDelete(auditDeleteCtx(t, reg, true), map[string]interface{}{
		"tenant_id": auditDeleteTenant,
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

	_, err := reg.executeIAMAuditDelete(auditDeleteCtx(t, reg, true), map[string]interface{}{
		"tenant_id": auditDeleteTenant,
		"before":    "2024-01-01T00:00:00Z",
	})
	require.EqualError(t, err, "delete failed")
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
