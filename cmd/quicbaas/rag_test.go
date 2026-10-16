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
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage/memory"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

type mockRAGClient struct {
	QueryFunc        func(ctx context.Context, req *rag.QueryRequest) (*rag.QueryResponse, error)
	BuildContextFunc func(ctx context.Context, req *rag.BuildContextRequest) (*rag.BuildContextResponse, error)
}

func (m *mockRAGClient) Query(ctx context.Context, req *rag.QueryRequest) (*rag.QueryResponse, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, req)
	}
	return &rag.QueryResponse{
		Context:     "Test context from RAG query",
		Tokens:      100,
		Truncated:   false,
		ResultCount: 5,
		Citations:   []rag.Citation{},
	}, nil
}

func (m *mockRAGClient) BuildContext(ctx context.Context, req *rag.BuildContextRequest) (*rag.BuildContextResponse, error) {
	if m.BuildContextFunc != nil {
		return m.BuildContextFunc(ctx, req)
	}
	return &rag.BuildContextResponse{
		Context:     "Built context from provided results",
		Tokens:      50,
		Truncated:   false,
		ResultCount: len(req.Results),
		Citations:   []rag.Citation{},
	}, nil
}

// mockRAGSDKClient implements quicbaas.Client for RAG testing.
type mockRAGSDKClient struct {
	ragClient *mockRAGClient
}

func (m *mockRAGSDKClient) KV() quicbaas.KVClient                                 { return nil }
func (m *mockRAGSDKClient) Batch() quicbaas.BatchClient                           { return nil }
func (m *mockRAGSDKClient) Transaction() quicbaas.TransactionClient               { return nil }
func (m *mockRAGSDKClient) Query() quicbaas.QueryClient                           { return nil }
func (m *mockRAGSDKClient) Cluster() quicbaas.ClusterClient                       { return nil }
func (m *mockRAGSDKClient) Health() quicbaas.HealthClient                         { return nil }
func (m *mockRAGSDKClient) Operations() quicbaas.OperationsClient                 { return nil }
func (m *mockRAGSDKClient) Cache() quicbaas.CacheClient                           { return nil }
func (m *mockRAGSDKClient) IAM() quicbaas.IAMClient                               { return nil }
func (m *mockRAGSDKClient) Vector() quicbaas.VectorClient                         { return nil }
func (m *mockRAGSDKClient) TimeSeries() quicbaas.TimeSeriesClient                 { return nil }
func (m *mockRAGSDKClient) Backup() quicbaas.BackupClient                         { return nil }
func (m *mockRAGSDKClient) ObjectStorage() quicbaas.ObjectStorageClient           { return nil }
func (m *mockRAGSDKClient) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient { return nil }
func (m *mockRAGSDKClient) ObjectTransfer() quicbaas.ObjectTransferClient         { return nil }
func (m *mockRAGSDKClient) ParameterStore() quicbaas.ParameterStoreClient         { return nil }
func (m *mockRAGSDKClient) Scheduler() quicbaas.SchedulerClient                   { return nil }
func (m *mockRAGSDKClient) Messaging() quicbaas.MessagingClient                   { return nil }
func (m *mockRAGSDKClient) Embedding() quicbaas.EmbeddingClient                   { return nil }
func (m *mockRAGSDKClient) Ollama() quicbaas.OllamaClient                         { return nil }
func (m *mockRAGSDKClient) Transport() quicbaas.Transport                         { return nil }
func (m *mockRAGSDKClient) CoreData() quicbaas.CoreDataService                    { return nil }
func (m *mockRAGSDKClient) ClusterMgmt() quicbaas.ClusterManagementService        { return nil }
func (m *mockRAGSDKClient) Security() quicbaas.SecurityService                    { return nil }
func (m *mockRAGSDKClient) Identity() quicbaas.IdentityService                    { return nil }
func (m *mockRAGSDKClient) Search() quicbaas.SearchService                        { return nil }
func (m *mockRAGSDKClient) Storage() quicbaas.StorageService                      { return nil }
func (m *mockRAGSDKClient) Async() quicbaas.AsyncService                          { return nil }
func (m *mockRAGSDKClient) AI() quicbaas.AIService                                { return nil }
func (m *mockRAGSDKClient) DataStore() (quicbaas.QRDBKVStore, error)              { return nil, nil }

func (m *mockRAGSDKClient) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService  { return nil }
func (m *mockRAGSDKClient) TenantMgmtService() quicbaas.TenantService             { return nil }
func (m *mockRAGSDKClient) Barrier() (quicbaas.QRDBBarrierClient, error)          { return nil, nil }
func (m *mockRAGSDKClient) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error) { return nil, nil }
func (m *mockRAGSDKClient) SealStore() (quicbaas.QRDBSealStoreClient, error)      { return nil, nil }
func (m *mockRAGSDKClient) TenantMgmt() (quicbaas.QRDBTenantClient, error)        { return nil, nil }
func (m *mockRAGSDKClient) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) {
	return nil, nil
}
func (m *mockRAGSDKClient) Share() (quicbaas.QRDBShareClient, error) { return nil, nil }

func (m *mockRAGSDKClient) RAG() quicbaas.RAGClient {
	if m.ragClient != nil {
		return m.ragClient
	}
	return &mockRAGClient{}
}

func (m *mockRAGSDKClient) Close() error {
	return nil
}

func (m *mockRAGSDKClient) Project() quicbaas.ProjectClient { return nil }
func (m *mockRAGSDKClient) SDLC() quicbaas.SDLCClient       { return nil }
func (m *mockRAGSDKClient) Prompts() quicbaas.PromptsClient { return nil }
func (m *mockRAGSDKClient) Agents() quicbaas.AgentsClient   { return nil }
func (m *mockRAGSDKClient) Account() quicbaas.AccountClient { return nil }

func (m *mockRAGSDKClient) Orchestrator() quicbaas.OrchestratorClient { return nil }

// saveAndRestoreRAGState saves and returns a function to restore RAG-related global state.
func saveAndRestoreRAGState(t *testing.T) func() {
	savedFactory := sdkClientFactory
	savedTimeout := globalTimeout
	savedOutputFormat := outputFormat
	savedNamespace := ragNamespace
	savedTopK := ragTopK
	savedMaxTokens := ragMaxTokens
	savedMinScore := ragMinScore
	savedModel := ragModel
	savedTemplate := ragTemplate
	savedIncludeCitations := ragIncludeCitations
	savedResultsFile := ragResultsFile
	savedTruncationStrategy := ragTruncationStrategy

	return func() {
		sdkClientFactory = savedFactory
		globalTimeout = savedTimeout
		outputFormat = savedOutputFormat
		ragNamespace = savedNamespace
		ragTopK = savedTopK
		ragMaxTokens = savedMaxTokens
		ragMinScore = savedMinScore
		ragModel = savedModel
		ragTemplate = savedTemplate
		ragIncludeCitations = savedIncludeCitations
		ragResultsFile = savedResultsFile
		ragTruncationStrategy = savedTruncationStrategy
	}
}

func TestNewRAGCommands(t *testing.T) {
	cmd := newRAGCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "rag", cmd.Use)
	assert.Contains(t, cmd.Short, "Retrieval-Augmented Generation")
	assert.Contains(t, cmd.Long, "RAG operations are supported on all transports")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "query", "query command should be registered")
	assert.Contains(t, commandNames, "context", "context command should be registered")
}

func TestNewRAGQueryCommand(t *testing.T) {
	cmd := newRAGQueryCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "query <query-text>", cmd.Use)
	assert.Contains(t, cmd.Short, "all-in-one RAG query")

	// Check flags are defined
	flags := cmd.Flags()

	namespaceFlag := flags.Lookup("namespace")
	require.NotNil(t, namespaceFlag)

	topKFlag := flags.Lookup("top-k")
	require.NotNil(t, topKFlag)
	assert.Equal(t, "10", topKFlag.DefValue)

	maxTokensFlag := flags.Lookup("max-tokens")
	require.NotNil(t, maxTokensFlag)
	assert.Equal(t, "4000", maxTokensFlag.DefValue)

	minScoreFlag := flags.Lookup("min-score")
	require.NotNil(t, minScoreFlag)
	assert.Equal(t, "0.7", minScoreFlag.DefValue)

	templateFlag := flags.Lookup("template")
	require.NotNil(t, templateFlag)
	assert.Equal(t, "markdown", templateFlag.DefValue)

	citationsFlag := flags.Lookup("include-citations")
	require.NotNil(t, citationsFlag)
	assert.Equal(t, "false", citationsFlag.DefValue)
}

func TestNewRAGContextCommand(t *testing.T) {
	cmd := newRAGContextCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "context", cmd.Use)
	assert.Contains(t, cmd.Short, "Build context")

	// Check flags are defined
	flags := cmd.Flags()

	resultsFlag := flags.Lookup("results")
	require.NotNil(t, resultsFlag)

	maxTokensFlag := flags.Lookup("max-tokens")
	require.NotNil(t, maxTokensFlag)
	assert.Equal(t, "4000", maxTokensFlag.DefValue)

	truncationFlag := flags.Lookup("truncation")
	require.NotNil(t, truncationFlag)
	assert.Equal(t, "smart", truncationFlag.DefValue)
}

func TestRunRAGQueryCommand_Success(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	var capturedReq *rag.QueryRequest

	// Set up mock
	mockRAG := &mockRAGClient{
		QueryFunc: func(ctx context.Context, req *rag.QueryRequest) (*rag.QueryResponse, error) {
			capturedReq = req
			return &rag.QueryResponse{
				Context:     "Query result context",
				Tokens:      150,
				Truncated:   false,
				ResultCount: 3,
				Citations: []rag.Citation{
					{Index: 1, FilePath: "file1.go", StartLine: 10, EndLine: 20, Score: 0.95},
				},
			}, nil
		},
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{ragClient: mockRAG}, nil
	}

	globalTimeout = 30 * time.Second
	outputFormat = "json"

	// Create command first (this resets flag variables to defaults)
	cmd := newRAGQueryCommand()

	// Set command flags AFTER creating the command (StringVar resets variables to defaults)
	ragNamespace = "test-namespace"
	ragTopK = 10
	ragMaxTokens = 4000
	ragMinScore = 0.7
	ragTemplate = "markdown"
	ragIncludeCitations = true

	err := runRAGQueryCommand(cmd, []string{"test query"})

	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	assert.Equal(t, "test query", capturedReq.Query)
	assert.Equal(t, "test-namespace", capturedReq.Namespace)
	assert.Equal(t, 10, capturedReq.TopK)
}

func TestRunRAGQueryCommand_EmptyQuery(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{}, nil
	}

	globalTimeout = 30 * time.Second

	cmd := newRAGQueryCommand()
	ragNamespace = "test-namespace"

	err := runRAGQueryCommand(cmd, []string{""})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "query text cannot be empty")
}

func TestRunRAGContextCommand_Success(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	// Create temporary results file FIRST
	results := []struct {
		Content  string            `json:"content"`
		Score    float32           `json:"score"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}{
		{Content: "Test content 1", Score: 0.9, Metadata: map[string]string{"file": "test.go"}},
		{Content: "Test content 2", Score: 0.85, Metadata: nil},
	}

	jsonData, err := json.Marshal(results)
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "rag-results-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(jsonData)
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	var capturedReq *rag.BuildContextRequest

	// Set up mock
	mockRAG := &mockRAGClient{
		BuildContextFunc: func(ctx context.Context, req *rag.BuildContextRequest) (*rag.BuildContextResponse, error) {
			capturedReq = req
			return &rag.BuildContextResponse{
				Context:     "Built context from results",
				Tokens:      75,
				Truncated:   false,
				ResultCount: 2,
			}, nil
		},
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{ragClient: mockRAG}, nil
	}

	globalTimeout = 30 * time.Second
	outputFormat = "json"

	// Create command first (this resets flag variables to defaults)
	cmd := newRAGContextCommand()

	// Set all state AFTER creating the command (StringVar resets variables to defaults)
	ragResultsFile = tmpFile.Name()
	ragMaxTokens = 4000
	ragTemplate = "markdown"
	ragTruncationStrategy = "smart"
	ragIncludeCitations = false

	err = runRAGContextCommand(cmd, []string{})

	require.NoError(t, err)
	require.NotNil(t, capturedReq)
	assert.Equal(t, 2, len(capturedReq.Results))
	assert.Equal(t, "Test content 1", capturedReq.Results[0].Content)
}

func TestRunRAGContextCommand_InvalidJSON(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	// Create temporary file with invalid JSON
	tmpFile, err := os.CreateTemp("", "rag-results-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("invalid json")
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{}, nil
	}

	globalTimeout = 30 * time.Second

	cmd := newRAGContextCommand()
	ragResultsFile = tmpFile.Name()

	err = runRAGContextCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse results JSON")
}

func TestRunRAGContextCommand_EmptyResults(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	// Create temporary file with empty results array
	tmpFile, err := os.CreateTemp("", "rag-results-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("[]")
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{}, nil
	}

	globalTimeout = 30 * time.Second

	cmd := newRAGContextCommand()
	ragResultsFile = tmpFile.Name()

	err = runRAGContextCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "results file contains no results")
}

func TestRunRAGContextCommand_FileNotFound(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{}, nil
	}

	globalTimeout = 30 * time.Second

	cmd := newRAGContextCommand()
	ragResultsFile = "/nonexistent/path/results.json"

	err := runRAGContextCommand(cmd, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read results file")
}

func TestCreateRAGClient(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	mockRAG := &mockRAGClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockRAGSDKClient{ragClient: mockRAG}, nil
	}

	client, err := createRAGClient()
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestFormatRAGQueryResponse_TableFormat(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	outputFormat = "table"

	resp := &rag.QueryResponse{
		Context:     "Test context",
		Tokens:      100,
		Truncated:   false,
		ResultCount: 5,
		Citations: []rag.Citation{
			{Index: 1, FilePath: "file.go", StartLine: 10, EndLine: 20, Score: 0.9},
		},
	}

	err := formatRAGQueryResponse(resp)
	assert.NoError(t, err)
}

func TestFormatRAGQueryResponse_JSONFormat(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	outputFormat = "json"

	resp := &rag.QueryResponse{
		Context:     "Test context",
		Tokens:      100,
		Truncated:   true,
		ResultCount: 3,
	}

	err := formatRAGQueryResponse(resp)
	assert.NoError(t, err)
}

func TestFormatRAGBuildContextResponse_TableFormat(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	outputFormat = "table"

	resp := &rag.BuildContextResponse{
		Context:     "Built context",
		Tokens:      50,
		Truncated:   false,
		ResultCount: 2,
	}

	err := formatRAGBuildContextResponse(resp)
	assert.NoError(t, err)
}

func TestFormatRAGBuildContextResponse_YAMLFormat(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	outputFormat = "yaml"

	resp := &rag.BuildContextResponse{
		Context:     "Built context",
		Tokens:      50,
		Truncated:   false,
		ResultCount: 2,
		Citations: []rag.Citation{
			{Index: 1, FilePath: "test.go", StartLine: 1, EndLine: 10, Score: 0.85},
		},
	}

	err := formatRAGBuildContextResponse(resp)
	assert.NoError(t, err)
}

func TestRunRAGQueryCommand_ClientError(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	// Set up mock to return error when creating client
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	globalTimeout = 30 * time.Second

	cmd := newRAGQueryCommand()
	ragNamespace = "test-namespace"

	err := runRAGQueryCommand(cmd, []string{"test query"})

	require.Error(t, err)
}

func TestRunRAGContextCommand_ClientError(t *testing.T) {
	restore := saveAndRestoreRAGState(t)
	defer restore()

	// Create temporary results file
	tmpFile, err := os.CreateTemp("", "rag-results-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(`[{"content": "test", "score": 0.9}]`)
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	// Set up mock to return error when creating client
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	globalTimeout = 30 * time.Second

	cmd := newRAGContextCommand()
	ragResultsFile = tmpFile.Name()

	err = runRAGContextCommand(cmd, []string{})

	require.Error(t, err)
}

func TestMemoryBatchAdapter_CommitSuccess(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)

	batch := &memoryBatchAdapter{
		storage: memStorage,
	}

	// Put a valid key first
	err := batch.Set([]byte("good-key"), []byte("good-value"))
	assert.NoError(t, err)

	// Commit should succeed
	err = batch.Commit()
	assert.NoError(t, err)

	// Verify the data was written
	val, err := memStorage.Get([]byte("good-key"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("good-value"), val)
}

func TestMemoryBatchAdapter_Delete(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)

	// First put a key directly
	err := memStorage.Set([]byte("to-delete"), []byte("value"))
	require.NoError(t, err)

	batch := &memoryBatchAdapter{
		storage: memStorage,
	}

	// Queue delete
	err = batch.Delete([]byte("to-delete"))
	assert.NoError(t, err)

	// Commit should delete the key
	err = batch.Commit()
	assert.NoError(t, err)

	// Verify deletion
	_, err = memStorage.Get([]byte("to-delete"))
	assert.Error(t, err)
}

func TestMemoryBatchAdapter_Rollback(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)

	batch := &memoryBatchAdapter{
		storage: memStorage,
	}

	// Queue some operations
	err := batch.Set([]byte("key1"), []byte("val1"))
	assert.NoError(t, err)
	err = batch.Set([]byte("key2"), []byte("val2"))
	assert.NoError(t, err)

	// Rollback should clear ops
	err = batch.Rollback()
	assert.NoError(t, err)

	// Commit after rollback should be a no-op
	err = batch.Commit()
	assert.NoError(t, err)

	// Keys should not exist
	_, err = memStorage.Get([]byte("key1"))
	assert.Error(t, err)
}

func TestMemoryBatchAdapter_Close(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)

	batch := &memoryBatchAdapter{
		storage: memStorage,
	}

	err := batch.Set([]byte("key1"), []byte("val1"))
	assert.NoError(t, err)

	// Close should call Rollback internally
	err = batch.Close()
	assert.NoError(t, err)
}

// --------------------------------------------------------------------------
// memoryIteratorAdapter tests (covers SeekToFirst, SeekToLast, Seek,
// Next, Prev, Valid, Key, Value, Error, Close)
// --------------------------------------------------------------------------

func TestMemoryIteratorAdapter_WithData(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	err := memStorage.Set([]byte("aaa"), []byte("v1"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("bbb"), []byte("v2"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("ccc"), []byte("v3"))
	require.NoError(t, err)

	opts := &storage.IteratorOptions{
		LowerBound: []byte("aaa"),
		UpperBound: []byte("ddd"),
	}
	it := &memoryIteratorAdapter{storage: memStorage, opts: opts}

	// SeekToFirst
	assert.True(t, it.SeekToFirst())
	assert.True(t, it.Valid())
	assert.Equal(t, []byte("aaa"), it.Key())
	assert.Equal(t, []byte("v1"), it.Value())
	assert.NoError(t, it.Error())

	// Next
	assert.True(t, it.Next())
	assert.Equal(t, []byte("bbb"), it.Key())

	assert.True(t, it.Next())
	assert.Equal(t, []byte("ccc"), it.Key())

	// Next past end
	assert.False(t, it.Next())

	// SeekToLast
	assert.True(t, it.SeekToLast())
	assert.Equal(t, []byte("ccc"), it.Key())

	// Prev
	assert.True(t, it.Prev())
	assert.Equal(t, []byte("bbb"), it.Key())

	// Prev past start
	assert.True(t, it.Prev())
	assert.Equal(t, []byte("aaa"), it.Key())
	assert.False(t, it.Prev())

	// Close
	err = it.Close()
	assert.NoError(t, err)
	assert.Nil(t, it.results)
}

func TestMemoryIteratorAdapter_NilOpts(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	err := memStorage.Set([]byte("key1"), []byte("val1"))
	require.NoError(t, err)

	it := &memoryIteratorAdapter{storage: memStorage, opts: nil}

	// SeekToFirst with nil opts should still work
	assert.True(t, it.SeekToFirst())
	assert.True(t, it.Valid())
	assert.Equal(t, []byte("key1"), it.Key())
}

func TestMemoryIteratorAdapter_SeekToLast_NilOpts(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	err := memStorage.Set([]byte("x1"), []byte("v1"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("x2"), []byte("v2"))
	require.NoError(t, err)

	it := &memoryIteratorAdapter{storage: memStorage, opts: nil}

	assert.True(t, it.SeekToLast())
	assert.Equal(t, []byte("x2"), it.Key())
}

func TestMemoryIteratorAdapter_Seek(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	err := memStorage.Set([]byte("aaa"), []byte("v1"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("bbb"), []byte("v2"))
	require.NoError(t, err)
	err = memStorage.Set([]byte("ccc"), []byte("v3"))
	require.NoError(t, err)

	t.Run("SeekExact", func(t *testing.T) {
		it := &memoryIteratorAdapter{storage: memStorage, opts: nil}
		assert.True(t, it.Seek([]byte("bbb")))
		assert.Equal(t, []byte("bbb"), it.Key())
	})

	t.Run("SeekWithOpts", func(t *testing.T) {
		opts := &storage.IteratorOptions{
			UpperBound: []byte("ddd"),
		}
		it := &memoryIteratorAdapter{storage: memStorage, opts: opts}
		assert.True(t, it.Seek([]byte("aaa")))
		assert.Equal(t, []byte("aaa"), it.Key())
	})
}

func TestMemoryIteratorAdapter_InvalidPosition(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	it := &memoryIteratorAdapter{storage: memStorage, opts: nil}

	// Before any seek, position is invalid
	assert.False(t, it.Valid())
	assert.Nil(t, it.Key())
	assert.Nil(t, it.Value())
}

func TestMemoryIteratorAdapter_Empty(t *testing.T) {
	ensureTestLoggerInitialized(t)

	memStorage := memory.New(log)
	it := &memoryIteratorAdapter{storage: memStorage, opts: nil}

	// SeekToFirst on empty storage
	assert.False(t, it.SeekToFirst())
	assert.False(t, it.SeekToLast())
	assert.False(t, it.Seek([]byte("anything")))
}

// --------------------------------------------------------------------------
// memorySnapshotAdapter tests (covers Get, NewIterator, Close)
// --------------------------------------------------------------------------

func TestMemorySnapshotAdapter_Get(t *testing.T) {
	data := map[string][]byte{
		"snap-k1": []byte("snap-v1"),
		"snap-k2": []byte("snap-v2"),
	}
	snap := &memorySnapshotAdapter{data: data}

	val, err := snap.Get([]byte("snap-k1"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("snap-v1"), val)
}

func TestMemorySnapshotAdapter_Get_NotFound(t *testing.T) {
	data := map[string][]byte{}
	snap := &memorySnapshotAdapter{data: data}

	val, err := snap.Get([]byte("missing"))
	assert.Error(t, err)
	assert.Nil(t, val)
}

func TestMemorySnapshotAdapter_NewIterator(t *testing.T) {
	data := map[string][]byte{
		"k1": []byte("v1"),
		"k2": []byte("v2"),
	}
	snap := &memorySnapshotAdapter{data: data}

	iter := snap.NewIterator(nil)
	require.NotNil(t, iter)
	defer iter.Close()

	assert.True(t, iter.SeekToFirst())
	assert.True(t, iter.Valid())
}

func TestMemorySnapshotAdapter_Close(t *testing.T) {
	data := map[string][]byte{
		"k1": []byte("v1"),
	}
	snap := &memorySnapshotAdapter{data: data}

	err := snap.Close()
	assert.NoError(t, err)
	assert.Nil(t, snap.data)
}

// --------------------------------------------------------------------------
// snapshotIteratorAdapter tests (covers SeekToFirst, SeekToLast, Seek,
// Next, Prev, Valid, Key, Value, Error, Close)
// --------------------------------------------------------------------------

func TestSnapshotIteratorAdapter_Navigation(t *testing.T) {
	data := map[string][]byte{
		"alpha": []byte("v1"),
		"beta":  []byte("v2"),
		"gamma": []byte("v3"),
	}
	it := &snapshotIteratorAdapter{data: data}

	// SeekToFirst
	assert.True(t, it.SeekToFirst())
	assert.True(t, it.Valid())
	assert.NotNil(t, it.Key())
	assert.NotNil(t, it.Value())
	assert.NoError(t, it.Error())

	// Verify all keys are reachable
	keys := make(map[string]bool)
	keys[string(it.Key())] = true
	for it.Next() {
		keys[string(it.Key())] = true
	}
	assert.Len(t, keys, 3)
	assert.True(t, keys["alpha"])
	assert.True(t, keys["beta"])
	assert.True(t, keys["gamma"])

	// Next past end
	assert.False(t, it.Next())

	// SeekToLast
	assert.True(t, it.SeekToLast())
	assert.True(t, it.Valid())

	// Prev through all
	count := 1
	for it.Prev() {
		count++
	}
	assert.Equal(t, 3, count)

	// Prev past beginning
	assert.False(t, it.Prev())
}

func TestSnapshotIteratorAdapter_Seek(t *testing.T) {
	data := map[string][]byte{
		"aaa": []byte("1"),
		"bbb": []byte("2"),
		"ccc": []byte("3"),
	}

	t.Run("SeekExact", func(t *testing.T) {
		it := &snapshotIteratorAdapter{data: data}
		found := it.Seek([]byte("bbb"))
		assert.True(t, found)
		assert.True(t, it.Valid())
	})

	t.Run("SeekReturns", func(t *testing.T) {
		// Seek with a target past all keys returns false
		it := &snapshotIteratorAdapter{data: data}
		found := it.Seek([]byte("zzz"))
		// Seek returns false because no key >= "zzz" exists
		assert.False(t, found)
	})
}

func TestSnapshotIteratorAdapter_Empty(t *testing.T) {
	it := &snapshotIteratorAdapter{data: map[string][]byte{}}

	assert.False(t, it.SeekToFirst())
	assert.False(t, it.SeekToLast())
	assert.False(t, it.Seek([]byte("x")))
	assert.False(t, it.Valid())
	assert.Nil(t, it.Key())
	assert.Nil(t, it.Value())
	assert.NoError(t, it.Error())
}

func TestSnapshotIteratorAdapter_Close(t *testing.T) {
	data := map[string][]byte{
		"k1": []byte("v1"),
	}
	it := &snapshotIteratorAdapter{data: data}

	err := it.Close()
	assert.NoError(t, err)
	assert.Nil(t, it.keys)
}

// --------------------------------------------------------------------------
// createStorageFromURL edge cases
// --------------------------------------------------------------------------

func TestCreateStorageFromURL_InvalidScheme(t *testing.T) {
	ensureTestLoggerInitialized(t)

	_, err := createStorageFromURL("unknown://path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported storage scheme")
}

func TestCreateStorageFromURL_EmptyURL(t *testing.T) {
	ensureTestLoggerInitialized(t)

	_, err := createStorageFromURL("")
	assert.Error(t, err)
}
