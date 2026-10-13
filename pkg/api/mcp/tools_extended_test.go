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

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// executeCreateShard Tests
// =============================================================================

func TestExecuteCreateShard_WithInitialMembers(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	result, err := reg.ExecuteTool(context.Background(), "quicbaas_create_shard", map[string]interface{}{
		"shardID": float64(1),
		"initialMembers": map[string]interface{}{
			"1": "localhost:8001",
			"2": "localhost:8002",
		},
	})
	require.NoError(t, err)
	_ = result
}

func TestExecuteCreateShard_Error(t *testing.T) {
	svc := &createShardErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_create_shard", map[string]interface{}{
		"shardID": float64(99),
	})
	require.Error(t, err)
}

type createShardErrorMock struct {
	mockService
}

func (m *createShardErrorMock) CreateShard(_ context.Context, _ *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	return nil, errors.New("shard creation failed")
}

// =============================================================================
// executeRebalanceCluster Tests
// =============================================================================

type rebalanceErrorMock struct {
	mockService
}

func (m *rebalanceErrorMock) RebalanceCluster(_ context.Context, _ *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	return nil, errors.New("rebalance failed")
}

func TestExecuteRebalanceCluster_Error(t *testing.T) {
	svc := &rebalanceErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_rebalance_cluster", map[string]interface{}{
		"targetShardCount": float64(3),
		"strategy":         "balanced",
	})
	require.Error(t, err)
}

// =============================================================================
// executeRebalanceStatus Tests
// =============================================================================

type rebalanceStatusErrorMock struct {
	mockService
}

func (m *rebalanceStatusErrorMock) GetRebalanceProgress(_ context.Context, _ *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
	return nil, errors.New("rebalance status failed")
}

func TestExecuteRebalanceStatus_Error(t *testing.T) {
	svc := &rebalanceStatusErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_rebalance_status", map[string]interface{}{})
	require.Error(t, err)
}

// =============================================================================
// executeBackupCreate Tests
// =============================================================================

type backupCreateErrorMock struct {
	mockService
}

func (m *backupCreateErrorMock) CreateBackup(_ context.Context, _ *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	return nil, errors.New("backup create failed")
}

func TestExecuteBackupCreate_Error(t *testing.T) {
	svc := &backupCreateErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_backup_create", map[string]interface{}{
		"path": "/backups/test",
	})
	require.Error(t, err)
}

// =============================================================================
// executeBackupRestore Tests
// =============================================================================

type backupRestoreErrorMock struct {
	mockService
}

func (m *backupRestoreErrorMock) RestoreBackup(_ context.Context, _ *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	return nil, errors.New("backup restore failed")
}

func TestExecuteBackupRestore_Error(t *testing.T) {
	svc := &backupRestoreErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_backup_restore", map[string]interface{}{
		"path": "/backups/test",
	})
	require.Error(t, err)
}

// =============================================================================
// executeReplaceNode Tests
// =============================================================================

type replaceNodeErrorMock struct {
	mockService
}

func (m *replaceNodeErrorMock) ReplaceNode(_ context.Context, _ *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
	return nil, errors.New("replace node failed")
}

func TestExecuteReplaceNode_Error(t *testing.T) {
	svc := &replaceNodeErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_replace_node", map[string]interface{}{
		"shardID":    float64(1),
		"oldNodeID":  float64(1),
		"newAddress": "localhost:9001",
	})
	require.Error(t, err)
}

// =============================================================================
// executeMigrateData Tests
// =============================================================================

type migrateDataErrorMock struct {
	mockService
}

func (m *migrateDataErrorMock) MigrateData(_ context.Context, _ *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
	return nil, errors.New("migrate data failed")
}

func TestExecuteMigrateData_Error(t *testing.T) {
	svc := &migrateDataErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_migrate_data", map[string]interface{}{
		"sourceShardID": float64(1),
		"targetShardID": float64(2),
	})
	require.Error(t, err)
}

// =============================================================================
// executeAggregate Tests
// =============================================================================

type aggregateErrorMock struct {
	mockService
}

func (m *aggregateErrorMock) Aggregate(_ context.Context, _ *service.AggregateRequest) (*service.AggregateResponse, error) {
	return nil, errors.New("aggregate failed")
}

func TestExecuteAggregate_Error(t *testing.T) {
	svc := &aggregateErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_aggregate", map[string]interface{}{
		"entityType": "users",
		"operation":  "count",
	})
	require.Error(t, err)
}

// =============================================================================
// executeQuery Tests
// =============================================================================

type queryErrorMock struct {
	mockService
}

func (m *queryErrorMock) Query(_ context.Context, _ *service.QueryRequest, stream chan<- *service.QueryResponse) error {
	close(stream)
	return errors.New("query failed")
}

func TestExecuteQuery_Error(t *testing.T) {
	svc := &queryErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_query", map[string]interface{}{
		"entityType": "users",
		"indexName":  "email_idx",
		"value":      "test@example.com",
	})
	require.Error(t, err)
}

// =============================================================================
// executeScanIndex Tests
// =============================================================================

type scanIndexErrorMock struct {
	mockService
}

func (m *scanIndexErrorMock) ScanIndex(_ context.Context, _ *service.ScanRequest, stream chan<- *service.ScanResponse) error {
	close(stream)
	return errors.New("scan index failed")
}

func TestExecuteScanIndex_Error(t *testing.T) {
	svc := &scanIndexErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "quicbaas_scan_index", map[string]interface{}{
		"entityType": "users",
		"indexName":  "email_idx",
		"startKey":   "a",
		"endKey":     "z",
	})
	require.Error(t, err)
}

// TestExecuteQuery_ServiceClosesTheStream is the #1284 regression: the service
// closes the stream it was handed, so the tool must not close it again. A
// second close panics the goroutine and with it the server.
func TestExecuteQuery_ServiceClosesTheStream(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)

	result, err := reg.ExecuteTool(context.Background(), "quicbaas_query", map[string]interface{}{
		"entityType": "users",
		"indexName":  "email_idx",
		"value":      "test@example.com",
	})
	require.NoError(t, err)
	require.Len(t, result, 1, "the response the service streamed was lost")
}

// TestExecuteScanIndex_ServiceClosesTheStream is the ScanIndex counterpart of
// TestExecuteQuery_ServiceClosesTheStream.
func TestExecuteScanIndex_ServiceClosesTheStream(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)

	result, err := reg.ExecuteTool(context.Background(), "quicbaas_scan_index", map[string]interface{}{
		"entityType": "users",
		"indexName":  "email_idx",
		"startKey":   "a",
		"endKey":     "z",
	})
	require.NoError(t, err)
	require.Len(t, result, 1, "the response the service streamed was lost")
}

// =============================================================================
// Removed user tools
// =============================================================================

// TestQRDBUserMutationToolsAreGone pins the issue #905 removal: the three
// qrdb_user_* mutation tools must not be registered and must not dispatch.
// They used to route to a service-layer auth manager production never wired, so
// every call failed; quicbaas_iam_user_* is the one user surface.
func TestQRDBUserMutationToolsAreGone(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)

	registered := make(map[string]struct{})
	for _, tool := range reg.ListTools() {
		registered[tool.Name] = struct{}{}
	}

	for _, name := range []string{"qrdb_user_create", "qrdb_user_delete", "qrdb_user_list"} {
		_, ok := registered[name]
		require.Falsef(t, ok, "tool %q must not be registered", name)

		_, _, mapped := getToolQRDBPermission(name)
		require.Falsef(t, mapped, "tool %q must not carry a permission mapping", name)

		_, err := reg.ExecuteTool(context.Background(), name, map[string]interface{}{})
		require.Errorf(t, err, "tool %q must not dispatch", name)
	}

	// The read-only qrdb-native user passthrough survives, and so does the
	// canonical IAM user surface.
	_, ok := registered["qrdb_user_get_by_username"]
	require.True(t, ok, "read-only qrdb_user_get_by_username must survive")
	_, ok = registered["quicbaas_iam_user_create"]
	require.True(t, ok, "canonical quicbaas_iam_user_create must survive")
}

// =============================================================================
// executeRoleCreate Tests
// =============================================================================

type roleCreateErrorMock struct {
	mockService
}

func (m *roleCreateErrorMock) CreateAuthRole(_ context.Context, _ *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
	return nil, errors.New("role create failed")
}

func TestExecuteRoleCreate_Error(t *testing.T) {
	svc := &roleCreateErrorMock{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(svc, log)

	_, err := reg.ExecuteTool(context.Background(), "qrdb_role_create", map[string]interface{}{
		"roleName":    "admin",
		"permissions": []interface{}{"read", "write"},
	})
	require.Error(t, err)
}
