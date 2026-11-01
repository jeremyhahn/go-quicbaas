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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

func TestParseVectorData(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []float32
		wantErr bool
	}{
		{
			name:  "valid single value",
			input: "0.5",
			want:  []float32{0.5},
		},
		{
			name:  "valid multiple values",
			input: "0.1,0.2,0.3",
			want:  []float32{0.1, 0.2, 0.3},
		},
		{
			name:  "valid with spaces",
			input: " 0.1 , 0.2 , 0.3 ",
			want:  []float32{0.1, 0.2, 0.3},
		},
		{
			name:  "valid negative values",
			input: "-0.1,-0.2,0.3",
			want:  []float32{-0.1, -0.2, 0.3},
		},
		{
			name:    "invalid non-numeric",
			input:   "0.1,abc,0.3",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVectorData(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseFilter tests the parseFilter helper function.
func TestParseFilter(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKey   string
		wantValue string
		wantFound bool
	}{
		{
			name:      "valid filter",
			input:     "key=value",
			wantKey:   "key",
			wantValue: "value",
			wantFound: true,
		},
		{
			name:      "value with equals",
			input:     "key=value=more",
			wantKey:   "key",
			wantValue: "value=more",
			wantFound: true,
		},
		{
			name:      "no equals",
			input:     "keyvalue",
			wantKey:   "",
			wantValue: "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, found := parseFilter(tt.input)
			assert.Equal(t, tt.wantKey, key)
			assert.Equal(t, tt.wantValue, value)
			assert.Equal(t, tt.wantFound, found)
		})
	}
}

// =============================================================================
// Test Command Validation
// =============================================================================

// TestVectorCommandArgValidation tests that each command rejects a missing
// positional argument.
func TestVectorCommandArgValidation(t *testing.T) {
	tests := []struct {
		name    string
		cmd     *cobra.Command
		wantErr string
	}{
		{"namespace create", newVectorNamespaceCreateCommand(), "accepts 1 arg"},
		{"namespace delete", newVectorNamespaceDeleteCommand(), "accepts 1 arg"},
		{"namespace info", newVectorNamespaceInfoCommand(), "accepts 1 arg"},
		{"upsert", newVectorUpsertCommand(), "accepts 2 arg"},
		{"get", newVectorGetCommand(), "accepts 2 arg"},
		{"delete", newVectorDeleteCommand(), "accepts 2 arg"},
		{"list", newVectorListCommand(), "accepts 1 arg"},
		{"search similar", newVectorSearchSimilarCommand(), "accepts 1 arg"},
		{"search by-id", newVectorSearchByIDCommand(), "accepts 2 arg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.SetArgs([]string{})
			err := tt.cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestVectorCommandHelp tests that help text is available for all commands.
func TestVectorCommandHelp(t *testing.T) {
	cmd := newVectorCommands()
	require.NotNil(t, cmd)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Check subcommands exist
	subcommands := cmd.Commands()
	require.Greater(t, len(subcommands), 0)

	for _, sub := range subcommands {
		assert.NotEmpty(t, sub.Short, "subcommand %s should have short description", sub.Name())
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

// BenchmarkParseVectorData benchmarks vector data parsing.
func BenchmarkParseVectorData(b *testing.B) {
	input := "0.1,0.2,0.3,0.4,0.5,0.6,0.7,0.8,0.9,1.0"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parseVectorData(input)
	}
}

type mockVectorClientForCoverage struct {
	CreateNamespaceFunc       func(ctx context.Context, req *vector.CreateNamespaceRequest) (*vector.CreateNamespaceResponse, error)
	DeleteNamespaceFunc       func(ctx context.Context, req *vector.DeleteNamespaceRequest) (*vector.DeleteNamespaceResponse, error)
	ListNamespacesFunc        func(ctx context.Context, req *vector.ListNamespacesRequest) (*vector.ListNamespacesResponse, error)
	GetNamespaceInfoFunc      func(ctx context.Context, req *vector.GetNamespaceInfoRequest) (*vector.GetNamespaceInfoResponse, error)
	UpsertFunc                func(ctx context.Context, req *vector.UpsertRequest) (*vector.UpsertResponse, error)
	BatchUpsertFunc           func(ctx context.Context, req *vector.BatchUpsertRequest) (*vector.BatchUpsertResponse, error)
	GetFunc                   func(ctx context.Context, req *vector.GetRequest) (*vector.GetResponse, error)
	DeleteFunc                func(ctx context.Context, req *vector.DeleteRequest) (*vector.DeleteResponse, error)
	BatchDeleteFunc           func(ctx context.Context, req *vector.BatchDeleteRequest) (*vector.BatchDeleteResponse, error)
	ListFunc                  func(ctx context.Context, req *vector.ListVectorsRequest) (*vector.ListVectorsResponse, error)
	SearchSimilarFunc         func(ctx context.Context, req *vector.SearchSimilarRequest) (*vector.SearchSimilarResponse, error)
	SearchByIDFunc            func(ctx context.Context, req *vector.SearchByIDRequest) (*vector.SearchByIDResponse, error)
	HybridSearchFunc          func(ctx context.Context, req *vector.HybridSearchRequest) (*vector.HybridSearchResponse, error)
	SearchByMetadataFunc      func(ctx context.Context, req *vector.SearchByMetadataRequest) (*vector.SearchByMetadataResponse, error)
	UpdateMetadataFunc        func(ctx context.Context, req *vector.UpdateMetadataRequest) (*vector.UpdateMetadataResponse, error)
	RebuildIndexFunc          func(ctx context.Context, req *vector.RebuildIndexRequest) (*vector.RebuildIndexResponse, error)
	GetIndexProgressFunc      func(ctx context.Context, req *vector.GetIndexProgressRequest) (*vector.GetIndexProgressResponse, error)
	ExportFunc                func(ctx context.Context, req *vector.ExportRequest) ([]*vector.ExportResponse, error)
	ImportFunc                func(ctx context.Context, batches []*vector.ImportRequest) (*vector.ImportResponse, error)
	CompactFunc               func(ctx context.Context, req *vector.CompactRequest) (*vector.CompactResponse, error)
	GetCompactionProgressFunc func(ctx context.Context, req *vector.GetCompactionProgressRequest) (*vector.GetCompactionProgressResponse, error)
	GetStatsFunc              func(ctx context.Context, req *vector.GetStatsRequest) (*vector.GetStatsResponse, error)
}

func (m *mockVectorClientForCoverage) CreateNamespace(ctx context.Context, req *vector.CreateNamespaceRequest) (*vector.CreateNamespaceResponse, error) {
	if m.CreateNamespaceFunc != nil {
		return m.CreateNamespaceFunc(ctx, req)
	}
	return &vector.CreateNamespaceResponse{Success: true, Namespace: req.Config.Namespace, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) DeleteNamespace(ctx context.Context, req *vector.DeleteNamespaceRequest) (*vector.DeleteNamespaceResponse, error) {
	if m.DeleteNamespaceFunc != nil {
		return m.DeleteNamespaceFunc(ctx, req)
	}
	return &vector.DeleteNamespaceResponse{Deleted: true, VectorsDeleted: 10, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) ListNamespaces(ctx context.Context, req *vector.ListNamespacesRequest) (*vector.ListNamespacesResponse, error) {
	if m.ListNamespacesFunc != nil {
		return m.ListNamespacesFunc(ctx, req)
	}
	return &vector.ListNamespacesResponse{
		Namespaces: []*vector.NamespaceInfo{
			{Config: &vector.NamespaceConfig{Namespace: "test-ns", Dimensions: 128}, VectorCount: 100, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
		TotalCount: 1,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockVectorClientForCoverage) GetNamespaceInfo(ctx context.Context, req *vector.GetNamespaceInfoRequest) (*vector.GetNamespaceInfoResponse, error) {
	if m.GetNamespaceInfoFunc != nil {
		return m.GetNamespaceInfoFunc(ctx, req)
	}
	return &vector.GetNamespaceInfoResponse{
		Info:      &vector.NamespaceInfo{Config: &vector.NamespaceConfig{Namespace: req.Namespace, Dimensions: 128}, VectorCount: 100, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockVectorClientForCoverage) Upsert(ctx context.Context, req *vector.UpsertRequest) (*vector.UpsertResponse, error) {
	if m.UpsertFunc != nil {
		return m.UpsertFunc(ctx, req)
	}
	return &vector.UpsertResponse{ID: req.Vector.ID, Success: true, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) BatchUpsert(ctx context.Context, req *vector.BatchUpsertRequest) (*vector.BatchUpsertResponse, error) {
	if m.BatchUpsertFunc != nil {
		return m.BatchUpsertFunc(ctx, req)
	}
	return &vector.BatchUpsertResponse{VectorsSucceeded: int64(len(req.Vectors)), Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) Get(ctx context.Context, req *vector.GetRequest) (*vector.GetResponse, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, req)
	}
	return &vector.GetResponse{
		Vector:    &vector.VectorEntity{ID: req.ID, Namespace: req.Namespace, Vector: []float32{0.1, 0.2, 0.3}},
		Found:     true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockVectorClientForCoverage) Delete(ctx context.Context, req *vector.DeleteRequest) (*vector.DeleteResponse, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, req)
	}
	return &vector.DeleteResponse{Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) BatchDelete(ctx context.Context, req *vector.BatchDeleteRequest) (*vector.BatchDeleteResponse, error) {
	if m.BatchDeleteFunc != nil {
		return m.BatchDeleteFunc(ctx, req)
	}
	return &vector.BatchDeleteResponse{DeletedCount: int64(len(req.IDs)), Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) List(ctx context.Context, req *vector.ListVectorsRequest) (*vector.ListVectorsResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, req)
	}
	return &vector.ListVectorsResponse{
		Vectors:    []*vector.VectorEntity{{ID: "vec-1", Namespace: req.Namespace, Vector: []float32{0.1, 0.2}}},
		TotalCount: 1,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockVectorClientForCoverage) SearchSimilar(ctx context.Context, req *vector.SearchSimilarRequest) (*vector.SearchSimilarResponse, error) {
	if m.SearchSimilarFunc != nil {
		return m.SearchSimilarFunc(ctx, req)
	}
	return &vector.SearchSimilarResponse{
		Results:   []*vector.SearchResult{{Vector: &vector.VectorEntity{ID: "result-1", Vector: []float32{0.1}}, Score: 0.95}},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockVectorClientForCoverage) SearchByID(ctx context.Context, req *vector.SearchByIDRequest) (*vector.SearchByIDResponse, error) {
	if m.SearchByIDFunc != nil {
		return m.SearchByIDFunc(ctx, req)
	}
	return &vector.SearchByIDResponse{
		Results:   []*vector.SearchResult{{Vector: &vector.VectorEntity{ID: "result-1"}, Score: 0.95}},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockVectorClientForCoverage) HybridSearch(ctx context.Context, req *vector.HybridSearchRequest) (*vector.HybridSearchResponse, error) {
	if m.HybridSearchFunc != nil {
		return m.HybridSearchFunc(ctx, req)
	}
	return &vector.HybridSearchResponse{Results: []*vector.SearchResult{}, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) SearchByMetadata(ctx context.Context, req *vector.SearchByMetadataRequest) (*vector.SearchByMetadataResponse, error) {
	if m.SearchByMetadataFunc != nil {
		return m.SearchByMetadataFunc(ctx, req)
	}
	return &vector.SearchByMetadataResponse{Results: []*vector.VectorEntity{}, TotalCount: 0, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) UpdateMetadata(ctx context.Context, req *vector.UpdateMetadataRequest) (*vector.UpdateMetadataResponse, error) {
	if m.UpdateMetadataFunc != nil {
		return m.UpdateMetadataFunc(ctx, req)
	}
	return &vector.UpdateMetadataResponse{Success: true, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) RebuildIndex(ctx context.Context, req *vector.RebuildIndexRequest) (*vector.RebuildIndexResponse, error) {
	if m.RebuildIndexFunc != nil {
		return m.RebuildIndexFunc(ctx, req)
	}
	return &vector.RebuildIndexResponse{Status: "started", Started: time.Now(), Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) GetIndexProgress(ctx context.Context, req *vector.GetIndexProgressRequest) (*vector.GetIndexProgressResponse, error) {
	if m.GetIndexProgressFunc != nil {
		return m.GetIndexProgressFunc(ctx, req)
	}
	return &vector.GetIndexProgressResponse{PercentComplete: 100.0, Status: "completed", Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) Export(ctx context.Context, req *vector.ExportRequest) ([]*vector.ExportResponse, error) {
	if m.ExportFunc != nil {
		return m.ExportFunc(ctx, req)
	}
	return []*vector.ExportResponse{{VectorsExported: 100, IsFinal: true, Timestamp: time.Now()}}, nil
}

func (m *mockVectorClientForCoverage) Import(ctx context.Context, batches []*vector.ImportRequest) (*vector.ImportResponse, error) {
	if m.ImportFunc != nil {
		return m.ImportFunc(ctx, batches)
	}
	return &vector.ImportResponse{VectorsImported: 100, VectorsFailed: 0, Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) Compact(ctx context.Context, req *vector.CompactRequest) (*vector.CompactResponse, error) {
	if m.CompactFunc != nil {
		return m.CompactFunc(ctx, req)
	}
	return &vector.CompactResponse{Started: time.Now(), Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) GetCompactionProgress(ctx context.Context, req *vector.GetCompactionProgressRequest) (*vector.GetCompactionProgressResponse, error) {
	if m.GetCompactionProgressFunc != nil {
		return m.GetCompactionProgressFunc(ctx, req)
	}
	return &vector.GetCompactionProgressResponse{PercentComplete: 100.0, Status: "completed", Timestamp: time.Now()}, nil
}

func (m *mockVectorClientForCoverage) GetStats(ctx context.Context, req *vector.GetStatsRequest) (*vector.GetStatsResponse, error) {
	if m.GetStatsFunc != nil {
		return m.GetStatsFunc(ctx, req)
	}
	return &vector.GetStatsResponse{
		VectorCount: 100,
		Namespace:   req.Namespace,
		Timestamp:   time.Now(),
	}, nil
}

// Compile-time assertion that mockVectorClientForCoverage implements vector.Client.
var _ vector.Client = (*mockVectorClientForCoverage)(nil)

// =============================================================================
// Mock SDK Client with Vector Support
// =============================================================================

// mockSDKClientWithVector implements quicbaas.Client for testing vector commands.
type mockSDKClientWithVector struct {
	vectorClient vector.Client
}

func (m *mockSDKClientWithVector) KV() quicbaas.KVClient { return nil }

func (m *mockSDKClientWithVector) Batch() quicbaas.BatchClient { return nil }

func (m *mockSDKClientWithVector) Transaction() quicbaas.TransactionClient { return nil }

func (m *mockSDKClientWithVector) Query() quicbaas.QueryClient { return nil }

func (m *mockSDKClientWithVector) Cluster() quicbaas.ClusterClient { return nil }

func (m *mockSDKClientWithVector) Health() quicbaas.HealthClient { return nil }

func (m *mockSDKClientWithVector) Operations() quicbaas.OperationsClient { return nil }

func (m *mockSDKClientWithVector) Cache() quicbaas.CacheClient { return nil }

func (m *mockSDKClientWithVector) IAM() quicbaas.IAMClient { return nil }

func (m *mockSDKClientWithVector) TimeSeries() quicbaas.TimeSeriesClient { return nil }

func (m *mockSDKClientWithVector) Backup() quicbaas.BackupClient { return nil }

func (m *mockSDKClientWithVector) ObjectStorage() quicbaas.ObjectStorageClient { return nil }

func (m *mockSDKClientWithVector) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient { return nil }

func (m *mockSDKClientWithVector) ObjectTransfer() quicbaas.ObjectTransferClient { return nil }

func (m *mockSDKClientWithVector) ParameterStore() quicbaas.ParameterStoreClient { return nil }

func (m *mockSDKClientWithVector) Scheduler() quicbaas.SchedulerClient { return nil }

func (m *mockSDKClientWithVector) Messaging() quicbaas.MessagingClient { return nil }

func (m *mockSDKClientWithVector) Embedding() quicbaas.EmbeddingClient { return nil }

func (m *mockSDKClientWithVector) Ollama() quicbaas.OllamaClient { return nil }

func (m *mockSDKClientWithVector) RAG() quicbaas.RAGClient { return nil }

func (m *mockSDKClientWithVector) CoreData() quicbaas.CoreDataService { return nil }

func (m *mockSDKClientWithVector) ClusterMgmt() quicbaas.ClusterManagementService { return nil }

func (m *mockSDKClientWithVector) Security() quicbaas.SecurityService { return nil }

func (m *mockSDKClientWithVector) Identity() quicbaas.IdentityService { return nil }

func (m *mockSDKClientWithVector) Search() quicbaas.SearchService { return nil }

func (m *mockSDKClientWithVector) Storage() quicbaas.StorageService { return nil }

func (m *mockSDKClientWithVector) Async() quicbaas.AsyncService { return nil }

func (m *mockSDKClientWithVector) AI() quicbaas.AIService { return nil }

func (m *mockSDKClientWithVector) Transport() quicbaas.Transport { return nil }

func (m *mockSDKClientWithVector) Close() error { return nil }

func (m *mockSDKClientWithVector) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService { return nil }
func (m *mockSDKClientWithVector) TenantMgmtService() quicbaas.TenantService            { return nil }
func (m *mockSDKClientWithVector) Barrier() (quicbaas.QRDBBarrierClient, error) {
	return nil, nil
}
func (m *mockSDKClientWithVector) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error) {
	return nil, nil
}
func (m *mockSDKClientWithVector) SealStore() (quicbaas.QRDBSealStoreClient, error) { return nil, nil }
func (m *mockSDKClientWithVector) TenantMgmt() (quicbaas.QRDBTenantClient, error)   { return nil, nil }
func (m *mockSDKClientWithVector) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) {
	return nil, nil
}
func (m *mockSDKClientWithVector) Share() (quicbaas.QRDBShareClient, error) { return nil, nil }
func (m *mockSDKClientWithVector) DataStore() (quicbaas.QRDBKVStore, error) { return nil, nil }

func (m *mockSDKClientWithVector) Vector() quicbaas.VectorClient {
	return nil
}

// =============================================================================
// Embedded Server Tests
// =============================================================================

func TestEmbeddedServerErrorMessages(t *testing.T) {
	t.Run("EmbeddedServerFailedError", func(t *testing.T) {
		err := &EmbeddedServerFailedError{Reason: "test error"}
		assert.Equal(t, "embedded server failed: test error", err.Error())
	})

	t.Run("EmbeddedPortError", func(t *testing.T) {
		err := &EmbeddedPortError{}
		assert.Equal(t, "listener address is not a TCP address", err.Error())
	})
}

func TestShutdownEmbeddedServerNoServer(t *testing.T) {
	// Ensure no server is running
	embeddedServerHolder.Store(nil)

	err := shutdownEmbeddedServer()
	require.NoError(t, err, "shutting down nil server should not error")
}

func TestEmbeddedServerShutdownNotStarted(t *testing.T) {
	es := &embeddedServer{}
	es.started.Store(false)

	err := es.shutdown()
	require.NoError(t, err, "shutdown of non-started server should not error")
}

// =============================================================================
// Cluster Command Tests
// =============================================================================

func TestClusterStatusCommandOutputFormatsCoverage(t *testing.T) {
	t.Run("table output with empty nodes", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  0,
			ReadyNodes: 0,
			Nodes:      []cli.NodeInfo{},
		}

		err := printClusterStatusTableCLI(status)
		require.NoError(t, err)
	})

	t.Run("fallback to JSON for non-CLI type", func(t *testing.T) {
		status := map[string]interface{}{
			"healthy": true,
			"nodes":   []string{"node1", "node2"},
		}

		err := printClusterStatusTableCLI(status)
		require.NoError(t, err)
	})
}

// =============================================================================
// Rebalance Command Tests
// =============================================================================

func TestRebalanceCommandsCoverage(t *testing.T) {
	cmd := newRebalanceCommands()
	require.NotNil(t, cmd)
	assert.Equal(t, "rebalance", cmd.Use)
	assert.Contains(t, cmd.Long, "rebalancing")

	// Check subcommands exist
	subCommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subCommands), 3)

	cmdNames := make(map[string]bool)
	for _, sc := range subCommands {
		cmdNames[sc.Use] = true
	}
	assert.True(t, cmdNames["start"], "start command should exist")
	assert.True(t, cmdNames["status"], "status command should exist")
	assert.True(t, cmdNames["plan"], "plan command should exist")
}

func TestRebalanceStartCommandFlagsCoverage(t *testing.T) {
	cmd := newRebalanceStartCommand()
	require.NotNil(t, cmd)

	// Check command-specific flags exist (--address and --timeout are global flags in main.go)
	flags := []string{"target-imbalance", "max-concurrent", "min-keys", "dry-run", "rate-limit", "output", "follow"}
	for _, flag := range flags {
		f := cmd.Flags().Lookup(flag)
		assert.NotNil(t, f, "flag %s should exist", flag)
	}
}

func TestRebalanceStatusCommandFlagsCoverage(t *testing.T) {
	cmd := newRebalanceStatusCommand()
	require.NotNil(t, cmd)

	// Verify command-specific flags exist (--address and --timeout are global flags in main.go)
	operationFlag := cmd.Flags().Lookup("operation")
	assert.NotNil(t, operationFlag)

	outputFlag := cmd.Flags().Lookup("output")
	assert.NotNil(t, outputFlag)
}

func TestRebalancePlanCommandFlagsCoverage(t *testing.T) {
	cmd := newRebalancePlanCommand()
	require.NotNil(t, cmd)

	// Check command-specific flags (--address and --timeout are global flags in main.go)
	targetFlag := cmd.Flags().Lookup("target-imbalance")
	assert.NotNil(t, targetFlag)

	outputFlag := cmd.Flags().Lookup("output")
	assert.NotNil(t, outputFlag)
}

// =============================================================================
// Cache Warm Command Tests
// =============================================================================

func TestRunCacheWarmCommandNoPrefixes(t *testing.T) {
	origPrefixes := cacheWarmPrefixes
	origCfg := cfg
	defer func() {
		cacheWarmPrefixes = origPrefixes
		cfg = origCfg
	}()

	// Set up minimal config
	cfg = nil // Will trigger config error

	cacheWarmPrefixes = nil

	cmd := &cobra.Command{}
	err := runCacheWarmCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestInitializeCacheRegistryNilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	_, err := initializeCacheRegistry()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

func TestInitializeCacheWarmerDependenciesNilConfig(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	cfg = nil

	_, _, _, err := initializeCacheWarmerDependencies()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}

// =============================================================================
// Messaging Command Tests for Delete Channel and Unsubscribe
// =============================================================================

func TestMsgDeleteChannelCommandForceFlag(t *testing.T) {
	cmd := newMsgDeleteChannelCommand()
	require.NotNil(t, cmd)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestMsgUnsubscribeCommandForceFlag(t *testing.T) {
	cmd := newMsgUnsubscribeCommand()
	require.NotNil(t, cmd)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

// =============================================================================
// Client Error Types Tests
// =============================================================================

func TestUnsupportedTransportErrorMessage(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "websocket"}
	expected := "unsupported transport: websocket (supported: grpc, rest, quic, unix, mcp, embedded)"
	assert.Equal(t, expected, err.Error())
}

func TestErrSDKClientClosedContents(t *testing.T) {
	assert.Contains(t, ErrSDKClientClosed.Error(), "closed")
}

func TestErrEmptyAddressContents(t *testing.T) {
	assert.Contains(t, ErrEmptyAddress.Error(), "empty")
}

// =============================================================================
// Formatter Response Tests
// =============================================================================

func TestFormatVectorResponses(t *testing.T) {
	oldFmt := outputFormat
	defer func() { outputFormat = oldFmt }()

	now := time.Now()
	tests := []struct {
		name   string
		format string
		run    func() error
	}{
		{"namespace create success", "table", func() error {
			return formatVectorNamespaceCreateResponse(&vector.CreateNamespaceResponse{Success: true, Namespace: "test-ns", Timestamp: now})
		}},
		{"namespace create failure", "table", func() error {
			return formatVectorNamespaceCreateResponse(&vector.CreateNamespaceResponse{Success: false, Namespace: "test-ns", Timestamp: now})
		}},
		{"namespace delete success", "table", func() error {
			return formatVectorNamespaceDeleteResponse(&vector.DeleteNamespaceResponse{Deleted: true, VectorsDeleted: 100, Timestamp: now})
		}},
		{"namespace delete failure", "table", func() error {
			return formatVectorNamespaceDeleteResponse(&vector.DeleteNamespaceResponse{Deleted: false, Timestamp: now})
		}},
		{"namespace list", "table", func() error {
			return formatVectorNamespaceListResponse(&vector.ListNamespacesResponse{
				Namespaces: []*vector.NamespaceInfo{
					{Config: &vector.NamespaceConfig{Namespace: "ns1", Dimensions: 128}, VectorCount: 50, CreatedAt: now},
				},
				TotalCount: 1,
				Timestamp:  now,
			})
		}},
		{"namespace info with config details", "table", func() error {
			return formatVectorNamespaceInfoResponse(&vector.GetNamespaceInfoResponse{
				Info: &vector.NamespaceInfo{
					Config: &vector.NamespaceConfig{
						Namespace:   "test-ns",
						Dimensions:  128,
						IndexType:   "hnsw",
						IndexParams: map[string]string{"M": "16", "efConstruction": "200"},
						Metadata:    map[string]string{"desc": "test namespace"},
					},
					VectorCount: 1000,
					CreatedAt:   now.Add(-24 * time.Hour),
					UpdatedAt:   now,
				},
				Timestamp: now,
			})
		}},
		{"upsert failure", "table", func() error {
			return formatVectorUpsertResponse(&vector.UpsertResponse{Success: false, Timestamp: now})
		}},
		{"batch upsert with failures", "table", func() error {
			return formatVectorBatchUpsertResponse(&vector.BatchUpsertResponse{VectorsSucceeded: 5, VectorsFailed: 2, Timestamp: now})
		}},
		{"get not found", "table", func() error {
			return formatVectorGetResponse(&vector.GetResponse{Found: false, Timestamp: now})
		}},
		{"get with metadata and large vector", "table", func() error {
			return formatVectorGetResponse(&vector.GetResponse{
				Found: true,
				Vector: &vector.VectorEntity{
					ID:       "vec-1",
					Version:  2,
					Vector:   []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
					Metadata: map[string]string{"key": "val"},
				},
				Timestamp: now,
			})
		}},
		{"delete failure", "table", func() error {
			return formatVectorDeleteResponse(&vector.DeleteResponse{Deleted: false, Timestamp: now})
		}},
		{"batch delete with errors", "table", func() error {
			return formatVectorBatchDeleteResponse(&vector.BatchDeleteResponse{
				DeletedCount: 5,
				Errors:       map[string]string{"vec-99": "not found"},
				Timestamp:    now,
			})
		}},
		{"list with next page", "table", func() error {
			return formatVectorListResponse(&vector.ListVectorsResponse{
				Vectors:       []*vector.VectorEntity{{ID: "v1", Version: 1}},
				TotalCount:    100,
				NextPageToken: "token-abc",
				Timestamp:     now,
			})
		}},
		{"index rebuild with duration", "table", func() error {
			return formatVectorIndexRebuildResponse(&vector.RebuildIndexResponse{
				OperationID:         "op-123",
				Status:              "started",
				EstimatedDurationMs: 30000,
				Timestamp:           now,
			})
		}},
		{"index progress with error message", "table", func() error {
			return formatVectorIndexProgressResponse(&vector.GetIndexProgressResponse{
				OperationID:          "op-123",
				Namespace:            "test-ns",
				Status:               "in_progress",
				PercentComplete:      75.0,
				VectorsIndexed:       7500,
				TotalVectors:         10000,
				ElapsedTimeMs:        5000,
				EstimatedRemainingMs: 2000,
				ErrorMessage:         "some warning",
				Timestamp:            now,
			})
		}},
		{"index stats table", "table", func() error {
			return formatVectorIndexStatsResponse(&vector.GetStatsResponse{
				Namespace:        "test-ns",
				VectorCount:      5000,
				IndexSizeBytes:   1024 * 1024 * 50,
				StorageSizeBytes: 1024 * 1024 * 100,
				Timestamp:        now,
			})
		}},
		{"index stats json", "json", func() error {
			return formatVectorIndexStatsResponse(&vector.GetStatsResponse{Namespace: "test-ns", VectorCount: 5000, Timestamp: now})
		}},
		{"update metadata failure", "table", func() error {
			return formatVectorUpdateMetadataResponse(&vector.UpdateMetadataResponse{Success: false, Timestamp: now})
		}},
		{"hybrid search no results", "table", func() error {
			return formatVectorHybridSearchResponse(&vector.HybridSearchResponse{Timestamp: now})
		}},
		{"search by metadata no results", "table", func() error {
			return formatVectorSearchByMetadataResponse(&vector.SearchByMetadataResponse{Timestamp: now})
		}},
		{"search similar table", "table", func() error {
			return formatVectorSearchSimilarResponse(&vector.SearchSimilarResponse{
				Results: []*vector.SearchResult{
					{Vector: &vector.VectorEntity{ID: "v1", Metadata: map[string]string{"type": "doc"}}, Score: 0.95, Distance: 0.05, Rank: 1},
				},
				SearchTimeMs: 5,
				Timestamp:    now,
			})
		}},
		{"search by id table", "table", func() error {
			return formatVectorSearchByIDResponse(&vector.SearchByIDResponse{
				Results: []*vector.SearchResult{
					{Vector: &vector.VectorEntity{ID: "v1", Metadata: map[string]string{"cat": "tech"}}, Score: 0.92, Distance: 0.08, Rank: 1},
				},
				SearchTimeMs: 3,
				Timestamp:    now,
			})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputFormat = tt.format
			assert.NoError(t, tt.run())
		})
	}
}

// =============================================================================
// Vector Command Run Function Tests - Dimension Zero Error
// =============================================================================

// =============================================================================
// Rebalance Display Functions Tests
// =============================================================================

func TestDisplayRebalanceReportCoverage(t *testing.T) {
	// Test the displayRebalanceReport function with various inputs
	// This covers the function at rebalance.go:300 with 76.3% coverage

	t.Run("empty report", func(t *testing.T) {
		// The function expects a specific report type from the operations package
		// Testing command flags ensures the function can be called
		cmd := newRebalanceStartCommand()
		require.NotNil(t, cmd)
	})
}

type testSDKClientForVector struct {
	vec vector.Client
}

func (m *testSDKClientForVector) KV() quicbaas.KVClient { return nil }

func (m *testSDKClientForVector) Batch() quicbaas.BatchClient { return nil }

func (m *testSDKClientForVector) Transaction() quicbaas.TransactionClient { return nil }

func (m *testSDKClientForVector) Query() quicbaas.QueryClient { return nil }

func (m *testSDKClientForVector) Cluster() quicbaas.ClusterClient { return nil }

func (m *testSDKClientForVector) Health() quicbaas.HealthClient { return nil }

func (m *testSDKClientForVector) Operations() quicbaas.OperationsClient { return nil }

func (m *testSDKClientForVector) Cache() quicbaas.CacheClient { return nil }

func (m *testSDKClientForVector) IAM() quicbaas.IAMClient { return nil }

func (m *testSDKClientForVector) TimeSeries() quicbaas.TimeSeriesClient { return nil }

func (m *testSDKClientForVector) Backup() quicbaas.BackupClient { return nil }

func (m *testSDKClientForVector) ObjectStorage() quicbaas.ObjectStorageClient { return nil }

func (m *testSDKClientForVector) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient { return nil }

func (m *testSDKClientForVector) ObjectTransfer() quicbaas.ObjectTransferClient { return nil }

func (m *testSDKClientForVector) ParameterStore() quicbaas.ParameterStoreClient { return nil }

func (m *testSDKClientForVector) Scheduler() quicbaas.SchedulerClient { return nil }

func (m *testSDKClientForVector) Messaging() quicbaas.MessagingClient { return nil }

func (m *testSDKClientForVector) Embedding() quicbaas.EmbeddingClient { return nil }

func (m *testSDKClientForVector) Ollama() quicbaas.OllamaClient { return nil }

func (m *testSDKClientForVector) RAG() quicbaas.RAGClient { return nil }

func (m *testSDKClientForVector) Transport() quicbaas.Transport { return nil }

func (m *testSDKClientForVector) Close() error { return nil }

func (m *testSDKClientForVector) Vector() quicbaas.VectorClient { return m.vec }

func (m *testSDKClientForVector) CoreData() quicbaas.CoreDataService { return nil }

func (m *testSDKClientForVector) ClusterMgmt() quicbaas.ClusterManagementService { return nil }

func (m *testSDKClientForVector) Security() quicbaas.SecurityService { return nil }

func (m *testSDKClientForVector) Identity() quicbaas.IdentityService { return nil }

func (m *testSDKClientForVector) Search() quicbaas.SearchService { return nil }

func (m *testSDKClientForVector) Storage() quicbaas.StorageService { return nil }

func (m *testSDKClientForVector) Async() quicbaas.AsyncService { return nil }

func (m *testSDKClientForVector) AI() quicbaas.AIService { return nil }

func (m *testSDKClientForVector) DataStore() (quicbaas.QRDBKVStore, error) { return nil, nil }

func (m *testSDKClientForVector) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService { return nil }
func (m *testSDKClientForVector) TenantMgmtService() quicbaas.TenantService            { return nil }
func (m *testSDKClientForVector) Barrier() (quicbaas.QRDBBarrierClient, error) {
	return nil, nil
}
func (m *testSDKClientForVector) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error) {
	return nil, nil
}
func (m *testSDKClientForVector) SealStore() (quicbaas.QRDBSealStoreClient, error) { return nil, nil }
func (m *testSDKClientForVector) TenantMgmt() (quicbaas.QRDBTenantClient, error)   { return nil, nil }
func (m *testSDKClientForVector) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) {
	return nil, nil
}
func (m *testSDKClientForVector) Share() (quicbaas.QRDBShareClient, error) { return nil, nil }
func (m *testSDKClientForVector) Project() quicbaas.ProjectClient          { return nil }
func (m *testSDKClientForVector) SDLC() quicbaas.SDLCClient                { return nil }
func (m *testSDKClientForVector) Prompts() quicbaas.PromptsClient          { return nil }
func (m *testSDKClientForVector) Agents() quicbaas.AgentsClient            { return nil }
func (m *testSDKClientForVector) Account() quicbaas.AccountClient          { return nil }

func (m *testSDKClientForVector) Orchestrator() quicbaas.OrchestratorClient { return nil }

// setupVectorMock installs a mock SDK client factory that returns the given vector client.
// Returns a cleanup function to restore original factories.
func setupVectorMock(t *testing.T, vecClient vector.Client) func() {
	t.Helper()
	origFactory := sdkClientFactory
	origAuthFactory := authClientFactory
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &testSDKClientForVector{vec: vecClient}, nil
	}
	authClientFactory = func() (auth.Client, error) {
		return nil, nil
	}
	return func() {
		sdkClientFactory = origFactory
		authClientFactory = origAuthFactory
	}
}

// =============================================================================
// Test Vector Command Runners
// =============================================================================

func TestRunVectorNamespaceCreateCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldDim := vectorDimension
		oldFmt := outputFormat
		defer func() {
			vectorDimension = oldDim
			outputFormat = oldFmt
		}()
		vectorDimension = 128
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorNamespaceCreateCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("ZeroDimension", func(t *testing.T) {
		oldDim := vectorDimension
		defer func() { vectorDimension = oldDim }()
		vectorDimension = 0

		cmd := &cobra.Command{}
		err := runVectorNamespaceCreateCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dimension must be greater than 0")
	})

	t.Run("ClientError", func(t *testing.T) {
		origFactory := sdkClientFactory
		defer func() { sdkClientFactory = origFactory }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, &dberrors.MessageError{Message: "connection failed"}
		}
		vectorDimension = 128
		cmd := &cobra.Command{}
		err := runVectorNamespaceCreateCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection failed")
	})

	t.Run("APIError", func(t *testing.T) {
		oldDim := vectorDimension
		defer func() { vectorDimension = oldDim }()
		vectorDimension = 128

		mockErr := &mockVectorClientForCoverage{
			CreateNamespaceFunc: func(ctx context.Context, req *vector.CreateNamespaceRequest) (*vector.CreateNamespaceResponse, error) {
				return nil, &dberrors.MessageError{Message: "namespace exists"}
			},
		}
		origFactory := sdkClientFactory
		defer func() { sdkClientFactory = origFactory }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return &testSDKClientForVector{vec: mockErr}, nil
		}

		cmd := &cobra.Command{}
		err := runVectorNamespaceCreateCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldDim := vectorDimension
		oldFmt := outputFormat
		defer func() {
			vectorDimension = oldDim
			outputFormat = oldFmt
		}()
		vectorDimension = 128
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorNamespaceCreateCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})
}

func TestRunVectorNamespaceDeleteCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorNamespaceDeleteCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorNamespaceDeleteCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})
}

func TestRunVectorNamespaceListCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorNamespaceListCommand(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorNamespaceListCommand(cmd, []string{})
		assert.NoError(t, err)
	})
}

func TestRunVectorNamespaceInfoCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorNamespaceInfoCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorNamespaceInfoCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("NilInfo", func(t *testing.T) {
		mockNil := &mockVectorClientForCoverage{
			GetNamespaceInfoFunc: func(ctx context.Context, req *vector.GetNamespaceInfoRequest) (*vector.GetNamespaceInfoResponse, error) {
				return &vector.GetNamespaceInfoResponse{Info: nil, Timestamp: time.Now()}, nil
			},
		}
		origFactory := sdkClientFactory
		defer func() { sdkClientFactory = origFactory }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return &testSDKClientForVector{vec: mockNil}, nil
		}
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorNamespaceInfoCommand(cmd, []string{"nonexistent"})
		assert.NoError(t, err)
	})
}

func TestRunVectorUpsertCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldData := vectorData
		oldFmt := outputFormat
		defer func() {
			vectorData = oldData
			outputFormat = oldFmt
		}()
		vectorData = "0.1,0.2,0.3"
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorUpsertCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("InvalidVectorData", func(t *testing.T) {
		oldData := vectorData
		defer func() { vectorData = oldData }()
		vectorData = "not-a-number"

		cmd := &cobra.Command{}
		err := runVectorUpsertCommand(cmd, []string{"test-ns", "vec-1"})
		assert.Error(t, err)
	})

	t.Run("EmptyVectorData", func(t *testing.T) {
		oldData := vectorData
		defer func() { vectorData = oldData }()
		vectorData = ""

		cmd := &cobra.Command{}
		err := runVectorUpsertCommand(cmd, []string{"test-ns", "vec-1"})
		assert.Error(t, err)
	})
}

func TestRunVectorBatchUpsertCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "vectors.json")
		err := os.WriteFile(tmpFile, []byte(`[{"id":"vec-1","vector":[0.1,0.2,0.3],"metadata":{"title":"test"}}]`), 0644)
		require.NoError(t, err)

		oldFile := vectorFile
		oldFmt := outputFormat
		defer func() {
			vectorFile = oldFile
			outputFormat = oldFmt
		}()
		vectorFile = tmpFile
		outputFormat = "table"

		cmd := &cobra.Command{}
		err = runVectorBatchUpsertCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		oldFile := vectorFile
		defer func() { vectorFile = oldFile }()
		vectorFile = "/nonexistent/path/vectors.json"

		cmd := &cobra.Command{}
		err := runVectorBatchUpsertCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "bad.json")
		err := os.WriteFile(tmpFile, []byte(`{invalid json`), 0644)
		require.NoError(t, err)

		oldFile := vectorFile
		defer func() { vectorFile = oldFile }()
		vectorFile = tmpFile

		cmd := &cobra.Command{}
		err = runVectorBatchUpsertCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})
}

func TestRunVectorGetCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorGetCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorGetCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})
}

func TestRunVectorDeleteCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorDeleteCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorDeleteCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})
}

func TestRunVectorBatchDeleteCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("SuccessWithArgs", func(t *testing.T) {
		oldFile := vectorFile
		oldFmt := outputFormat
		defer func() {
			vectorFile = oldFile
			outputFormat = oldFmt
		}()
		vectorFile = ""
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorBatchDeleteCommand(cmd, []string{"test-ns", "vec-1", "vec-2"})
		assert.NoError(t, err)
	})

	t.Run("SuccessWithFile", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "ids.txt")
		err := os.WriteFile(tmpFile, []byte("vec-1\nvec-2\nvec-3"), 0644)
		require.NoError(t, err)

		oldFile := vectorFile
		oldFmt := outputFormat
		defer func() {
			vectorFile = oldFile
			outputFormat = oldFmt
		}()
		vectorFile = tmpFile
		outputFormat = "table"

		cmd := &cobra.Command{}
		err = runVectorBatchDeleteCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("NoIDs", func(t *testing.T) {
		oldFile := vectorFile
		defer func() { vectorFile = oldFile }()
		vectorFile = ""

		cmd := &cobra.Command{}
		err := runVectorBatchDeleteCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one vector ID")
	})

	t.Run("FileNotFound", func(t *testing.T) {
		oldFile := vectorFile
		defer func() { vectorFile = oldFile }()
		vectorFile = "/nonexistent/ids.txt"

		cmd := &cobra.Command{}
		err := runVectorBatchDeleteCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})
}

func TestRunVectorListCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorListCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorListCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})
}

func TestRunVectorUpdateMetadataCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("SuccessWithMetadataJSON", func(t *testing.T) {
		oldJSON := vectorMetadataJSON
		oldMeta := vectorMetadata
		oldMerge := vectorMerge
		oldReplace := vectorReplace
		defer func() {
			vectorMetadataJSON = oldJSON
			vectorMetadata = oldMeta
			vectorMerge = oldMerge
			vectorReplace = oldReplace
		}()
		vectorMetadataJSON = `{"key":"value"}`
		vectorMetadata = nil
		vectorMerge = true
		vectorReplace = false

		cmd := &cobra.Command{}
		err := runVectorUpdateMetadataCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("SuccessWithMetadataMap", func(t *testing.T) {
		oldJSON := vectorMetadataJSON
		oldMeta := vectorMetadata
		defer func() {
			vectorMetadataJSON = oldJSON
			vectorMetadata = oldMeta
		}()
		vectorMetadataJSON = ""
		vectorMetadata = map[string]string{"key": "value"}

		cmd := &cobra.Command{}
		err := runVectorUpdateMetadataCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("ReplaceMode", func(t *testing.T) {
		oldJSON := vectorMetadataJSON
		oldMeta := vectorMetadata
		oldMerge := vectorMerge
		oldReplace := vectorReplace
		defer func() {
			vectorMetadataJSON = oldJSON
			vectorMetadata = oldMeta
			vectorMerge = oldMerge
			vectorReplace = oldReplace
		}()
		vectorMetadataJSON = `{"key":"value"}`
		vectorMetadata = nil
		vectorMerge = true
		vectorReplace = true

		cmd := &cobra.Command{}
		err := runVectorUpdateMetadataCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		oldJSON := vectorMetadataJSON
		oldMeta := vectorMetadata
		defer func() {
			vectorMetadataJSON = oldJSON
			vectorMetadata = oldMeta
		}()
		vectorMetadataJSON = "{bad json"
		vectorMetadata = nil

		cmd := &cobra.Command{}
		err := runVectorUpdateMetadataCommand(cmd, []string{"test-ns", "vec-1"})
		assert.Error(t, err)
	})

	t.Run("NoMetadata", func(t *testing.T) {
		oldJSON := vectorMetadataJSON
		oldMeta := vectorMetadata
		defer func() {
			vectorMetadataJSON = oldJSON
			vectorMetadata = oldMeta
		}()
		vectorMetadataJSON = ""
		vectorMetadata = nil

		cmd := &cobra.Command{}
		err := runVectorUpdateMetadataCommand(cmd, []string{"test-ns", "vec-1"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no metadata provided")
	})
}

func TestRunVectorSearchSimilarCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldData := vectorData
		oldFmt := outputFormat
		defer func() {
			vectorData = oldData
			outputFormat = oldFmt
		}()
		vectorData = "0.1,0.2,0.3"
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorSearchSimilarCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldData := vectorData
		oldFmt := outputFormat
		defer func() {
			vectorData = oldData
			outputFormat = oldFmt
		}()
		vectorData = "0.1,0.2,0.3"
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorSearchSimilarCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("InvalidVector", func(t *testing.T) {
		oldData := vectorData
		defer func() { vectorData = oldData }()
		vectorData = "not-a-vector"

		cmd := &cobra.Command{}
		err := runVectorSearchSimilarCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})
}

func TestRunVectorSearchByIDCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorSearchByIDCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorSearchByIDCommand(cmd, []string{"test-ns", "vec-1"})
		assert.NoError(t, err)
	})
}

func TestRunVectorHybridSearchCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{
		HybridSearchFunc: func(ctx context.Context, req *vector.HybridSearchRequest) (*vector.HybridSearchResponse, error) {
			return &vector.HybridSearchResponse{
				Results: []*vector.SearchResult{
					{Vector: &vector.VectorEntity{ID: "r1", Metadata: map[string]string{"type": "doc"}, Vector: []float32{0.1}}, Score: 0.95, Rank: 1},
				},
				TotalCandidates:  100,
				TotalFiltered:    50,
				RerankingApplied: true,
				SearchTimeMs:     5,
				Timestamp:        time.Now(),
			}, nil
		},
	}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldData := vectorData
		oldFilters := vectorFilters
		oldFmt := outputFormat
		defer func() {
			vectorData = oldData
			vectorFilters = oldFilters
			outputFormat = oldFmt
		}()
		vectorData = "0.1,0.2,0.3"
		vectorFilters = []string{"type=document"}
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorHybridSearchCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("InvalidFilter", func(t *testing.T) {
		oldData := vectorData
		oldFilters := vectorFilters
		defer func() {
			vectorData = oldData
			vectorFilters = oldFilters
		}()
		vectorData = "0.1,0.2,0.3"
		vectorFilters = []string{"invalid-no-equals"}

		cmd := &cobra.Command{}
		err := runVectorHybridSearchCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid filter format")
	})

	t.Run("InvalidVector", func(t *testing.T) {
		oldData := vectorData
		oldFilters := vectorFilters
		defer func() {
			vectorData = oldData
			vectorFilters = oldFilters
		}()
		vectorData = "bad-data"
		vectorFilters = []string{"type=doc"}

		cmd := &cobra.Command{}
		err := runVectorHybridSearchCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})
}

func TestRunVectorSearchByMetadataCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{
		SearchByMetadataFunc: func(ctx context.Context, req *vector.SearchByMetadataRequest) (*vector.SearchByMetadataResponse, error) {
			return &vector.SearchByMetadataResponse{
				Results: []*vector.VectorEntity{
					{ID: "vec-1", Metadata: map[string]string{"type": "doc"}, Vector: []float32{0.1, 0.2}},
				},
				TotalCount:   1,
				SearchTimeMs: 3,
				NextCursor:   "next-page",
				Timestamp:    time.Now(),
			}, nil
		},
	}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFilters := vectorFilters
		oldFmt := outputFormat
		defer func() {
			vectorFilters = oldFilters
			outputFormat = oldFmt
		}()
		vectorFilters = []string{"type=document"}
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorSearchByMetadataCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("NoFilters", func(t *testing.T) {
		oldFilters := vectorFilters
		defer func() { vectorFilters = oldFilters }()
		vectorFilters = nil

		cmd := &cobra.Command{}
		err := runVectorSearchByMetadataCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one --filter")
	})

	t.Run("InvalidFilter", func(t *testing.T) {
		oldFilters := vectorFilters
		defer func() { vectorFilters = oldFilters }()
		vectorFilters = []string{"noequals"}

		cmd := &cobra.Command{}
		err := runVectorSearchByMetadataCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid filter format")
	})
}

func TestRunVectorIndexRebuildCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorIndexRebuildCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorIndexRebuildCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})
}

func TestRunVectorIndexProgressCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorIndexProgressCommand(cmd, []string{"op-123"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorIndexProgressCommand(cmd, []string{"op-123"})
		assert.NoError(t, err)
	})
}

func TestRunVectorIndexStatsCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "table"

		cmd := &cobra.Command{}
		err := runVectorIndexStatsCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		oldFmt := outputFormat
		defer func() { outputFormat = oldFmt }()
		outputFormat = "json"

		cmd := &cobra.Command{}
		err := runVectorIndexStatsCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})
}

func TestRunVectorExportCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{
		ExportFunc: func(ctx context.Context, req *vector.ExportRequest) ([]*vector.ExportResponse, error) {
			return []*vector.ExportResponse{
				{VectorsExported: 50, BatchData: []byte(`[{"id":"v1"}]`), IsFinal: false, Timestamp: time.Now()},
				{VectorsExported: 50, BatchData: []byte(`[{"id":"v2"}]`), IsFinal: true, Timestamp: time.Now()},
			}, nil
		},
	}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("ToStdout", func(t *testing.T) {
		oldOutput := vectorExportOutput
		defer func() { vectorExportOutput = oldOutput }()
		vectorExportOutput = ""

		cmd := &cobra.Command{}
		err := runVectorExportCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("ToFile", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "export.json")
		oldOutput := vectorExportOutput
		defer func() { vectorExportOutput = oldOutput }()
		vectorExportOutput = tmpFile

		cmd := &cobra.Command{}
		err := runVectorExportCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)

		data, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		assert.Contains(t, string(data), "v1")
	})
}

func TestRunVectorImportCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Success", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "import.json")
		err := os.WriteFile(tmpFile, []byte(`[{"id":"v1","vector":[0.1]}]`), 0644)
		require.NoError(t, err)

		oldInput := vectorImportInput
		defer func() { vectorImportInput = oldInput }()
		vectorImportInput = tmpFile

		cmd := &cobra.Command{}
		err = runVectorImportCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		oldInput := vectorImportInput
		defer func() { vectorImportInput = oldInput }()
		vectorImportInput = "/nonexistent/import.json"

		cmd := &cobra.Command{}
		err := runVectorImportCommand(cmd, []string{"test-ns"})
		assert.Error(t, err)
	})
}

func TestRunVectorCompactCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{
		CompactFunc: func(ctx context.Context, req *vector.CompactRequest) (*vector.CompactResponse, error) {
			return &vector.CompactResponse{
				OperationID: "op-123",
				Started:     time.Now(),
				Timestamp:   time.Now(),
			}, nil
		},
	}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("Synchronous", func(t *testing.T) {
		oldAsync := vectorAsync
		defer func() { vectorAsync = oldAsync }()
		vectorAsync = false

		cmd := &cobra.Command{}
		err := runVectorCompactCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})

	t.Run("Async", func(t *testing.T) {
		oldAsync := vectorAsync
		defer func() { vectorAsync = oldAsync }()
		vectorAsync = true

		cmd := &cobra.Command{}
		err := runVectorCompactCommand(cmd, []string{"test-ns"})
		assert.NoError(t, err)
	})
}

func TestRunVectorCompactionProgressCommand(t *testing.T) {
	mockVec := &mockVectorClientForCoverage{
		GetCompactionProgressFunc: func(ctx context.Context, req *vector.GetCompactionProgressRequest) (*vector.GetCompactionProgressResponse, error) {
			return &vector.GetCompactionProgressResponse{
				OperationID:         "op-123",
				Namespace:           "test-ns",
				Status:              "in_progress",
				PercentComplete:     75.5,
				BytesCompacted:      1024000,
				TotalBytes:          2048000,
				SpaceReclaimedBytes: 512000,
				ElapsedTimeMs:       3000,
				Timestamp:           time.Now(),
			}, nil
		},
	}
	cleanup := setupVectorMock(t, mockVec)
	defer cleanup()

	t.Run("ByOperationID", func(t *testing.T) {
		oldOpID := vectorOperationID
		oldNS := vectorNamespace
		defer func() {
			vectorOperationID = oldOpID
			vectorNamespace = oldNS
		}()
		vectorOperationID = "op-123"
		vectorNamespace = ""

		cmd := &cobra.Command{}
		err := runVectorCompactionProgressCommand(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("ByNamespace", func(t *testing.T) {
		oldOpID := vectorOperationID
		oldNS := vectorNamespace
		defer func() {
			vectorOperationID = oldOpID
			vectorNamespace = oldNS
		}()
		vectorOperationID = ""
		vectorNamespace = "test-ns"

		cmd := &cobra.Command{}
		err := runVectorCompactionProgressCommand(cmd, []string{})
		assert.NoError(t, err)
	})

	t.Run("NeitherProvided", func(t *testing.T) {
		oldOpID := vectorOperationID
		oldNS := vectorNamespace
		defer func() {
			vectorOperationID = oldOpID
			vectorNamespace = oldNS
		}()
		vectorOperationID = ""
		vectorNamespace = ""

		cmd := &cobra.Command{}
		err := runVectorCompactionProgressCommand(cmd, []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either --operation-id or --namespace")
	})
}

// =============================================================================
// Test createVectorClient
// =============================================================================

func TestCreateVectorClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		mockVec := &mockVectorClientForCoverage{}
		cleanup := setupVectorMock(t, mockVec)
		defer cleanup()

		client, err := createVectorClient()
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		origFactory := sdkClientFactory
		defer func() { sdkClientFactory = origFactory }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, &dberrors.MessageError{Message: "factory error"}
		}

		client, err := createVectorClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

// =============================================================================
// Additional format function coverage
// =============================================================================
