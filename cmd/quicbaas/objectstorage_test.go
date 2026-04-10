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

package main

import (
	"context"
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
)

func TestEmbeddedServerFailedError_Error(t *testing.T) {
	err := &EmbeddedServerFailedError{Reason: "connection refused"}
	assert.Contains(t, err.Error(), "embedded server failed")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestEmbeddedPortError_Error(t *testing.T) {
	err := &EmbeddedPortError{}
	assert.Contains(t, err.Error(), "listener address is not a TCP address")
}

func TestEmbeddedServer_ShutdownNotStarted(t *testing.T) {
	es := &embeddedServer{}
	err := es.shutdown()
	assert.NoError(t, err)
}

func TestEmbeddedServer_ShutdownStartedNoServer(t *testing.T) {
	es := &embeddedServer{}
	es.started.Store(true)
	es.tempDir = t.TempDir()

	err := es.shutdown()
	assert.NoError(t, err)
	assert.False(t, es.started.Load(), "started should be false after shutdown")
}

// =============================================================================
// factories.go coverage - GRPCBackupClientWrapper stubs
// These wrapper methods delegate to the adapter, so they fail with nil adapter
// but the code path through the wrapper method body is covered.
// =============================================================================

func TestGRPCBackupClientWrapper_CreateBackup_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	assert.Panics(t, func() {
		wrapper.CreateBackup(context.Background(), &BackupCreateRequest{})
	})
}

func TestGRPCBackupClientWrapper_ListBackups_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	assert.Panics(t, func() {
		wrapper.ListBackups(context.Background(), &BackupListRequest{})
	})
}

func TestGRPCBackupClientWrapper_RestoreBackup_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	assert.Panics(t, func() {
		wrapper.RestoreBackup(context.Background(), &BackupRestoreRequest{})
	})
}

func TestGRPCBackupClientWrapper_DeleteBackup_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	assert.Panics(t, func() {
		wrapper.DeleteBackup(context.Background(), &BackupDeleteRequest{})
	})
}

func TestGRPCBackupClientWrapper_VerifyBackup_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	assert.Panics(t, func() {
		wrapper.VerifyBackup(context.Background(), &BackupVerifyRequest{})
	})
}

func TestGRPCBackupClientWrapper_Close_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	assert.Panics(t, func() {
		wrapper.Close()
	})
}

// =============================================================================
// client.go coverage - loadMCPAuthToken
// =============================================================================

func TestLoadMCPAuthToken_EmptyTokenFile(t *testing.T) {
	origTokenFile := authTokenFile
	defer func() { authTokenFile = origTokenFile }()

	authTokenFile = "/nonexistent/path/token"
	token := loadMCPAuthToken()
	assert.Empty(t, token)
}

func TestLoadMCPAuthToken_ValidToken(t *testing.T) {
	origTokenFile := authTokenFile
	defer func() { authTokenFile = origTokenFile }()

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	tokenData := &cli.TokenData{
		AccessToken:  "test-access-token-123",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TokenType:    "Bearer",
	}

	err := cli.SaveToken(tokenPath, tokenData)
	require.NoError(t, err)

	authTokenFile = tokenPath
	token := loadMCPAuthToken()
	assert.Equal(t, "test-access-token-123", token)
}

func TestLoadMCPAuthToken_ExpiredToken(t *testing.T) {
	origTokenFile := authTokenFile
	defer func() { authTokenFile = origTokenFile }()

	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")

	tokenData := &cli.TokenData{
		AccessToken:  "expired-token",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		TokenType:    "Bearer",
	}

	err := cli.SaveToken(tokenPath, tokenData)
	require.NoError(t, err)

	authTokenFile = tokenPath
	token := loadMCPAuthToken()
	assert.Empty(t, token, "expired token should return empty string")
}

func TestLoadMCPAuthToken_DefaultPathFallback(t *testing.T) {
	origTokenFile := authTokenFile
	defer func() { authTokenFile = origTokenFile }()

	authTokenFile = ""
	token := loadMCPAuthToken()
	assert.Empty(t, token)
}

// =============================================================================
// client.go coverage - buildSDKClientOptions embedded transport
// =============================================================================

func TestBuildSDKClientOptions_EmbeddedTransport_NoServer(t *testing.T) {
	savedTransport := transport
	savedTimeout := globalTimeout
	defer func() {
		transport = savedTransport
		globalTimeout = savedTimeout
		embeddedServerHolder.Store(nil)
	}()

	embeddedServerHolder.Store(nil)
	transport = "embedded"
	globalTimeout = 5 * time.Second

	opts, err := buildSDKClientOptions()
	if err != nil {
		assert.Nil(t, opts)
	} else {
		assert.NotNil(t, opts)
	}
}

// =============================================================================
// client.go coverage - closeSDKClient with embedded server
// =============================================================================

func TestCloseSDKClient_WithEmbeddedServer(t *testing.T) {
	defer func() {
		resetSDKClient()
		embeddedServerHolder.Store(nil)
	}()

	es := &embeddedServer{}
	embeddedServerHolder.Store(es)

	mock := &mockClient{}
	var client quicbaas.Client = mock
	sdkClientHolder.Store(&client)

	err := closeSDKClient()
	assert.NoError(t, err)
	assert.True(t, mock.closed, "client should be closed")
	assert.Nil(t, embeddedServerHolder.Load(), "embedded server should be cleared")
}

// =============================================================================
// rebalance.go coverage - display helper functions (new paths only)
// =============================================================================

func TestDisplayRebalanceReport_Table_NoMoves(t *testing.T) {
	report := &RebalanceReport{
		StartTime:       time.Now().Add(-1 * time.Minute),
		EndTime:         time.Now(),
		Duration:        1 * time.Minute,
		MovesCompleted:  0,
		MovesFailed:     0,
		ImbalanceBefore: 0.01,
		ImbalanceAfter:  0.01,
	}

	err := displayRebalanceReport(report, "table")
	assert.NoError(t, err)
}

func TestDisplayRebalanceReport_Table_WithFailedMoves(t *testing.T) {
	report := &RebalanceReport{
		StartTime:       time.Now().Add(-3 * time.Minute),
		EndTime:         time.Now(),
		Duration:        3 * time.Minute,
		MovesCompleted:  8,
		MovesFailed:     2,
		KeysRebalanced:  4000,
		BytesRebalanced: 1024 * 1024,
		ImbalanceBefore: 0.20,
		ImbalanceAfter:  0.10,
		Errors:          []string{errors.New(errors.ErrInternal, "test", "shard move failed").Error()},
	}

	err := displayRebalanceReport(report, "table")
	assert.NoError(t, err)
}

func TestDisplayRebalanceReport_Table_ManyErrors(t *testing.T) {
	errs := make([]string, 8)
	for i := 0; i < 8; i++ {
		errs[i] = errors.New(errors.ErrInternal, "test", "error").Error()
	}
	report := &RebalanceReport{
		StartTime:       time.Now().Add(-2 * time.Minute),
		EndTime:         time.Now(),
		Duration:        2 * time.Minute,
		MovesCompleted:  5,
		MovesFailed:     8,
		ImbalanceBefore: 0.30,
		ImbalanceAfter:  0.25,
		Errors:          errs,
	}

	err := displayRebalanceReport(report, "table")
	assert.NoError(t, err)
}

func TestDisplayRebalanceReport_Table_SuccessfulMoves(t *testing.T) {
	report := &RebalanceReport{
		StartTime:       time.Now().Add(-1 * time.Minute),
		EndTime:         time.Now(),
		Duration:        1 * time.Minute,
		MovesCompleted:  5,
		MovesFailed:     0,
		KeysRebalanced:  2500,
		BytesRebalanced: 1024 * 500,
		ImbalanceBefore: 0.15,
		ImbalanceAfter:  0.02,
	}

	err := displayRebalanceReport(report, "table")
	assert.NoError(t, err)
}

func TestDisplayRebalanceReport_InvalidFormat(t *testing.T) {
	report := &RebalanceReport{}
	err := displayRebalanceReport(report, "yaml")
	assert.Error(t, err)
}

func TestDisplayRebalanceProgressSDK_InvalidFormat(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{}
	err := displayRebalanceProgressSDK(resp, "xml")
	assert.Error(t, err)
}

func TestDisplayRebalanceReport_Table_ZeroImprovement(t *testing.T) {
	report := &RebalanceReport{
		StartTime:       time.Now().Add(-1 * time.Minute),
		EndTime:         time.Now(),
		Duration:        1 * time.Minute,
		MovesCompleted:  3,
		MovesFailed:     0,
		ImbalanceBefore: 0.05,
		ImbalanceAfter:  0.05,
	}

	err := displayRebalanceReport(report, "table")
	assert.NoError(t, err)
}

// =============================================================================
// objectstorage.go coverage - createObjectStorageClient transport switch
// =============================================================================

func TestCreateObjectStorageClient_GRPCTransport(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "grpc"
	_, err := createObjectStorageClient()
	assert.Error(t, err)
}

func TestCreateObjectStorageClient_EmptyTransport(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = ""
	_, err := createObjectStorageClient()
	assert.Error(t, err)
}

func TestCreateObjectStorageClient_UnsupportedTransport(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "rest"
	_, err := createObjectStorageClient()
	require.Error(t, err)
	var unsupportedErr *UnsupportedTransportError
	assert.ErrorAs(t, err, &unsupportedErr)
}

// =============================================================================
// user.go coverage - runUserRevokeRoleCommand edge cases
// =============================================================================

func TestRunUserRevokeRoleCommand_SuccessNotTrue(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := userForce
	origLog := log
	defer func() {
		rbacClientFactory = origFactory
		userForce = origForce
		log = origLog
	}()

	testLog := createTestLoggerForCoverageNinetyOne(t)
	log = testLog

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{
					Success: false,
					Message: "role not assigned to user",
				}, nil
			},
		}, nil
	}
	userForce = true

	cmd := newUserRevokeRoleCommand()
	cmd.SetArgs([]string{"testuser", "admin", "--yes"})

	err := cmd.Execute()
	assert.NoError(t, err)
}

func TestRunUserRevokeRoleCommand_FactoryError(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := userForce
	origLog := log
	defer func() {
		rbacClientFactory = origFactory
		userForce = origForce
		log = origLog
	}()

	testLog := createTestLoggerForCoverageNinetyOne(t)
	log = testLog

	rbacClientFactory = func() (rbac.Client, error) {
		return nil, errors.Newf(errors.ErrUnauthorized, "test", "not authenticated")
	}
	userForce = true

	cmd := newUserRevokeRoleCommand()
	cmd.SetArgs([]string{"testuser", "admin", "--yes"})

	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

// =============================================================================
// role.go coverage - runRoleUnassignCommand edge cases
// =============================================================================

func TestRunRoleUnassignCommand_SuccessNotTrue(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := roleForce
	origLog := log
	defer func() {
		rbacClientFactory = origFactory
		roleForce = origForce
		log = origLog
	}()

	testLog := createTestLoggerForCoverageNinetyOne(t)
	log = testLog

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{
					Success: false,
					Message: "role not found on user",
				}, nil
			},
		}, nil
	}
	roleForce = true

	cmd := newRoleUnassignCommand()
	cmd.SetArgs([]string{"admin", "testuser", "--yes"})

	err := cmd.Execute()
	assert.NoError(t, err)
}

func TestRunRoleUnassignCommand_FactoryError(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := roleForce
	origLog := log
	defer func() {
		rbacClientFactory = origFactory
		roleForce = origForce
		log = origLog
	}()

	testLog := createTestLoggerForCoverageNinetyOne(t)
	log = testLog

	rbacClientFactory = func() (rbac.Client, error) {
		return nil, errors.Newf(errors.ErrUnauthorized, "test", "auth failed")
	}
	roleForce = true

	cmd := newRoleUnassignCommand()
	cmd.SetArgs([]string{"admin", "testuser", "--yes"})

	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// objectstorage.go coverage - runObjectListByTagCommand invalid tag format
// =============================================================================

func TestRunObjectListByTagCommand_InvalidTagFormat(t *testing.T) {
	origFactory := objectStorageClientFactory
	origTagFilter := objectTagFilter
	defer func() {
		objectStorageClientFactory = origFactory
		objectTagFilter = origTagFilter
	}()

	objectTagFilter = "invalidtag"

	cmd := newObjectListByTagCommand()
	err := runObjectListByTagCommand(cmd, []string{"bucket"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tag format")
}

// =============================================================================
// objectstorage.go coverage - runObjectMetadataUpdateCommand invalid JSON
// =============================================================================

func TestRunObjectMetadataUpdateCommand_InvalidPolicyJSON(t *testing.T) {
	origMetadata := objectMetadata
	defer func() { objectMetadata = origMetadata }()

	objectMetadata = "not-json"
	cmd := newObjectMetadataUpdateCommand()
	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
}

// =============================================================================
// objectstorage.go coverage - runObjectLifecycleSetCommand invalid JSON
// =============================================================================

func TestRunObjectLifecycleSetCommand_InvalidPolicyJSON(t *testing.T) {
	origPolicyJSON := objectPolicyJSON
	defer func() { objectPolicyJSON = origPolicyJSON }()

	objectPolicyJSON = "bad-json{"
	cmd := newObjectLifecycleSetCommand()
	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid policy JSON")
}

// =============================================================================
// objectstorage.go coverage - runObjectRestoreCommand tier parsing
// =============================================================================

func TestRunObjectRestoreCommand_HotTier(t *testing.T) {
	origFactory := objectStorageClientFactory
	origTier := objectRestoreTier
	defer func() {
		objectStorageClientFactory = origFactory
		objectRestoreTier = origTier
	}()

	objectRestoreTier = "hot"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectRestoreCommand()
	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectRestoreCommand_WarmTier(t *testing.T) {
	origFactory := objectStorageClientFactory
	origTier := objectRestoreTier
	defer func() {
		objectStorageClientFactory = origFactory
		objectRestoreTier = origTier
	}()

	objectRestoreTier = "warm"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectRestoreCommand()
	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectRestoreCommand_UnspecifiedTier(t *testing.T) {
	origFactory := objectStorageClientFactory
	origTier := objectRestoreTier
	defer func() {
		objectStorageClientFactory = origFactory
		objectRestoreTier = origTier
	}()

	objectRestoreTier = "unknown"
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectRestoreCommand()
	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

// =============================================================================
// objectstorage.go coverage - runObjectPutCommand with valid file + metadata
// =============================================================================

func TestRunObjectPutCommand_ValidFileValidMetadata(t *testing.T) {
	origFactory := objectStorageClientFactory
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	defer func() {
		objectStorageClientFactory = origFactory
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
	}()

	tmpFile := filepath.Join(t.TempDir(), "testdata.txt")
	err := os.WriteFile(tmpFile, []byte("test data content"), 0600)
	require.NoError(t, err)

	objectFile = tmpFile
	objectMetadata = `{"env":"prod","tier":"hot"}`
	objectTags = nil

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectPutCommand()
	err = runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

// =============================================================================
// cluster.go coverage - printClusterStatusTableCLI with raft groups and nodes
// =============================================================================

func TestPrintClusterStatusTableCLI_WithRaftGroupsAndAllPaths(t *testing.T) {
	now := time.Now()
	status := &cli.ClusterStatusResponse{
		Healthy:    false,
		LeaderID:   2,
		NodeCount:  3,
		ReadyNodes: 2,
		Nodes: []cli.NodeInfo{
			{
				NodeID:    1,
				Address:   "10.0.0.1:50051",
				IsLeader:  false,
				IsHealthy: false,
				LastSeen:  now.Add(-10 * time.Second),
			},
			{
				NodeID:    2,
				Address:   "10.0.0.2:50051",
				IsLeader:  true,
				IsHealthy: true,
				LastSeen:  now.Add(-1 * time.Second),
			},
			{
				NodeID:    3,
				Address:   "10.0.0.3:50051",
				IsLeader:  false,
				IsHealthy: true,
				LastSeen:  time.Time{},
			},
		},
		RaftGroupInfo: []cli.RaftGroupInfo{
			{GroupID: 1, LeaderID: 2, MemberCount: 3},
			{GroupID: 2, LeaderID: 1, MemberCount: 2},
		},
	}

	err := printClusterStatusTableCLI(status)
	assert.NoError(t, err)
}

// =============================================================================
// rebalance.go coverage - followRebalanceProgressSDK
// =============================================================================

// mockClusterClientForRebalance implements cluster.Client for testing.
type mockClusterClientForRebalance struct {
	statusFunc      func(ctx context.Context) (*cluster.ClusterStatus, error)
	nodesFunc       func(ctx context.Context) (*cluster.NodesResponse, error)
	addNodeFunc     func(ctx context.Context, req *cluster.AddNodeRequest) error
	removeNodeFunc  func(ctx context.Context, req *cluster.RemoveNodeRequest) error
	rebalanceFunc   func(ctx context.Context, req *cluster.RebalanceClusterRequest) (*cluster.RebalanceClusterResponse, error)
	getProgressFunc func(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error)
}

func (m *mockClusterClientForRebalance) Status(ctx context.Context) (*cluster.ClusterStatus, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return nil, errors.New(errors.ErrUnimplemented, "mock", "not implemented")
}

func (m *mockClusterClientForRebalance) Nodes(ctx context.Context) (*cluster.NodesResponse, error) {
	if m.nodesFunc != nil {
		return m.nodesFunc(ctx)
	}
	return nil, errors.New(errors.ErrUnimplemented, "mock", "not implemented")
}

func (m *mockClusterClientForRebalance) AddNode(ctx context.Context, req *cluster.AddNodeRequest) error {
	if m.addNodeFunc != nil {
		return m.addNodeFunc(ctx, req)
	}
	return errors.New(errors.ErrUnimplemented, "mock", "not implemented")
}

func (m *mockClusterClientForRebalance) RemoveNode(ctx context.Context, req *cluster.RemoveNodeRequest) error {
	if m.removeNodeFunc != nil {
		return m.removeNodeFunc(ctx, req)
	}
	return errors.New(errors.ErrUnimplemented, "mock", "not implemented")
}

func (m *mockClusterClientForRebalance) RebalanceCluster(ctx context.Context, req *cluster.RebalanceClusterRequest) (*cluster.RebalanceClusterResponse, error) {
	if m.rebalanceFunc != nil {
		return m.rebalanceFunc(ctx, req)
	}
	return nil, errors.New(errors.ErrUnimplemented, "mock", "not implemented")
}

func (m *mockClusterClientForRebalance) GetRebalanceProgress(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error) {
	if m.getProgressFunc != nil {
		return m.getProgressFunc(ctx, req)
	}
	return nil, errors.New(errors.ErrUnimplemented, "mock", "not implemented")
}

// Compile-time interface check.
var _ cluster.Client = (*mockClusterClientForRebalance)(nil)

func TestFollowRebalanceProgressSDK_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &mockClusterClientForRebalance{}

	err := followRebalanceProgressSDK(ctx, client, "op-123", "table")
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFollowRebalanceProgressSDK_CompletedStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	callCount := 0
	client := &mockClusterClientForRebalance{
		getProgressFunc: func(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error) {
			callCount++
			return &cluster.GetRebalanceProgressResponse{
				OperationID:     "op-123",
				Status:          "completed",
				PercentComplete: 100.0,
				ShardsMoved:     5,
				TotalShards:     5,
				ElapsedTime:     int64(time.Minute),
			}, nil
		},
	}

	err := followRebalanceProgressSDK(ctx, client, "op-123", "table")
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 1)
}

func TestFollowRebalanceProgressSDK_FailedStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := &mockClusterClientForRebalance{
		getProgressFunc: func(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error) {
			return &cluster.GetRebalanceProgressResponse{
				OperationID:     "op-456",
				Status:          "failed",
				PercentComplete: 50.0,
				ShardsMoved:     3,
				TotalShards:     6,
			}, nil
		},
	}

	err := followRebalanceProgressSDK(ctx, client, "op-456", "table")
	assert.NoError(t, err)
}

func TestFollowRebalanceProgressSDK_ErrorThenComplete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	callCount := 0
	client := &mockClusterClientForRebalance{
		getProgressFunc: func(ctx context.Context, req *cluster.GetRebalanceProgressRequest) (*cluster.GetRebalanceProgressResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New(errors.ErrInternal, "test", "temporary error")
			}
			return &cluster.GetRebalanceProgressResponse{
				OperationID:     "op-789",
				Status:          "completed",
				PercentComplete: 100.0,
				ShardsMoved:     5,
				TotalShards:     5,
			}, nil
		},
	}

	err := followRebalanceProgressSDK(ctx, client, "op-789", "table")
	assert.NoError(t, err)
}

// =============================================================================
// Mock RBAC client ListRoles coverage
// =============================================================================

func TestMockRBACClient_ListRoles_DefaultBehavior(t *testing.T) {
	client := &MockRBACClient{}
	resp, err := client.ListRoles(context.Background(), &rbac.ListRolesRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// =============================================================================
// Helper
// =============================================================================

func createTestLoggerForCoverageNinetyOne(t *testing.T) *logger.Logger {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return testLog
}

func TestFormatObjectPutResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.PutObjectResponse{
		Key:      "bucket/test-key",
		Size:     1024,
		Version:  1,
		Checksum: []byte{0xaa, 0xbb, 0xcc},
	}
	err := formatObjectPutResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPutResponse_WithTimestampAndReplicas(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.PutObjectResponse{
		Key:          "bucket/test-key",
		Size:         2048,
		Version:      3,
		Checksum:     []byte{0xde, 0xad},
		CreatedAt:    timestamppb.New(now),
		ReplicatedTo: []string{"store-a", "store-b"},
	}
	err := formatObjectPutResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPutResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.PutObjectResponse{
		Key:     "bucket/json-key",
		Size:    512,
		Version: 1,
	}
	err := formatObjectPutResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPutResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.PutObjectResponse{
		Key:     "bucket/yaml-key",
		Size:    256,
		Version: 1,
	}
	err := formatObjectPutResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPutResponse_InvalidFormatErr(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "invalid-fmt"

	resp := &pb.PutObjectResponse{Key: "bucket/key"}
	err := formatObjectPutResponse(resp)
	assert.Error(t, err)
}

func TestFormatObjectGetResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.GetObjectResponse{
		Key:   "bucket/get-key",
		Found: true,
		Value: []byte("hello world"),
		Size:  11,
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectGetResponse_JSONWithMeta(t *testing.T) {
	orig := objectOutputFormat
	origMeta := objectIncludeMeta
	defer func() {
		objectOutputFormat = orig
		objectIncludeMeta = origMeta
	}()
	objectOutputFormat = "json"
	objectIncludeMeta = true

	resp := &pb.GetObjectResponse{
		Key:         "bucket/json-get",
		Found:       true,
		Value:       []byte("data"),
		Size:        4,
		Version:     2,
		ContentType: "text/plain",
		Checksum:    []byte{0x01, 0x02},
		Tags:        map[string]string{"env": "prod"},
		Metadata: &pb.ObjectMetadata{
			Owner: "admin",
		},
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectGetResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.GetObjectResponse{
		Key:   "bucket/yaml-get",
		Found: true,
		Tags:  map[string]string{"tier": "hot"},
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectGetResponse_RawOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "raw"

	resp := &pb.GetObjectResponse{
		Key:   "bucket/raw-get",
		Found: true,
		Value: []byte("raw binary content"),
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectDeleteResponse_DefaultDeleted(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.DeleteObjectResponse{
		Key:       "bucket/del-key",
		Deleted:   true,
		Permanent: false,
		DeletedAt: timestamppb.New(now),
	}
	err := formatObjectDeleteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectDeleteResponse_DefaultPermanent(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.DeleteObjectResponse{
		Key:       "bucket/del-perm",
		Deleted:   true,
		Permanent: true,
	}
	err := formatObjectDeleteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectDeleteResponse_DefaultNotFound(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.DeleteObjectResponse{
		Key:     "bucket/not-found",
		Deleted: false,
	}
	err := formatObjectDeleteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectDeleteResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.DeleteObjectResponse{Key: "bucket/del-json", Deleted: true}
	err := formatObjectDeleteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectDeleteResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.DeleteObjectResponse{Key: "bucket/del-yaml", Deleted: true}
	err := formatObjectDeleteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectHeadResponse_DefaultExists(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.HeadObjectResponse{
		Key:         "bucket/head-key",
		Exists:      true,
		Size:        4096,
		ContentType: "application/json",
		Version:     5,
		Checksum:    []byte{0xff},
		Status:      pb.ObjectStatus_OBJECT_STATUS_ACTIVE,
		Tier:        pb.StorageTier_STORAGE_TIER_HOT,
		ModifiedAt:  timestamppb.New(now),
		Tags:        map[string]string{"env": "staging"},
	}
	err := formatObjectHeadResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectHeadResponse_DefaultNotFound(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.HeadObjectResponse{
		Key:    "bucket/missing",
		Exists: false,
	}
	err := formatObjectHeadResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectHeadResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.HeadObjectResponse{Key: "bucket/head-json", Exists: true, Size: 100}
	err := formatObjectHeadResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectHeadResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.HeadObjectResponse{Key: "bucket/head-yaml", Exists: true}
	err := formatObjectHeadResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.ListObjectsResponse{
		Objects: []*pb.ObjectInfo{
			{Key: "bucket/obj1", Size: 100, Version: 1, ModifiedAt: timestamppb.New(now)},
			{Key: "bucket/obj2", Size: 200, Version: 2},
		},
		Total:      2,
		NextCursor: "cursor-abc",
		HasMore:    true,
	}
	err := formatObjectListResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.ListObjectsResponse{Total: 0}
	err := formatObjectListResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.ListObjectsResponse{Total: 1, Objects: []*pb.ObjectInfo{{Key: "k"}}}
	err := formatObjectListResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTagResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ListByTagResponse{
		Keys:       []string{"key1", "key2"},
		Total:      2,
		NextCursor: "next",
		HasMore:    true,
	}
	err := formatObjectListByTagResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTagResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.ListByTagResponse{Keys: []string{"k"}, Total: 1}
	err := formatObjectListByTagResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTagResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.ListByTagResponse{Keys: []string{"k"}, Total: 1}
	err := formatObjectListByTagResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTenantResponse_DefaultWithObjects(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ListByTenantResponse{
		Objects: []*pb.ObjectInfo{
			{Key: "tenant/obj1", Size: 100, Version: 1},
		},
		Total:      1,
		NextCursor: "c2",
		HasMore:    true,
	}
	err := formatObjectListByTenantResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTenantResponse_DefaultWithKeys(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ListByTenantResponse{
		Keys:  []string{"key-a", "key-b"},
		Total: 2,
	}
	err := formatObjectListByTenantResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTenantResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.ListByTenantResponse{Total: 0}
	err := formatObjectListByTenantResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectListByTenantResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.ListByTenantResponse{Total: 0}
	err := formatObjectListByTenantResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectCopyResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.CopyObjectResponse{
		Key:       "bucket/dest-key",
		Version:   2,
		Size:      512,
		CreatedAt: timestamppb.New(now),
	}
	err := formatObjectCopyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectCopyResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.CopyObjectResponse{Key: "bucket/copy-json"}
	err := formatObjectCopyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectArchiveResponse_DefaultSuccess(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.ArchiveObjectResponse{
		Key:          "bucket/archive-key",
		Success:      true,
		ArchiveStore: "cold-store",
		OriginalSize: 2048,
		ArchivedSize: 1024,
		ArchivedAt:   timestamppb.New(now),
	}
	err := formatObjectArchiveResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectArchiveResponse_DefaultFailed(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ArchiveObjectResponse{Key: "bucket/archive-fail", Success: false}
	err := formatObjectArchiveResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectArchiveResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.ArchiveObjectResponse{Key: "bucket/archive-json", Success: true}
	err := formatObjectArchiveResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectRestoreResponse_DefaultSuccess(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.RestoreObjectResponse{
		Key:                 "bucket/restore-key",
		Success:             true,
		RestoredTo:          "hot-store",
		OperationId:         "op-123",
		EstimatedCompletion: timestamppb.New(now.Add(time.Hour)),
	}
	err := formatObjectRestoreResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectRestoreResponse_DefaultFailed(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.RestoreObjectResponse{Key: "bucket/restore-fail", Success: false}
	err := formatObjectRestoreResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectRestoreResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.RestoreObjectResponse{Key: "bucket/restore-json", Success: true}
	err := formatObjectRestoreResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectVerifyResponse_DefaultValid(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.VerifyObjectResponse{
		Key:      "bucket/verify-key",
		Valid:    true,
		Repaired: false,
		Locations: []*pb.LocationVerification{
			{Store: "store-a", Valid: true, Size: 100},
			{Store: "store-b", Valid: false, Error: "checksum mismatch", Size: 100},
		},
	}
	err := formatObjectVerifyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectVerifyResponse_DefaultInvalid(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.VerifyObjectResponse{Key: "bucket/verify-inv", Valid: false, Repaired: true}
	err := formatObjectVerifyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectVerifyResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.VerifyObjectResponse{Key: "bucket/verify-json", Valid: true}
	err := formatObjectVerifyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectReplicateResponse_DefaultSuccess(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ReplicateObjectResponse{
		Key:          "bucket/rep-key",
		Success:      true,
		ReplicatedTo: []string{"store-a", "store-b"},
		OperationId:  "rep-op-1",
	}
	err := formatObjectReplicateResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectReplicateResponse_DefaultPartialFailed(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ReplicateObjectResponse{
		Key:          "bucket/rep-fail",
		Success:      false,
		ReplicatedTo: []string{"store-a"},
		FailedStores: []string{"store-c"},
	}
	err := formatObjectReplicateResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectReplicateResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.ReplicateObjectResponse{Key: "bucket/rep-json", Success: true}
	err := formatObjectReplicateResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectMigrateResponse_DefaultSuccess(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.MigrateObjectResponse{
		Key:           "bucket/mig-key",
		Success:       true,
		OldBackend:    "cold-store",
		NewBackend:    "hot-store",
		SourceDeleted: true,
		Verified:      true,
	}
	err := formatObjectMigrateResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectMigrateResponse_DefaultFailed(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.MigrateObjectResponse{
		Key:        "bucket/mig-fail",
		Success:    false,
		OldBackend: "hot",
		NewBackend: "cold",
	}
	err := formatObjectMigrateResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectMigrateResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.MigrateObjectResponse{Key: "bucket/mig-json", Success: true}
	err := formatObjectMigrateResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPolicyResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.GetObjectPolicyResponse{
		Key: "bucket/pol-key",
		Policy: &pb.ObjectPolicy{
			ReplicationPolicy: &pb.ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  3,
				Mode:         pb.ReplicationMode_REPLICATION_MODE_SYNC,
				TargetStores: []string{"store-a", "store-b"},
			},
			ArchivePolicy: &pb.ArchivePolicy{
				Enabled:           true,
				ArchiveAfterDays:  30,
				TargetStore:       "archive-store",
				DeleteFromPrimary: true,
			},
			RetentionPolicy: &pb.RetentionPolicy{
				Enabled:       true,
				RetentionDays: 90,
				MaxVersions:   5,
				LegalHold:     false,
			},
		},
		ExplicitPolicy: &pb.ObjectPolicy{
			RetentionPolicy: &pb.RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
				MaxVersions:   3,
			},
		},
		InheritedPolicy: &pb.ObjectPolicy{
			ReplicationPolicy: &pb.ReplicationPolicy{
				Enabled:     true,
				MinReplicas: 2,
			},
		},
	}
	err := formatObjectPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPolicyResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.GetObjectPolicyResponse{Key: "bucket/pol-json"}
	err := formatObjectPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPolicyResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.GetObjectPolicyResponse{Key: "bucket/pol-yaml"}
	err := formatObjectPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	now := time.Now()
	resp := &pb.SetObjectPolicyResponse{
		Key:       "bucket/setpol-key",
		Version:   2,
		UpdatedAt: timestamppb.New(now),
	}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/setpol-json", Version: 1}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectStatsResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.ListBackendsResponse{
		Total: 2,
		Backends: []*pb.BackendInfo{
			{
				Name:          "primary-store",
				Type:          pb.BackendType_BACKEND_TYPE_LOCAL,
				Tier:          pb.StorageTier_STORAGE_TIER_HOT,
				Enabled:       true,
				Healthy:       true,
				UsedBytes:     1073741824,
				CapacityBytes: 10737418240,
			},
			{
				Name:          "archive-store",
				Type:          pb.BackendType_BACKEND_TYPE_S3,
				Tier:          pb.StorageTier_STORAGE_TIER_ARCHIVE,
				Enabled:       true,
				Healthy:       false,
				UsedBytes:     536870912,
				CapacityBytes: 1099511627776,
			},
		},
	}
	err := formatObjectStatsResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectStatsResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.ListBackendsResponse{Total: 0}
	err := formatObjectStatsResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectStatsResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.ListBackendsResponse{Total: 0}
	err := formatObjectStatsResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_DefaultOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.GetObjectPolicyResponse{
		Key: "bucket/*",
		Policy: &pb.ObjectPolicy{
			RetentionPolicy: &pb.RetentionPolicy{
				Enabled:       true,
				RetentionDays: 365,
			},
		},
	}
	err := formatObjectLifecycleResponse("bucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_DefaultNoPolicy(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.GetObjectPolicyResponse{Key: "bucket/*"}
	err := formatObjectLifecycleResponse("bucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_JSONOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.GetObjectPolicyResponse{Key: "bucket/*"}
	err := formatObjectLifecycleResponse("bucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_YAMLOutput(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.GetObjectPolicyResponse{Key: "bucket/*"}
	err := formatObjectLifecycleResponse("bucket", resp)
	assert.NoError(t, err)
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestObjectTruncateStringCoverage(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated with ellipsis", "hello world", 8, "hello..."},
		{"very short max", "hello", 3, "hel"},
		{"max of 1", "hello", 1, "h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := objectTruncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestObjectFormatStorageSizeCoverage(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero bytes", 0, "0 B"},
		{"small bytes", 512, "512 B"},
		{"one KB", 1024, "1.0 KB"},
		{"megabytes", 1048576, "1.0 MB"},
		{"gigabytes", 1073741824, "1.0 GB"},
		{"terabytes", 1099511627776, "1.0 TB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := objectFormatStorageSize(tt.bytes)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// Object Storage Command Factory Error Tests
// =============================================================================

func TestRunObjectPutCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	testErr := &errors.DragonError{
		Code: errors.ErrUnavailable,
		Op:   "test",
		Err:  errors.New(errors.ErrUnavailable, "test", "connection refused"),
	}
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, testErr
	}

	cmd := newObjectPutCommand()
	objectFile = ""
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectPutCommand_NoData(t *testing.T) {
	origFile := objectFile
	defer func() { objectFile = origFile }()
	objectFile = ""

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectPutCommand_BadFile(t *testing.T) {
	origFile := objectFile
	defer func() { objectFile = origFile }()
	objectFile = "/nonexistent/path/to/file.dat"

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectPutCommand_BadMetadataJSON(t *testing.T) {
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	defer func() {
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
	}()

	objectFile = "/dev/null"
	objectMetadata = "not-valid-json"
	objectTags = nil

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectGetCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectGetCommand()
	err := runObjectGetCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectDeleteCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectDeleteCommand()
	err := runObjectDeleteCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectHeadCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectHeadCommand()
	err := runObjectHeadCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectListCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectListCommand()
	err := runObjectListCommand(cmd, []string{"test-namespace"})
	assert.Error(t, err)
}

func TestRunObjectListByTagCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectListByTagCommand()
	err := runObjectListByTagCommand(cmd, []string{"bucket"})
	assert.Error(t, err)
}

func TestRunObjectListByTenantCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectListByTenantCommand()
	err := runObjectListByTenantCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRunObjectCopyCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectCopyCommand()
	err := runObjectCopyCommand(cmd, []string{"src/key", "dst/key"})
	assert.Error(t, err)
}

func TestRunObjectArchiveCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectArchiveCommand()
	err := runObjectArchiveCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectRestoreCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectRestoreCommand()
	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectVerifyCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectVerifyCommand()
	err := runObjectVerifyCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectReplicateCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectReplicateCommand()
	err := runObjectReplicateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectMigrateCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectMigrateCommand()
	err := runObjectMigrateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectMetadataGetCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectMetadataGetCommand()
	err := runObjectMetadataGetCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectMetadataUpdateCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectMetadataUpdateCommand()
	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectStatsCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectStatsCommand()
	err := runObjectStatsCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestRunObjectLifecycleSetCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectLifecycleSetCommand()
	err := runObjectLifecycleSetCommand(cmd, []string{"bucket"})
	assert.Error(t, err)
}

func TestRunObjectLifecycleGetCommand_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	defer func() { objectStorageClientFactory = origFactory }()

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, &errors.DragonError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "fail")}
	}

	cmd := newObjectLifecycleGetCommand()
	err := runObjectLifecycleGetCommand(cmd, []string{"bucket"})
	assert.Error(t, err)
}

// =============================================================================
// PrintObjectPolicy Test
// =============================================================================

func TestPrintObjectPolicyAllPolicies(t *testing.T) {
	policy := &pb.ObjectPolicy{
		ReplicationPolicy: &pb.ReplicationPolicy{
			Enabled:      true,
			MinReplicas:  3,
			Mode:         pb.ReplicationMode_REPLICATION_MODE_ASYNC,
			TargetStores: []string{"store-x"},
		},
		ArchivePolicy: &pb.ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  60,
			TargetStore:       "cold",
			DeleteFromPrimary: false,
		},
		RetentionPolicy: &pb.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 365,
			MaxVersions:   10,
			LegalHold:     true,
		},
	}
	printObjectPolicy(policy)
}

func TestPrintObjectPolicyNilSubPolicies(t *testing.T) {
	policy := &pb.ObjectPolicy{}
	printObjectPolicy(policy)
}

// =============================================================================
// New Command Builder Tests
// =============================================================================

func TestNewObjectStorageCommandsCoverage(t *testing.T) {
	cmd := newObjectStorageCommands()
	require.NotNil(t, cmd)
	assert.Equal(t, "object", cmd.Use)
	assert.True(t, len(cmd.Commands()) > 0, "should have subcommands")
}

func TestNewObjectPutCommandCoverage(t *testing.T) {
	cmd := newObjectPutCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "put")
}

func TestNewObjectGetCommandCoverage(t *testing.T) {
	cmd := newObjectGetCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "get")
}

func TestNewObjectDeleteCommandCoverage(t *testing.T) {
	cmd := newObjectDeleteCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "delete")
}

func TestNewObjectHeadCommandCoverage(t *testing.T) {
	cmd := newObjectHeadCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "head")
}

func TestNewObjectListCommandCoverage(t *testing.T) {
	cmd := newObjectListCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "list")
}

func TestNewObjectCopyCommandCoverage(t *testing.T) {
	cmd := newObjectCopyCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "copy")
}

func TestNewObjectArchiveCommandCoverage(t *testing.T) {
	cmd := newObjectArchiveCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "archive")
}

func TestNewObjectRestoreCommandCoverage(t *testing.T) {
	cmd := newObjectRestoreCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "restore")
}

func TestNewObjectVerifyCommandCoverage(t *testing.T) {
	cmd := newObjectVerifyCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "verify")
}

func TestNewObjectReplicateCommandCoverage(t *testing.T) {
	cmd := newObjectReplicateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "replicate")
}

func TestNewObjectMigrateCommandCoverage(t *testing.T) {
	cmd := newObjectMigrateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "migrate")
}

func TestNewObjectStatsCommandCoverage(t *testing.T) {
	cmd := newObjectStatsCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "stats")
}

func TestNewObjectMetadataGetCommandCoverage(t *testing.T) {
	cmd := newObjectMetadataGetCommand()
	require.NotNil(t, cmd)
}

func TestNewObjectMetadataUpdateCommandCoverage(t *testing.T) {
	cmd := newObjectMetadataUpdateCommand()
	require.NotNil(t, cmd)
}

func TestNewObjectLifecycleSetCommandCoverage(t *testing.T) {
	cmd := newObjectLifecycleSetCommand()
	require.NotNil(t, cmd)
}

func TestNewObjectLifecycleGetCommandCoverage(t *testing.T) {
	cmd := newObjectLifecycleGetCommand()
	require.NotNil(t, cmd)
}

func TestNewObjectListByTagCommandCoverage(t *testing.T) {
	cmd := newObjectListByTagCommand()
	require.NotNil(t, cmd)
}

func TestNewObjectListByTenantCommandCoverage(t *testing.T) {
	cmd := newObjectListByTenantCommand()
	require.NotNil(t, cmd)
}

func newDeadGRPCClient(t *testing.T) *grpc.Client {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stderr",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	client, err := grpc.NewClient("127.0.0.1:1",
		grpc.WithLogger(log),
		grpc.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}), //nolint:gosec // G402: unit test
		grpc.WithTimeout(100*time.Millisecond),
		grpc.WithRetry(0, 10*time.Millisecond),
	)
	require.NoError(t, err)
	return client
}

// saveAndRestoreObjectGlobals saves all objectstorage globals and returns a restore function.
func saveAndRestoreObjectGlobals(t *testing.T) func() {
	t.Helper()
	origFactory := objectStorageClientFactory
	origOutputFormat := objectOutputFormat
	origTimeout := objectTimeout
	origTLSEnabled := objectTLSEnabled
	origTLSCert := objectTLSCert
	origTLSKey := objectTLSKey
	origTLSCA := objectTLSCA
	origMaxRetries := objectMaxRetries
	origRetryBackoff := objectRetryBackoff
	origFile := objectFile
	origMetadata := objectMetadata
	origContentType := objectContentType
	origTenantID := globalTenantID
	origTags := objectTags
	origOutputPath := objectOutputPath
	origIncludeMeta := objectIncludeMeta
	origPrefix := objectPrefix
	origLimit := objectLimit
	origCursor := objectCursor
	origTagFilter := objectTagFilter
	origCopyTags := objectCopyTags
	origDestBackend := objectDestBackend
	origDeleteSource := objectDeleteSource
	origSync := objectSync
	origTargetStore := objectTargetStore
	origDeleteFromPrimary := objectDeleteFromPrimary
	origCompressionLevel := objectCompressionLevel
	origRestoreDays := objectRestoreDays
	origRestoreTier := objectRestoreTier
	origCheckAll := objectCheckAll
	origRepair := objectRepair
	origPolicyJSON := objectPolicyJSON

	return func() {
		objectStorageClientFactory = origFactory
		objectOutputFormat = origOutputFormat
		objectTimeout = origTimeout
		objectTLSEnabled = origTLSEnabled
		objectTLSCert = origTLSCert
		objectTLSKey = origTLSKey
		objectTLSCA = origTLSCA
		objectMaxRetries = origMaxRetries
		objectRetryBackoff = origRetryBackoff
		objectFile = origFile
		objectMetadata = origMetadata
		objectContentType = origContentType
		globalTenantID = origTenantID
		objectTags = origTags
		objectOutputPath = origOutputPath
		objectIncludeMeta = origIncludeMeta
		objectPrefix = origPrefix
		objectLimit = origLimit
		objectCursor = origCursor
		objectTagFilter = origTagFilter
		objectCopyTags = origCopyTags
		objectDestBackend = origDestBackend
		objectDeleteSource = origDeleteSource
		objectSync = origSync
		objectTargetStore = origTargetStore
		objectDeleteFromPrimary = origDeleteFromPrimary
		objectCompressionLevel = origCompressionLevel
		objectRestoreDays = origRestoreDays
		objectRestoreTier = origRestoreTier
		objectCheckAll = origCheckAll
		objectRepair = origRepair
		objectPolicyJSON = origPolicyJSON
	}
}

// =============================================================================
// Object Storage run*Command - RPC failure paths (covers context creation,
// request building, and formatError return)
//
// IMPORTANT: Globals must be set AFTER newObject*Command() because the command
// constructor registers flags with StringVar/BoolVar which reset the global
// to its default value.
// =============================================================================

func TestObjectStorage91_PutCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	tmpFile := filepath.Join(t.TempDir(), "testdata.txt")
	err := os.WriteFile(tmpFile, []byte("test content"), 0600)
	require.NoError(t, err)

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	// Create command first (registers flags, resets globals to defaults).
	cmd := newObjectPutCommand()

	// Set globals AFTER command creation so they are not reset.
	objectFile = tmpFile
	objectContentType = "text/plain"
	globalTenantID = "tenant-1"
	objectTags = map[string]string{"env": "test"}
	objectTimeout = 500 * time.Millisecond

	err = runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err, "RPC call to dead server should fail")
}

func TestObjectStorage91_PutCommand_MetadataWithExistingTags(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	tmpFile := filepath.Join(t.TempDir(), "testdata.txt")
	err := os.WriteFile(tmpFile, []byte("data"), 0600)
	require.NoError(t, err)

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "mock error")
	}

	// Create command first, then set globals.
	cmd := newObjectPutCommand()
	objectFile = tmpFile
	objectMetadata = `{"key1":"val1"}`
	objectTags = map[string]string{"existing": "tag"}
	objectTimeout = 500 * time.Millisecond

	err = runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_PutCommand_MetadataWithoutTags(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	tmpFile := filepath.Join(t.TempDir(), "testdata.txt")
	err := os.WriteFile(tmpFile, []byte("data"), 0600)
	require.NoError(t, err)

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	// Create command first, then set globals.
	cmd := newObjectPutCommand()
	objectFile = tmpFile
	objectMetadata = `{"key1":"val1"}`
	objectTags = nil
	objectTimeout = 500 * time.Millisecond

	err = runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_PutCommand_InvalidMetadataJSON(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	tmpFile := filepath.Join(t.TempDir(), "testdata.txt")
	err := os.WriteFile(tmpFile, []byte("data"), 0600)
	require.NoError(t, err)

	objectStorageClientFactory = func() (*grpc.Client, error) {
		return newDeadGRPCClient(t), nil
	}

	cmd := newObjectPutCommand()
	objectFile = tmpFile
	objectMetadata = `{invalid-json}`
	objectTags = nil
	objectTimeout = 500 * time.Millisecond

	err = runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err, "invalid metadata JSON should return error")
}

func TestObjectStorage91_PutCommand_NoData(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	cmd := newObjectPutCommand()
	objectFile = ""
	objectTimeout = 500 * time.Millisecond

	// No file and stdin is a terminal => should error with "no data provided"
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_GetCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectGetCommand()
	globalTenantID = "tenant-1"
	objectIncludeMeta = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectGetCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err, "RPC call to dead server should fail")
}

func TestObjectStorage91_DeleteCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectDeleteCommand()
	globalTenantID = "tenant-1"
	objectDeleteSource = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectDeleteCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_HeadCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectHeadCommand()
	globalTenantID = "tenant-1"
	objectTimeout = 500 * time.Millisecond

	err := runObjectHeadCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_ListCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectListCommand()
	objectPrefix = "images/"
	globalTenantID = "tenant-1"
	objectLimit = 50
	objectCursor = "cursor-abc"
	objectIncludeMeta = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectListCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
}

func TestObjectStorage91_ListByTagCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectListByTagCommand()
	objectTagFilter = "type=document"
	globalTenantID = "tenant-1"
	objectLimit = 50
	objectCursor = ""
	objectTimeout = 500 * time.Millisecond

	err := runObjectListByTagCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestObjectStorage91_ListByTagCommand_InvalidTagFormat(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	cmd := newObjectListByTagCommand()
	objectTagFilter = "invalidnoequals"
	objectTimeout = 500 * time.Millisecond

	err := runObjectListByTagCommand(cmd, []string{})
	assert.Error(t, err, "tag without = separator should return error")
}

func TestObjectStorage91_ListByTenantCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectListByTenantCommand()
	globalTenantID = "tenant-123"
	objectLimit = 100
	objectCursor = ""
	objectIncludeMeta = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectListByTenantCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestObjectStorage91_CopyCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectCopyCommand()
	objectCopyTags = true
	globalTenantID = "tenant-1"
	objectTimeout = 500 * time.Millisecond

	err := runObjectCopyCommand(cmd, []string{"src/key", "dst/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_ArchiveCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectArchiveCommand()
	objectTargetStore = "cold-store"
	objectDeleteFromPrimary = true
	objectCompressionLevel = 6
	objectTimeout = 500 * time.Millisecond

	err := runObjectArchiveCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_RestoreCommand_RPCFailure_HotTier(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectRestoreCommand()
	objectDestBackend = "hot-backend"
	objectRestoreDays = 14
	objectRestoreTier = "hot"
	objectTimeout = 500 * time.Millisecond

	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_RestoreCommand_RPCFailure_WarmTier(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectRestoreCommand()
	objectRestoreTier = "warm"
	objectTimeout = 500 * time.Millisecond

	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_RestoreCommand_RPCFailure_DefaultTier(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectRestoreCommand()
	objectRestoreTier = ""
	objectTimeout = 500 * time.Millisecond

	err := runObjectRestoreCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_VerifyCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectVerifyCommand()
	objectCheckAll = true
	objectRepair = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectVerifyCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_ReplicateCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectReplicateCommand()
	objectDestBackend = "store-a, store-b"
	objectSync = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectReplicateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_MigrateCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectMigrateCommand()
	objectDestBackend = "archive-backend"
	objectDeleteSource = true
	objectTimeout = 500 * time.Millisecond

	err := runObjectMigrateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_MetadataGetCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectMetadataGetCommand()
	objectTimeout = 500 * time.Millisecond

	err := runObjectMetadataGetCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_MetadataUpdateCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	// Create command first (resets objectMetadata to "").
	cmd := newObjectMetadataUpdateCommand()
	// Set globals after command creation.
	objectMetadata = `{"replication":{"enabled":true,"min_replicas":3}}`
	objectTimeout = 500 * time.Millisecond

	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestObjectStorage91_MetadataUpdateCommand_InvalidJSON(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	cmd := newObjectMetadataUpdateCommand()
	objectMetadata = `{not valid json}`
	objectTimeout = 500 * time.Millisecond

	err := runObjectMetadataUpdateCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err, "invalid metadata JSON should return error")
}

func TestObjectStorage91_StatsCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectStatsCommand()
	objectTimeout = 500 * time.Millisecond

	err := runObjectStatsCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestObjectStorage91_LifecycleSetCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	// Create command first (resets objectPolicyJSON to "").
	cmd := newObjectLifecycleSetCommand()
	// Set globals after command creation.
	objectPolicyJSON = `{"archive":{"enabled":true,"archive_after_days":30}}`
	objectTimeout = 500 * time.Millisecond

	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
}

func TestObjectStorage91_LifecycleSetCommand_InvalidJSON(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	cmd := newObjectLifecycleSetCommand()
	objectPolicyJSON = `{not valid}`
	objectTimeout = 500 * time.Millisecond

	err := runObjectLifecycleSetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err, "invalid policy JSON should return error")
}

func TestObjectStorage91_LifecycleGetCommand_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	defer restore()

	client := newDeadGRPCClient(t)
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return client, nil
	}

	cmd := newObjectLifecycleGetCommand()
	objectTimeout = 500 * time.Millisecond

	err := runObjectLifecycleGetCommand(cmd, []string{"mybucket"})
	assert.Error(t, err)
}

// =============================================================================
// createObjectStorageGRPCClient - TLS required (insecure path removed)
// =============================================================================

func TestObjectStorage91_CreateGRPCClient_TLSRequired(t *testing.T) {
	origTLSEnabled := objectTLSEnabled
	origAddress := address
	origTimeout := objectTimeout
	origMaxRetries := objectMaxRetries
	origRetryBackoff := objectRetryBackoff
	defer func() {
		objectTLSEnabled = origTLSEnabled
		address = origAddress
		objectTimeout = origTimeout
		objectMaxRetries = origMaxRetries
		objectRetryBackoff = origRetryBackoff
	}()

	objectTLSEnabled = false
	address = "127.0.0.1:1"
	objectTimeout = 100 * time.Millisecond
	objectMaxRetries = 0
	objectRetryBackoff = 10 * time.Millisecond

	client, err := createObjectStorageGRPCClient()
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

// =============================================================================
// Embedded server tests - additional coverage paths
// =============================================================================

func TestEmbedded91_GetOrCreateEmbeddedServer_FastPath(t *testing.T) {
	defer func() {
		embeddedServerHolder.Store(nil)
	}()

	// Store a pre-existing server in the holder
	existing := &embeddedServer{}
	existing.started.Store(true)
	embeddedServerHolder.Store(existing)

	// Fast path: returns existing server without creating a new one
	srv, err := getOrCreateEmbeddedServer()
	assert.NoError(t, err)
	assert.Same(t, existing, srv)
}

func TestEmbedded91_GetEmbeddedService_WithExistingServer(t *testing.T) {
	defer func() {
		embeddedServerHolder.Store(nil)
	}()

	// Store a server with a nil service to trigger a non-nil srv but nil service scenario
	existing := &embeddedServer{}
	existing.started.Store(true)
	embeddedServerHolder.Store(existing)

	svc, err := getEmbeddedService()
	assert.NoError(t, err)
	// service will be nil since we didn't set it, but getEmbeddedService doesn't check that
	assert.Nil(t, svc)
}

func TestEmbedded91_Shutdown_WithTempDir(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "embedded-test")
	err := os.MkdirAll(subDir, 0700)
	require.NoError(t, err)

	es := &embeddedServer{
		tempDir: subDir,
	}
	es.started.Store(true)

	err = es.shutdown()
	assert.NoError(t, err)
	assert.False(t, es.started.Load())

	// Verify temp dir was cleaned up
	_, statErr := os.Stat(subDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestEmbedded91_ShutdownEmbeddedServer_NilHolder(t *testing.T) {
	defer func() {
		embeddedServerHolder.Store(nil)
	}()

	// Ensure holder is nil
	embeddedServerHolder.Store(nil)

	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

func TestEmbedded91_ShutdownEmbeddedServer_WithServer(t *testing.T) {
	defer func() {
		embeddedServerHolder.Store(nil)
	}()

	tmpDir := t.TempDir()
	es := &embeddedServer{
		tempDir: tmpDir,
	}
	es.started.Store(true)
	embeddedServerHolder.Store(es)

	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
	assert.Nil(t, embeddedServerHolder.Load())
}

func TestEmbedded91_GetFreePortForEmbedded_Success(t *testing.T) {
	port, err := getFreePortForEmbedded()
	assert.NoError(t, err)
	assert.Greater(t, port, 0)
	assert.Less(t, port, 65536)
}

// =============================================================================
// Rebalance commands - RunE closure coverage through SDK client error
// =============================================================================

func TestRebalance91_StartCommand_SDKClientError(t *testing.T) {
	origAddress := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddress
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	// Set address to empty to trigger ErrEmptyAddress in createSDKClient
	address = ""
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newRebalanceStartCommand()
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

func TestRebalance91_StatusCommand_SDKClientError(t *testing.T) {
	origAddress := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddress
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	address = ""
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newRebalanceStatusCommand()
	// Set the required flag
	cmd.Flags().Set("operation", "op-123")
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

func TestRebalance91_PlanCommand_SDKClientError(t *testing.T) {
	origAddress := address
	origTransport := transport
	origTimeout := globalTimeout
	defer func() {
		address = origAddress
		transport = origTransport
		globalTimeout = origTimeout
		resetSDKClient()
	}()

	address = ""
	transport = "grpc"
	globalTimeout = 1 * time.Second

	cmd := newRebalancePlanCommand()
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Rebalance display helpers - additional output format paths
// =============================================================================

func TestRebalance91_DisplayRebalanceReport_JSON(t *testing.T) {
	report := &RebalanceReport{
		StartTime:       time.Now().Add(-1 * time.Minute),
		EndTime:         time.Now(),
		Duration:        1 * time.Minute,
		MovesCompleted:  3,
		MovesFailed:     0,
		KeysRebalanced:  1500,
		BytesRebalanced: 1024 * 1024,
		ImbalanceBefore: 0.15,
		ImbalanceAfter:  0.02,
	}

	err := displayRebalanceReport(report, "json")
	assert.NoError(t, err)
}

func TestRebalance91_DisplayProgressSDK_JSON(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{
		OperationID:     "op-123",
		Status:          "in_progress",
		PercentComplete: 55.5,
		ShardsMoved:     3,
		TotalShards:     6,
		ElapsedTime:     int64(30 * time.Second),
	}

	err := displayRebalanceProgressSDK(resp, "json")
	assert.NoError(t, err)
}

func TestRebalance91_DisplayProgressSDK_Table(t *testing.T) {
	resp := &cluster.GetRebalanceProgressResponse{
		OperationID:     "op-456",
		Status:          "completed",
		PercentComplete: 100.0,
		ShardsMoved:     5,
		TotalShards:     5,
		ElapsedTime:     int64(2 * time.Minute),
	}

	err := displayRebalanceProgressSDK(resp, "table")
	assert.NoError(t, err)
}
