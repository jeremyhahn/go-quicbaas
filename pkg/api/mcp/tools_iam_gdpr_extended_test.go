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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// GDPR Error Type Tests
// =============================================================================

func TestGDPRServiceUnavailableError(t *testing.T) {
	err := &GDPRServiceUnavailableError{Reason: "not configured"}
	assert.Contains(t, err.Error(), "GDPR service unavailable")
	assert.Contains(t, err.Error(), "not configured")
}

// =============================================================================
// GDPR Export - Extended Success Tests
// =============================================================================

func TestExecuteIAMGDPRExportRequest_WithCategories(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMGDPRExportRequest(context.Background(), map[string]interface{}{
		"tenant_id":          "t1",
		"user_id":            "u1",
		"requested_by":       "admin",
		"format":             "csv",
		"include_categories": []interface{}{"profile", "sessions"},
		"exclude_categories": []interface{}{"audit_logs"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMGDPRExportRequest_DefaultFormat(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// When format is empty, defaults to "json"
	result, err := reg.executeIAMGDPRExportRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "u1",
		"requested_by": "admin",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMGDPRExportStatus_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// First create an export request to get an ID
	exportResult, err := reg.executeIAMGDPRExportRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "u1",
		"requested_by": "admin",
	})
	require.NoError(t, err)

	exportMap, ok := exportResult.(map[string]interface{})
	require.True(t, ok)
	exportID, ok := exportMap["id"].(string)
	require.True(t, ok)

	// Check status
	result, err := reg.executeIAMGDPRExportStatus(context.Background(), map[string]interface{}{
		"export_id": exportID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMGDPRExportGenerate_Success(t *testing.T) {
	log := newTestLogger(t)
	// Create manager with a pre-populated user
	userStore := newMockUserStorage()
	createErr := userStore.CreateUser(context.Background(), &iam.User{
		ID: "u1", TenantID: "t1", Email: "user@test.com",
	})
	require.NoError(t, createErr)

	mgr, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewMemoryConsentStorage(),
		DataExportStorage: iam.NewMemoryDataExportStorage(),
		DeletionStorage:   iam.NewMemoryDeletionStorage(),
		UserStorage:       userStore,
	})
	require.NoError(t, err)
	mock := &gdprMockQuicBaaS{gdprMgr: mgr}
	reg := &ToolRegistry{service: mock, logger: log}

	// Create export request
	exportResult, exportErr := reg.executeIAMGDPRExportRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "u1",
		"requested_by": "admin",
	})
	require.NoError(t, exportErr)

	exportMap, ok := exportResult.(map[string]interface{})
	require.True(t, ok)
	exportID, ok := exportMap["id"].(string)
	require.True(t, ok)

	// Generate export
	result, genErr := reg.executeIAMGDPRExportGenerate(context.Background(), map[string]interface{}{
		"export_id": exportID,
	})
	require.NoError(t, genErr)
	require.NotNil(t, result)
}

// =============================================================================
// GDPR Deletion - Extended Success Tests
// =============================================================================

func TestExecuteIAMGDPRDeletionRequest_WithCategories(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
		"tenant_id":           "t1",
		"user_id":             "u1",
		"requested_by":        "admin",
		"reason":              "GDPR request",
		"grace_period_days":   float64(14),
		"delete_categories":   []interface{}{"profile", "sessions"},
		"preserve_categories": []interface{}{"audit_logs"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMGDPRDeletionCancel_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create deletion request
	delResult, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "u1",
		"requested_by": "admin",
	})
	require.NoError(t, err)

	delMap, ok := delResult.(map[string]interface{})
	require.True(t, ok)
	delID, ok := delMap["id"].(string)
	require.True(t, ok)

	// Cancel it
	result, cancelErr := reg.executeIAMGDPRDeletionCancel(context.Background(), map[string]interface{}{
		"deletion_id":  delID,
		"cancelled_by": "admin",
	})
	require.NoError(t, cancelErr)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["cancelled"])
}

func TestExecuteIAMGDPRDeletionProcess_GracePeriodActive(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create a deletion request (default 30-day grace period)
	delResult, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "u1",
		"requested_by": "admin",
	})
	require.NoError(t, err)

	delMap, ok := delResult.(map[string]interface{})
	require.True(t, ok)
	delID, ok := delMap["id"].(string)
	require.True(t, ok)

	// Attempting to process during grace period should return an error
	_, processErr := reg.executeIAMGDPRDeletionProcess(context.Background(), map[string]interface{}{
		"deletion_id": delID,
	})
	require.Error(t, processErr)
	assert.Contains(t, processErr.Error(), "grace period")
}

func TestExecuteIAMGDPRDeletionStatus_Success(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Create deletion request
	delResult, err := reg.executeIAMGDPRDeletionRequest(context.Background(), map[string]interface{}{
		"tenant_id":    "t1",
		"user_id":      "u1",
		"requested_by": "admin",
	})
	require.NoError(t, err)

	delMap, ok := delResult.(map[string]interface{})
	require.True(t, ok)
	delID, ok := delMap["id"].(string)
	require.True(t, ok)

	// Check status
	result, statusErr := reg.executeIAMGDPRDeletionStatus(context.Background(), map[string]interface{}{
		"deletion_id": delID,
	})
	require.NoError(t, statusErr)
	require.NotNil(t, result)
}

// =============================================================================
// GDPR Consent - Extended Tests
// =============================================================================

func TestExecuteIAMGDPRConsentGrant_WithExpiration(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	result, err := reg.executeIAMGDPRConsentGrant(context.Background(), map[string]interface{}{
		"tenant_id":  "t1",
		"user_id":    "u1",
		"purpose":    "analytics",
		"version":    "2.0",
		"expires_at": "2030-12-31T23:59:59Z",
		"ip_address": "192.168.1.1",
		"user_agent": "Mozilla/5.0",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMGDPRConsentWithdraw_NotFound(t *testing.T) {
	log := newTestLogger(t)
	mock := newGDPRMockWithManager(t)
	reg := &ToolRegistry{service: mock, logger: log}

	// Withdraw consent that was never granted
	_, err := reg.executeIAMGDPRConsentWithdraw(context.Background(), map[string]interface{}{
		"tenant_id":  "t1",
		"user_id":    "u1",
		"purpose":    "marketing",
		"ip_address": "10.0.0.1",
	})
	require.Error(t, err)
}
