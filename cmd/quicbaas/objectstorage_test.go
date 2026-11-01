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

	"github.com/spf13/cobra"
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
)

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

// TestGRPCBackupClientWrapper_NilAdapter checks that every wrapper method
// delegates to its adapter: with no adapter there is nothing to call.
func TestGRPCBackupClientWrapper_NilAdapter(t *testing.T) {
	wrapper := &GRPCBackupClientWrapper{adapter: nil}
	ctx := context.Background()
	tests := []struct {
		name string
		call func()
	}{
		{"create", func() { wrapper.CreateBackup(ctx, &BackupCreateRequest{}) }},
		{"list", func() { wrapper.ListBackups(ctx, &BackupListRequest{}) }},
		{"restore", func() { wrapper.RestoreBackup(ctx, &BackupRestoreRequest{}) }},
		{"delete", func() { wrapper.DeleteBackup(ctx, &BackupDeleteRequest{}) }},
		{"verify", func() { wrapper.VerifyBackup(ctx, &BackupVerifyRequest{}) }},
		{"close", func() { wrapper.Close() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, tt.call)
		})
	}
}

func TestLoadStoredAuthToken(t *testing.T) {
	origTokenFile := authTokenFile
	t.Cleanup(func() { authTokenFile = origTokenFile })

	saveToken := func(t *testing.T, access string, expiresAt time.Time) string {
		t.Helper()
		tokenPath := filepath.Join(t.TempDir(), "token")
		require.NoError(t, cli.SaveToken(tokenPath, &cli.TokenData{
			AccessToken:  access,
			RefreshToken: "refresh",
			ExpiresAt:    expiresAt,
			TokenType:    "Bearer",
		}))
		return tokenPath
	}

	tests := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{"missing file", func(*testing.T) string { return "/nonexistent/path/token" }, ""},
		{"valid token", func(t *testing.T) string {
			return saveToken(t, "test-access-token-123", time.Now().Add(time.Hour))
		}, "test-access-token-123"},
		{"expired token", func(t *testing.T) string {
			return saveToken(t, "expired-token", time.Now().Add(-time.Hour))
		}, ""},
		{"default path fallback", func(*testing.T) string { return "" }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authTokenFile = tt.path(t)
			assert.Equal(t, tt.want, loadStoredAuthToken())
		})
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

func TestDisplayRebalanceReport_Table(t *testing.T) {
	manyErrors := make([]string, 8)
	for i := range manyErrors {
		manyErrors[i] = errors.New(errors.ErrInternal, "test", "error").Error()
	}
	tests := []struct {
		name   string
		report *RebalanceReport
	}{
		{"no moves", &RebalanceReport{Duration: time.Minute, ImbalanceBefore: 0.01, ImbalanceAfter: 0.01}},
		{"with failed moves", &RebalanceReport{
			Duration: 3 * time.Minute, MovesCompleted: 8, MovesFailed: 2,
			KeysRebalanced: 4000, BytesRebalanced: 1024 * 1024,
			ImbalanceBefore: 0.20, ImbalanceAfter: 0.10,
			Errors: []string{errors.New(errors.ErrInternal, "test", "shard move failed").Error()},
		}},
		{"many errors", &RebalanceReport{
			Duration: 2 * time.Minute, MovesCompleted: 5, MovesFailed: 8,
			ImbalanceBefore: 0.30, ImbalanceAfter: 0.25, Errors: manyErrors,
		}},
		{"successful moves", &RebalanceReport{
			Duration: time.Minute, MovesCompleted: 5,
			KeysRebalanced: 2500, BytesRebalanced: 1024 * 500,
			ImbalanceBefore: 0.15, ImbalanceAfter: 0.02,
		}},
		{"zero improvement", &RebalanceReport{Duration: time.Minute, MovesCompleted: 3, ImbalanceBefore: 0.05, ImbalanceAfter: 0.05}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.report.EndTime = time.Now()
			tt.report.StartTime = tt.report.EndTime.Add(-tt.report.Duration)
			assert.NoError(t, displayRebalanceReport(tt.report, "table"))
		})
	}
}

func TestDisplayRebalanceReport_InvalidFormat(t *testing.T) {
	assert.Error(t, displayRebalanceReport(&RebalanceReport{}, "yaml"))
}

func TestDisplayRebalanceProgressSDK_InvalidFormat(t *testing.T) {
	assert.Error(t, displayRebalanceProgressSDK(&cluster.GetRebalanceProgressResponse{}, "xml"))
}

func TestCreateObjectStorageClient_Transports(t *testing.T) {
	origTransport := transport
	t.Cleanup(func() { transport = origTransport })

	tests := []struct {
		name            string
		transport       string
		wantUnsupported bool
	}{
		{name: "grpc without server", transport: "grpc"},
		{name: "empty defaults to grpc", transport: ""},
		{name: "unsupported transport", transport: "rest", wantUnsupported: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport = tt.transport
			_, err := createObjectStorageClient()
			require.Error(t, err)
			if tt.wantUnsupported {
				var unsupportedErr *UnsupportedTransportError
				assert.ErrorAs(t, err, &unsupportedErr)
			}
		})
	}
}

// TestRunObjectCommands_InvalidInput checks that malformed flag values are
// rejected before any client is created.
func TestRunObjectCommands_InvalidInput(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	t.Cleanup(restore)

	tests := []struct {
		name    string
		run     func(*cobra.Command, []string) error
		newCmd  func() *cobra.Command
		args    []string
		setup   func()
		wantMsg string
	}{
		{"list-by-tag without separator", runObjectListByTagCommand, newObjectListByTagCommand, []string{"bucket"},
			func() { objectTagFilter = "invalidtag" }, "invalid tag format"},
		{"metadata update malformed JSON", runObjectMetadataUpdateCommand, newObjectMetadataUpdateCommand, []string{"bucket/key"},
			func() { objectMetadata = `{not valid json}` }, "invalid policy JSON"},
		{"metadata update empty JSON", runObjectMetadataUpdateCommand, newObjectMetadataUpdateCommand, []string{"bucket/key"},
			func() { objectMetadata = "" }, "invalid policy JSON"},
		{"lifecycle set malformed JSON", runObjectLifecycleSetCommand, newObjectLifecycleSetCommand, []string{"mybucket"},
			func() { objectPolicyJSON = "bad-json{" }, "invalid policy JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectStorageClientFactory = func() (*grpc.Client, error) {
				t.Fatal("client factory must not be called for invalid input")
				return nil, nil
			}
			cmd := tt.newCmd()
			tt.setup()
			err := tt.run(cmd, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
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

// objectFormatCase is one row of a formatter table: the output format, the
// response to render, and whether rendering must fail.
type objectFormatCase[R any] struct {
	name        string
	format      string
	includeMeta bool
	resp        R
	wantErr     bool
}

// runObjectFormatCases renders each row with the given formatter under the
// row's output format, restoring the package globals afterwards.
func runObjectFormatCases[R any](t *testing.T, format func(R) error, cases []objectFormatCase[R]) {
	t.Helper()
	origFormat := objectOutputFormat
	origMeta := objectIncludeMeta
	t.Cleanup(func() {
		objectOutputFormat = origFormat
		objectIncludeMeta = origMeta
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objectOutputFormat = tc.format
			objectIncludeMeta = tc.includeMeta
			err := format(tc.resp)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestFormatObjectPutResponse_Formats(t *testing.T) {
	now := time.Now()
	runObjectFormatCases(t, formatObjectPutResponse, []objectFormatCase[*pb.PutObjectResponse]{
		{name: "default", resp: &pb.PutObjectResponse{Key: "bucket/test-key", Size: 1024, Version: 1, Checksum: []byte{0xaa, 0xbb, 0xcc}}},
		{name: "default with timestamp and replicas", resp: &pb.PutObjectResponse{
			Key: "bucket/test-key", Size: 2048, Version: 3, Checksum: []byte{0xde, 0xad},
			CreatedAt: timestamppb.New(now), ReplicatedTo: []string{"store-a", "store-b"},
		}},
		{name: "json", format: "json", resp: &pb.PutObjectResponse{Key: "bucket/json-key", Size: 512, Version: 1}},
		{name: "yaml", format: "yaml", resp: &pb.PutObjectResponse{Key: "bucket/yaml-key", Size: 256, Version: 1}},
		{name: "invalid format", format: "invalid-fmt", resp: &pb.PutObjectResponse{Key: "bucket/key"}, wantErr: true},
	})
}

func TestFormatObjectGetResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectGetResponse, []objectFormatCase[*pb.GetObjectResponse]{
		{name: "default", resp: &pb.GetObjectResponse{Key: "bucket/get-key", Found: true, Value: []byte("hello world"), Size: 11}},
		{name: "json with meta", format: "json", includeMeta: true, resp: &pb.GetObjectResponse{
			Key: "bucket/json-get", Found: true, Value: []byte("data"), Size: 4, Version: 2,
			ContentType: "text/plain", Checksum: []byte{0x01, 0x02},
			Tags: map[string]string{"env": "prod"}, Metadata: &pb.ObjectMetadata{Owner: "admin"},
		}},
		{name: "yaml", format: "yaml", resp: &pb.GetObjectResponse{Key: "bucket/yaml-get", Found: true, Tags: map[string]string{"tier": "hot"}}},
		{name: "raw", format: "raw", resp: &pb.GetObjectResponse{Key: "bucket/raw-get", Found: true, Value: []byte("raw binary content")}},
	})
}

func TestFormatObjectDeleteResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectDeleteResponse, []objectFormatCase[*pb.DeleteObjectResponse]{
		{name: "default deleted", resp: &pb.DeleteObjectResponse{Key: "bucket/del-key", Deleted: true, DeletedAt: timestamppb.New(time.Now())}},
		{name: "default permanent", resp: &pb.DeleteObjectResponse{Key: "bucket/del-perm", Deleted: true, Permanent: true}},
		{name: "default not found", resp: &pb.DeleteObjectResponse{Key: "bucket/not-found"}},
		{name: "json", format: "json", resp: &pb.DeleteObjectResponse{Key: "bucket/del-json", Deleted: true}},
		{name: "yaml", format: "yaml", resp: &pb.DeleteObjectResponse{Key: "bucket/del-yaml", Deleted: true}},
	})
}

func TestFormatObjectHeadResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectHeadResponse, []objectFormatCase[*pb.HeadObjectResponse]{
		{name: "default exists", resp: &pb.HeadObjectResponse{
			Key: "bucket/head-key", Exists: true, Size: 4096, ContentType: "application/json",
			Version: 5, Checksum: []byte{0xff}, Status: pb.ObjectStatus_OBJECT_STATUS_ACTIVE,
			Tier: pb.StorageTier_STORAGE_TIER_HOT, ModifiedAt: timestamppb.New(time.Now()),
			Tags: map[string]string{"env": "staging"},
		}},
		{name: "default not found", resp: &pb.HeadObjectResponse{Key: "bucket/missing"}},
		{name: "json", format: "json", resp: &pb.HeadObjectResponse{Key: "bucket/head-json", Exists: true, Size: 100}},
		{name: "yaml", format: "yaml", resp: &pb.HeadObjectResponse{Key: "bucket/head-yaml", Exists: true}},
	})
}

func TestFormatObjectListResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectListResponse, []objectFormatCase[*pb.ListObjectsResponse]{
		{name: "default with cursor", resp: &pb.ListObjectsResponse{
			Objects: []*pb.ObjectInfo{
				{Key: "bucket/obj1", Size: 100, Version: 1, ModifiedAt: timestamppb.New(time.Now())},
				{Key: "bucket/obj2", Size: 200, Version: 2},
			},
			Total: 2, NextCursor: "cursor-abc", HasMore: true,
		}},
		{name: "json", format: "json", resp: &pb.ListObjectsResponse{}},
		{name: "yaml", format: "yaml", resp: &pb.ListObjectsResponse{Total: 1, Objects: []*pb.ObjectInfo{{Key: "k"}}}},
	})
}

func TestFormatObjectListByTagResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectListByTagResponse, []objectFormatCase[*pb.ListByTagResponse]{
		{name: "default with cursor", resp: &pb.ListByTagResponse{Keys: []string{"key1", "key2"}, Total: 2, NextCursor: "next", HasMore: true}},
		{name: "json", format: "json", resp: &pb.ListByTagResponse{Keys: []string{"k"}, Total: 1}},
		{name: "yaml", format: "yaml", resp: &pb.ListByTagResponse{Keys: []string{"k"}, Total: 1}},
	})
}

func TestFormatObjectListByTenantResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectListByTenantResponse, []objectFormatCase[*pb.ListByTenantResponse]{
		{name: "default with objects", resp: &pb.ListByTenantResponse{
			Objects: []*pb.ObjectInfo{{Key: "tenant/obj1", Size: 100, Version: 1}},
			Total:   1, NextCursor: "c2", HasMore: true,
		}},
		{name: "default with keys", resp: &pb.ListByTenantResponse{Keys: []string{"key-a", "key-b"}, Total: 2}},
		{name: "json", format: "json", resp: &pb.ListByTenantResponse{}},
		{name: "yaml", format: "yaml", resp: &pb.ListByTenantResponse{}},
	})
}

func TestFormatObjectCopyResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectCopyResponse, []objectFormatCase[*pb.CopyObjectResponse]{
		{name: "default", resp: &pb.CopyObjectResponse{Key: "bucket/dest-key", Version: 2, Size: 512, CreatedAt: timestamppb.New(time.Now())}},
		{name: "json", format: "json", resp: &pb.CopyObjectResponse{Key: "bucket/copy-json"}},
	})
}

func TestFormatObjectArchiveResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectArchiveResponse, []objectFormatCase[*pb.ArchiveObjectResponse]{
		{name: "default success", resp: &pb.ArchiveObjectResponse{
			Key: "bucket/archive-key", Success: true, ArchiveStore: "cold-store",
			OriginalSize: 2048, ArchivedSize: 1024, ArchivedAt: timestamppb.New(time.Now()),
		}},
		{name: "default failed", resp: &pb.ArchiveObjectResponse{Key: "bucket/archive-fail"}},
		{name: "json", format: "json", resp: &pb.ArchiveObjectResponse{Key: "bucket/archive-json", Success: true}},
	})
}

func TestFormatObjectRestoreResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectRestoreResponse, []objectFormatCase[*pb.RestoreObjectResponse]{
		{name: "default success", resp: &pb.RestoreObjectResponse{
			Key: "bucket/restore-key", Success: true, RestoredTo: "hot-store", OperationId: "op-123",
			EstimatedCompletion: timestamppb.New(time.Now().Add(time.Hour)),
		}},
		{name: "default failed", resp: &pb.RestoreObjectResponse{Key: "bucket/restore-fail"}},
		{name: "json", format: "json", resp: &pb.RestoreObjectResponse{Key: "bucket/restore-json", Success: true}},
	})
}

func TestFormatObjectVerifyResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectVerifyResponse, []objectFormatCase[*pb.VerifyObjectResponse]{
		{name: "default valid with locations", resp: &pb.VerifyObjectResponse{
			Key: "bucket/verify-key", Valid: true,
			Locations: []*pb.LocationVerification{
				{Store: "store-a", Valid: true, Size: 100},
				{Store: "store-b", Valid: false, Error: "checksum mismatch", Size: 100},
			},
		}},
		{name: "default invalid repaired", resp: &pb.VerifyObjectResponse{Key: "bucket/verify-inv", Repaired: true}},
		{name: "json", format: "json", resp: &pb.VerifyObjectResponse{Key: "bucket/verify-json", Valid: true}},
	})
}

func TestFormatObjectReplicateResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectReplicateResponse, []objectFormatCase[*pb.ReplicateObjectResponse]{
		{name: "default success", resp: &pb.ReplicateObjectResponse{
			Key: "bucket/rep-key", Success: true, ReplicatedTo: []string{"store-a", "store-b"}, OperationId: "rep-op-1",
		}},
		{name: "default partial failure", resp: &pb.ReplicateObjectResponse{
			Key: "bucket/rep-fail", ReplicatedTo: []string{"store-a"}, FailedStores: []string{"store-c"},
		}},
		{name: "json", format: "json", resp: &pb.ReplicateObjectResponse{Key: "bucket/rep-json", Success: true}},
	})
}

func TestFormatObjectMigrateResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectMigrateResponse, []objectFormatCase[*pb.MigrateObjectResponse]{
		{name: "default success", resp: &pb.MigrateObjectResponse{
			Key: "bucket/mig-key", Success: true, OldBackend: "cold-store", NewBackend: "hot-store",
			SourceDeleted: true, Verified: true,
		}},
		{name: "default failed", resp: &pb.MigrateObjectResponse{Key: "bucket/mig-fail", OldBackend: "hot", NewBackend: "cold"}},
		{name: "json", format: "json", resp: &pb.MigrateObjectResponse{Key: "bucket/mig-json", Success: true}},
	})
}

func TestFormatObjectPolicyResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectPolicyResponse, []objectFormatCase[*pb.GetObjectPolicyResponse]{
		{name: "default with effective explicit and inherited", resp: &pb.GetObjectPolicyResponse{
			Key: "bucket/pol-key",
			Policy: &pb.ObjectPolicy{
				ReplicationPolicy: &pb.ReplicationPolicy{
					Enabled: true, MinReplicas: 3, Mode: pb.ReplicationMode_REPLICATION_MODE_SYNC,
					TargetStores: []string{"store-a", "store-b"},
				},
				ArchivePolicy: &pb.ArchivePolicy{
					Enabled: true, ArchiveAfterDays: 30, TargetStore: "archive-store", DeleteFromPrimary: true,
				},
				RetentionPolicy: &pb.RetentionPolicy{Enabled: true, RetentionDays: 90, MaxVersions: 5},
			},
			ExplicitPolicy: &pb.ObjectPolicy{
				RetentionPolicy: &pb.RetentionPolicy{Enabled: true, RetentionDays: 30, MaxVersions: 3, LegalHold: true},
			},
			InheritedPolicy: &pb.ObjectPolicy{
				ReplicationPolicy: &pb.ReplicationPolicy{Enabled: true, MinReplicas: 2},
			},
		}},
		{name: "json", format: "json", resp: &pb.GetObjectPolicyResponse{Key: "bucket/pol-json"}},
		{name: "yaml", format: "yaml", resp: &pb.GetObjectPolicyResponse{Key: "bucket/pol-yaml"}},
	})
}

func TestFormatObjectSetPolicyResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectSetPolicyResponse, []objectFormatCase[*pb.SetObjectPolicyResponse]{
		{name: "default", resp: &pb.SetObjectPolicyResponse{Key: "bucket/setpol-key", Version: 2, UpdatedAt: timestamppb.New(time.Now())}},
		{name: "json", format: "json", resp: &pb.SetObjectPolicyResponse{Key: "bucket/setpol-json", Version: 1}},
	})
}

func TestFormatObjectStatsResponse_Formats(t *testing.T) {
	runObjectFormatCases(t, formatObjectStatsResponse, []objectFormatCase[*pb.ListBackendsResponse]{
		{name: "default with healthy and unhealthy backends", resp: &pb.ListBackendsResponse{
			Total: 2,
			Backends: []*pb.BackendInfo{
				{
					Name: "primary-store", Type: pb.BackendType_BACKEND_TYPE_LOCAL, Tier: pb.StorageTier_STORAGE_TIER_HOT,
					Enabled: true, Healthy: true, UsedBytes: 1073741824, CapacityBytes: 10737418240,
				},
				{
					Name: "archive-store", Type: pb.BackendType_BACKEND_TYPE_S3, Tier: pb.StorageTier_STORAGE_TIER_ARCHIVE,
					Enabled: true, UsedBytes: 536870912, CapacityBytes: 1099511627776,
				},
			},
		}},
		{name: "json", format: "json", resp: &pb.ListBackendsResponse{}},
		{name: "yaml", format: "yaml", resp: &pb.ListBackendsResponse{}},
	})
}

func TestFormatObjectLifecycleResponse_Formats(t *testing.T) {
	formatBucket := func(resp *pb.GetObjectPolicyResponse) error {
		return formatObjectLifecycleResponse("bucket", resp)
	}
	runObjectFormatCases(t, formatBucket, []objectFormatCase[*pb.GetObjectPolicyResponse]{
		{name: "default with retention", resp: &pb.GetObjectPolicyResponse{
			Key:    "bucket/*",
			Policy: &pb.ObjectPolicy{RetentionPolicy: &pb.RetentionPolicy{Enabled: true, RetentionDays: 365}},
		}},
		{name: "default no policy", resp: &pb.GetObjectPolicyResponse{Key: "bucket/*"}},
		{name: "json", format: "json", resp: &pb.GetObjectPolicyResponse{Key: "bucket/*"}},
		{name: "yaml", format: "yaml", resp: &pb.GetObjectPolicyResponse{Key: "bucket/*"}},
	})
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

func TestRunObjectPutCommand_InvalidInput(t *testing.T) {
	origFile := objectFile
	origMeta := objectMetadata
	origTags := objectTags
	t.Cleanup(func() {
		objectFile = origFile
		objectMetadata = origMeta
		objectTags = origTags
	})

	tests := []struct {
		name     string
		file     string
		metadata string
	}{
		{name: "no data", file: ""},
		{name: "unreadable file", file: "/nonexistent/path/to/file.dat"},
		{name: "invalid metadata JSON", file: "/dev/null", metadata: "not-valid-json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectFile = tt.file
			objectMetadata = tt.metadata
			objectTags = nil
			err := runObjectPutCommand(newObjectPutCommand(), []string{"bucket/key"})
			assert.Error(t, err)
		})
	}
}

// TestRunObjectCommands_FactoryError checks that every object verb returns
// the client factory's error instead of proceeding without a client.
func TestRunObjectCommands_FactoryError(t *testing.T) {
	origFactory := objectStorageClientFactory
	origFile := objectFile
	origTagFilter := objectTagFilter
	origMeta := objectMetadata
	origPolicy := objectPolicyJSON
	t.Cleanup(func() {
		objectStorageClientFactory = origFactory
		objectFile = origFile
		objectTagFilter = origTagFilter
		objectMetadata = origMeta
		objectPolicyJSON = origPolicy
	})

	factoryErr := &errors.QBError{Code: errors.ErrUnavailable, Op: "test", Err: errors.New(errors.ErrUnavailable, "test", "connection refused")}
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, factoryErr
	}

	dataFile := filepath.Join(t.TempDir(), "data.bin")
	require.NoError(t, os.WriteFile(dataFile, []byte("payload"), 0o600))

	tests := []struct {
		name   string
		run    func(*cobra.Command, []string) error
		newCmd func() *cobra.Command
		args   []string
	}{
		{"put", runObjectPutCommand, newObjectPutCommand, []string{"bucket/key"}},
		{"get", runObjectGetCommand, newObjectGetCommand, []string{"bucket/key"}},
		{"delete", runObjectDeleteCommand, newObjectDeleteCommand, []string{"bucket/key"}},
		{"head", runObjectHeadCommand, newObjectHeadCommand, []string{"bucket/key"}},
		{"list", runObjectListCommand, newObjectListCommand, []string{"test-namespace"}},
		{"list-by-tag", runObjectListByTagCommand, newObjectListByTagCommand, []string{"bucket"}},
		{"list-by-tenant", runObjectListByTenantCommand, newObjectListByTenantCommand, []string{}},
		{"copy", runObjectCopyCommand, newObjectCopyCommand, []string{"src/key", "dst/key"}},
		{"archive", runObjectArchiveCommand, newObjectArchiveCommand, []string{"bucket/key"}},
		{"restore", runObjectRestoreCommand, newObjectRestoreCommand, []string{"bucket/key"}},
		{"verify", runObjectVerifyCommand, newObjectVerifyCommand, []string{"bucket/key"}},
		{"replicate", runObjectReplicateCommand, newObjectReplicateCommand, []string{"bucket/key"}},
		{"migrate", runObjectMigrateCommand, newObjectMigrateCommand, []string{"bucket/key"}},
		{"metadata get", runObjectMetadataGetCommand, newObjectMetadataGetCommand, []string{"bucket/key"}},
		{"metadata update", runObjectMetadataUpdateCommand, newObjectMetadataUpdateCommand, []string{"bucket/key"}},
		{"stats", runObjectStatsCommand, newObjectStatsCommand, []string{}},
		{"lifecycle set", runObjectLifecycleSetCommand, newObjectLifecycleSetCommand, []string{"bucket"}},
		{"lifecycle get", runObjectLifecycleGetCommand, newObjectLifecycleGetCommand, []string{"bucket"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Building the command binds its flags to their defaults, so
			// valid inputs are set afterwards to reach the client factory.
			cmd := tt.newCmd()
			objectFile = dataFile
			objectTagFilter = "env=prod"
			objectMetadata = "{}"
			objectPolicyJSON = "{}"
			err := tt.run(cmd, tt.args)
			assert.ErrorIs(t, err, factoryErr)
		})
	}
}

// =============================================================================
// New Command Builder Tests
// =============================================================================

func TestNewObjectStorageCommands(t *testing.T) {
	cmd := newObjectStorageCommands()
	require.NotNil(t, cmd)
	assert.Equal(t, "object", cmd.Use)
	assert.NotEmpty(t, cmd.Commands(), "should have subcommands")
}

func TestNewObjectCommands_Use(t *testing.T) {
	tests := []struct {
		verb   string
		newCmd func() *cobra.Command
	}{
		{"put", newObjectPutCommand},
		{"get", newObjectGetCommand},
		{"delete", newObjectDeleteCommand},
		{"head", newObjectHeadCommand},
		{"list", newObjectListCommand},
		{"copy", newObjectCopyCommand},
		{"archive", newObjectArchiveCommand},
		{"restore", newObjectRestoreCommand},
		{"verify", newObjectVerifyCommand},
		{"replicate", newObjectReplicateCommand},
		{"migrate", newObjectMigrateCommand},
		{"stats", newObjectStatsCommand},
	}
	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			assert.Contains(t, tt.newCmd().Use, tt.verb)
		})
	}
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
		grpc.WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13}),
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
	origTimeout := globalTimeout
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
		globalTimeout = origTimeout
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

// TestRunObjectCommands_RPCFailure drives every object verb, with its flags
// set, against a client whose server is unreachable, so the request is built
// and the RPC error is returned through formatError.
func TestRunObjectCommands_RPCFailure(t *testing.T) {
	restore := saveAndRestoreObjectGlobals(t)
	t.Cleanup(restore)

	dataFile := filepath.Join(t.TempDir(), "testdata.txt")
	require.NoError(t, os.WriteFile(dataFile, []byte("test content"), 0o600))

	tests := []struct {
		name   string
		run    func(*cobra.Command, []string) error
		newCmd func() *cobra.Command
		args   []string
		setup  func()
	}{
		{"put with tags", runObjectPutCommand, newObjectPutCommand, []string{"bucket/key"}, func() {
			objectFile = dataFile
			objectContentType = "text/plain"
			globalTenantID = "tenant-1"
			objectTags = map[string]string{"env": "test"}
		}},
		{"put metadata ignored when tags set", runObjectPutCommand, newObjectPutCommand, []string{"bucket/key"}, func() {
			objectFile = dataFile
			objectMetadata = `{"key1":"val1"}`
			objectTags = map[string]string{"existing": "tag"}
		}},
		{"put metadata without tags", runObjectPutCommand, newObjectPutCommand, []string{"bucket/key"}, func() {
			objectFile = dataFile
			objectMetadata = `{"key1":"val1"}`
			objectTags = nil
		}},
		{"get with meta", runObjectGetCommand, newObjectGetCommand, []string{"bucket/key"}, func() {
			globalTenantID = "tenant-1"
			objectIncludeMeta = true
		}},
		{"delete", runObjectDeleteCommand, newObjectDeleteCommand, []string{"bucket/key"}, func() {
			globalTenantID = "tenant-1"
			objectDeleteSource = true
		}},
		{"head", runObjectHeadCommand, newObjectHeadCommand, []string{"bucket/key"}, func() {
			globalTenantID = "tenant-1"
		}},
		{"list", runObjectListCommand, newObjectListCommand, []string{"mybucket"}, func() {
			objectPrefix = "images/"
			globalTenantID = "tenant-1"
			objectLimit = 50
			objectCursor = "cursor-abc"
			objectIncludeMeta = true
		}},
		{"list-by-tag", runObjectListByTagCommand, newObjectListByTagCommand, []string{}, func() {
			objectTagFilter = "type=document"
			globalTenantID = "tenant-1"
			objectLimit = 50
		}},
		{"list-by-tenant", runObjectListByTenantCommand, newObjectListByTenantCommand, []string{}, func() {
			globalTenantID = "tenant-123"
			objectLimit = 100
			objectIncludeMeta = true
		}},
		{"copy with tags", runObjectCopyCommand, newObjectCopyCommand, []string{"src/key", "dst/key"}, func() {
			objectCopyTags = true
			globalTenantID = "tenant-1"
		}},
		{"archive", runObjectArchiveCommand, newObjectArchiveCommand, []string{"bucket/key"}, func() {
			objectTargetStore = "cold-store"
			objectDeleteFromPrimary = true
			objectCompressionLevel = 6
		}},
		{"restore hot tier", runObjectRestoreCommand, newObjectRestoreCommand, []string{"bucket/key"}, func() {
			objectDestBackend = "hot-backend"
			objectRestoreDays = 14
			objectRestoreTier = "hot"
		}},
		{"restore warm tier", runObjectRestoreCommand, newObjectRestoreCommand, []string{"bucket/key"}, func() {
			objectRestoreTier = "warm"
		}},
		{"restore default tier", runObjectRestoreCommand, newObjectRestoreCommand, []string{"bucket/key"}, func() {
			objectRestoreTier = ""
		}},
		{"verify with repair", runObjectVerifyCommand, newObjectVerifyCommand, []string{"bucket/key"}, func() {
			objectCheckAll = true
			objectRepair = true
		}},
		{"replicate to listed stores", runObjectReplicateCommand, newObjectReplicateCommand, []string{"bucket/key"}, func() {
			objectDestBackend = "store-a, store-b"
			objectSync = true
		}},
		{"migrate", runObjectMigrateCommand, newObjectMigrateCommand, []string{"bucket/key"}, func() {
			objectDestBackend = "archive-backend"
			objectDeleteSource = true
		}},
		{"metadata get", runObjectMetadataGetCommand, newObjectMetadataGetCommand, []string{"bucket/key"}, func() {}},
		{"metadata update", runObjectMetadataUpdateCommand, newObjectMetadataUpdateCommand, []string{"bucket/key"}, func() {
			objectMetadata = `{"replication":{"enabled":true,"min_replicas":3}}`
		}},
		{"stats", runObjectStatsCommand, newObjectStatsCommand, []string{}, func() {}},
		{"lifecycle set", runObjectLifecycleSetCommand, newObjectLifecycleSetCommand, []string{"mybucket"}, func() {
			objectPolicyJSON = `{"archive":{"enabled":true,"archive_after_days":30}}`
		}},
		{"lifecycle get", runObjectLifecycleGetCommand, newObjectLifecycleGetCommand, []string{"mybucket"}, func() {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newDeadGRPCClient(t)
			objectStorageClientFactory = func() (*grpc.Client, error) {
				return client, nil
			}
			// Building the command resets the flag globals, so the row's
			// inputs are set afterwards.
			cmd := tt.newCmd()
			tt.setup()
			globalTimeout = 500 * time.Millisecond

			err := tt.run(cmd, tt.args)
			assert.Error(t, err, "RPC call to dead server should fail")
		})
	}
}

// =============================================================================
// createObjectStorageGRPCClient - TLS required (insecure path removed)
// =============================================================================

func TestObjectStorage91_CreateGRPCClient_TLSRequired(t *testing.T) {
	origTLSEnabled := objectTLSEnabled
	origAddress := address
	origTimeout := globalTimeout
	origMaxRetries := objectMaxRetries
	origRetryBackoff := objectRetryBackoff
	defer func() {
		objectTLSEnabled = origTLSEnabled
		address = origAddress
		globalTimeout = origTimeout
		objectMaxRetries = origMaxRetries
		objectRetryBackoff = origRetryBackoff
	}()

	objectTLSEnabled = false
	address = "127.0.0.1:1"
	globalTimeout = 100 * time.Millisecond
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
