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
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

type mockEmbeddingClient struct {
	GenerateEmbeddingFunc       func(ctx context.Context, req *embedding.GenerateEmbeddingRequest) (*embedding.GenerateEmbeddingResponse, error)
	BatchGenerateEmbeddingsFunc func(ctx context.Context, req *embedding.BatchGenerateEmbeddingsRequest) (*embedding.BatchGenerateEmbeddingsResponse, error)
	ListProvidersFunc           func(ctx context.Context, req *embedding.ListProvidersRequest) (*embedding.ListProvidersResponse, error)
	GetProviderInfoFunc         func(ctx context.Context, req *embedding.GetProviderInfoRequest) (*embedding.GetProviderInfoResponse, error)
	SetDefaultProviderFunc      func(ctx context.Context, req *embedding.SetDefaultProviderRequest) (*embedding.SetDefaultProviderResponse, error)
	GetCacheStatsFunc           func(ctx context.Context, req *embedding.GetEmbeddingCacheStatsRequest) (*embedding.GetEmbeddingCacheStatsResponse, error)
	ClearCacheFunc              func(ctx context.Context, req *embedding.ClearEmbeddingCacheRequest) (*embedding.ClearEmbeddingCacheResponse, error)
}

func (m *mockEmbeddingClient) GenerateEmbedding(ctx context.Context, req *embedding.GenerateEmbeddingRequest) (*embedding.GenerateEmbeddingResponse, error) {
	if m.GenerateEmbeddingFunc != nil {
		return m.GenerateEmbeddingFunc(ctx, req)
	}
	return &embedding.GenerateEmbeddingResponse{
		Embedding: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Metadata: &embedding.Metadata{
			Provider:       embedding.ProviderVoyage,
			Model:          "voyage-large-2",
			Dimensions:     1024,
			Tokens:         10,
			CacheHit:       false,
			ProcessingTime: 100 * time.Millisecond,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingClient) BatchGenerateEmbeddings(ctx context.Context, req *embedding.BatchGenerateEmbeddingsRequest) (*embedding.BatchGenerateEmbeddingsResponse, error) {
	if m.BatchGenerateEmbeddingsFunc != nil {
		return m.BatchGenerateEmbeddingsFunc(ctx, req)
	}
	results := make([]*embedding.BatchEmbeddingResult, len(req.Items))
	for i, item := range req.Items {
		results[i] = &embedding.BatchEmbeddingResult{
			ID:        item.ID,
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata: &embedding.Metadata{
				Provider:   embedding.ProviderVoyage,
				Dimensions: 1024,
				CacheHit:   false,
			},
		}
	}
	return &embedding.BatchGenerateEmbeddingsResponse{
		Results:        results,
		SuccessCount:   len(req.Items),
		ErrorCount:     0,
		TotalTokens:    50,
		ProcessingTime: 200 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockEmbeddingClient) ListProviders(ctx context.Context, req *embedding.ListProvidersRequest) (*embedding.ListProvidersResponse, error) {
	if m.ListProvidersFunc != nil {
		return m.ListProvidersFunc(ctx, req)
	}
	return &embedding.ListProvidersResponse{
		Providers: []*embedding.ProviderInfo{
			{
				Name:       "voyage",
				Type:       embedding.ProviderVoyage,
				Model:      "voyage-large-2",
				Dimensions: 1024,
				MaxTokens:  32000,
				Status:     embedding.StatusAvailable,
				IsDefault:  true,
			},
			{
				Name:       "openai",
				Type:       embedding.ProviderOpenAI,
				Model:      "text-embedding-3-large",
				Dimensions: 3072,
				MaxTokens:  8192,
				Status:     embedding.StatusAvailable,
				IsDefault:  false,
			},
		},
		DefaultProvider: "voyage",
		Timestamp:       time.Now(),
	}, nil
}

func (m *mockEmbeddingClient) GetProviderInfo(ctx context.Context, req *embedding.GetProviderInfoRequest) (*embedding.GetProviderInfoResponse, error) {
	if m.GetProviderInfoFunc != nil {
		return m.GetProviderInfoFunc(ctx, req)
	}
	return &embedding.GetProviderInfoResponse{
		Info: &embedding.ProviderInfo{
			Name:               string(req.Provider),
			Type:               req.Provider,
			Model:              "voyage-large-2",
			Dimensions:         1024,
			MaxTokens:          32000,
			MaxBatchSize:       128,
			Status:             embedding.StatusAvailable,
			APIURL:             "https://api.voyageai.com/v1",
			RateLimitPerMinute: 1000,
			Timeout:            30 * time.Second,
			IsDefault:          true,
			SuccessCount:       1000,
			ErrorCount:         5,
			AvgLatency:         50 * time.Millisecond,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingClient) SetDefaultProvider(ctx context.Context, req *embedding.SetDefaultProviderRequest) (*embedding.SetDefaultProviderResponse, error) {
	if m.SetDefaultProviderFunc != nil {
		return m.SetDefaultProviderFunc(ctx, req)
	}
	return &embedding.SetDefaultProviderResponse{
		Provider:         req.Provider,
		PreviousProvider: embedding.ProviderVoyage,
		Timestamp:        time.Now(),
	}, nil
}

func (m *mockEmbeddingClient) GetCacheStats(ctx context.Context, req *embedding.GetEmbeddingCacheStatsRequest) (*embedding.GetEmbeddingCacheStatsResponse, error) {
	if m.GetCacheStatsFunc != nil {
		return m.GetCacheStatsFunc(ctx, req)
	}
	return &embedding.GetEmbeddingCacheStatsResponse{
		Enabled:            true,
		Size:               500,
		Capacity:           10000,
		Hits:               4500,
		Misses:             500,
		HitRate:            0.90,
		Evictions:          50,
		MemoryBytes:        52428800,
		CompressionEnabled: true,
		CompressionRatio:   0.65,
		Timestamp:          time.Now(),
	}, nil
}

func (m *mockEmbeddingClient) ClearCache(ctx context.Context, req *embedding.ClearEmbeddingCacheRequest) (*embedding.ClearEmbeddingCacheResponse, error) {
	if m.ClearCacheFunc != nil {
		return m.ClearCacheFunc(ctx, req)
	}
	return &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: 500,
		Provider:       req.Provider,
		Timestamp:      time.Now(),
	}, nil
}

// mockSDKClientWithEmbedding wraps mockSDKClient with embedding support.
type mockSDKClientWithEmbedding struct {
	*mockSDKClient
	embeddingClient *mockEmbeddingClient
}

func (m *mockSDKClientWithEmbedding) Embedding() quicbaas.EmbeddingClient {
	if m.embeddingClient != nil {
		return m.embeddingClient
	}
	return &mockEmbeddingClient{}
}

// =============================================================================
// Command Structure Tests
// =============================================================================

func TestNewEmbeddingCommands(t *testing.T) {
	cmd := newEmbeddingCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "embedding", cmd.Use)
	assert.Contains(t, cmd.Aliases, "embed")
	assert.Contains(t, cmd.Aliases, "emb")
	assert.Contains(t, cmd.Short, "Embedding generation")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "generate", "generate command should be registered")
	assert.Contains(t, commandNames, "batch", "batch command should be registered")
	assert.Contains(t, commandNames, "provider", "provider command should be registered")
	assert.Contains(t, commandNames, "providers", "providers command should be registered")
	assert.Contains(t, commandNames, "cache", "cache command should be registered")

	tests := []struct {
		name  string
		cmd   *cobra.Command
		use   string
		short string
		flags []string
	}{
		{
			name:  "generate",
			cmd:   newEmbeddingGenerateCommand(),
			use:   "generate <text>",
			short: "Generate an embedding",
			flags: []string{"provider", "metadata", "input-type", "source", "language", "cache-bypass"},
		},
		{
			name:  "batch",
			cmd:   newEmbeddingBatchCommand(),
			use:   "batch <texts-json>",
			short: "Generate embeddings for multiple texts",
			flags: []string{"provider", "cache-bypass"},
		},
		{
			name:  "providers",
			cmd:   newEmbeddingProvidersCommand(),
			use:   "providers",
			short: "List all available embedding providers",
			flags: []string{"include-unavailable"},
		},
		{
			name:  "provider info",
			cmd:   newEmbeddingProviderInfoCommand(),
			use:   "info <name>",
			short: "Get detailed information",
		},
		{
			name:  "provider set-default",
			cmd:   newEmbeddingProviderSetDefaultCommand(),
			use:   "set-default <name>",
			short: "Set the default embedding provider",
		},
		{
			name:  "cache stats",
			cmd:   newEmbeddingCacheStatsCommand(),
			use:   "stats",
			short: "Get embedding cache statistics",
		},
		{
			name:  "cache clear",
			cmd:   newEmbeddingCacheClearCommand(),
			use:   "clear",
			short: "Clear the embedding cache",
			flags: []string{"provider"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.cmd)
			assert.Equal(t, tt.use, tt.cmd.Use)
			assert.Contains(t, tt.cmd.Short, tt.short)
			for _, flag := range tt.flags {
				assert.NotNil(t, tt.cmd.Flags().Lookup(flag), "flag --%s", flag)
			}
		})
	}
}

// =============================================================================
// Run Command Tests
// =============================================================================

func TestRunEmbeddingGenerateCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		embeddingProvider = ""
		embeddingMetadata = ""
		embeddingInputType = ""
		embeddingSource = ""
		embeddingLanguage = ""
		embeddingCacheBypass = false
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	tests := []struct {
		name     string
		metadata string
	}{
		{name: "without metadata"},
		{name: "with metadata", metadata: `{"doc_id":"123","type":"article"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embeddingMetadata = tt.metadata

			err := runEmbeddingGenerateCommand(&cobra.Command{}, []string{"Hello, world!"})
			assert.NoError(t, err)
		})
	}
}

func TestRunEmbeddingGenerateCommand_EmptyText(t *testing.T) {
	cmd := &cobra.Command{}
	err := runEmbeddingGenerateCommand(cmd, []string{""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "text cannot be empty")
}

func TestRunEmbeddingGenerateCommand_InvalidMetadata(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	defer func() {
		embeddingMetadata = ""
	}()
	embeddingMetadata = "invalid-json{"

	cmd := &cobra.Command{}
	err := runEmbeddingGenerateCommand(cmd, []string{"test text"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid metadata JSON")
}

func TestRunEmbeddingCommands_Errors(t *testing.T) {
	// A nil embed means the SDK client factory itself fails; otherwise the
	// embedding service returns the error.
	tests := []struct {
		name  string
		embed *mockEmbeddingClient
		run   func() error
		want  string
	}{
		{
			name: "generate client error",
			run:  func() error { return runEmbeddingGenerateCommand(&cobra.Command{}, []string{"test text"}) },
			want: "connection refused",
		},
		{
			name: "generate service error",
			embed: &mockEmbeddingClient{
				GenerateEmbeddingFunc: func(ctx context.Context, req *embedding.GenerateEmbeddingRequest) (*embedding.GenerateEmbeddingResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "embedding.generate", "service unavailable")
				},
			},
			run:  func() error { return runEmbeddingGenerateCommand(&cobra.Command{}, []string{"test text"}) },
			want: "service unavailable",
		},
		{
			name: "batch client error",
			run: func() error {
				return runBatchGenerate([]*embedding.BatchEmbeddingItem{{ID: "item-1", Text: "test text"}})
			},
			want: "connection refused",
		},
		{
			name: "batch service error",
			embed: &mockEmbeddingClient{
				BatchGenerateEmbeddingsFunc: func(ctx context.Context, req *embedding.BatchGenerateEmbeddingsRequest) (*embedding.BatchGenerateEmbeddingsResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "embedding.batch", "service unavailable")
				},
			},
			run:  func() error { return runEmbeddingBatchCommand(&cobra.Command{}, []string{`["text1", "text2"]`}) },
			want: "service unavailable",
		},
		{
			name: "providers client error",
			run:  func() error { return runEmbeddingProvidersCommand(&cobra.Command{}, []string{}) },
			want: "connection refused",
		},
		{
			name: "providers service error",
			embed: &mockEmbeddingClient{
				ListProvidersFunc: func(ctx context.Context, req *embedding.ListProvidersRequest) (*embedding.ListProvidersResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "embedding.providers", "service unavailable")
				},
			},
			run:  func() error { return runEmbeddingProvidersCommand(&cobra.Command{}, []string{}) },
			want: "service unavailable",
		},
		{
			name: "provider info client error",
			run:  func() error { return runEmbeddingProviderInfoCommand(&cobra.Command{}, []string{"voyage"}) },
			want: "connection refused",
		},
		{
			name: "provider info service error",
			embed: &mockEmbeddingClient{
				GetProviderInfoFunc: func(ctx context.Context, req *embedding.GetProviderInfoRequest) (*embedding.GetProviderInfoResponse, error) {
					return nil, errors.Newf(errors.ErrNotFound, "embedding.provider", "provider not found")
				},
			},
			run:  func() error { return runEmbeddingProviderInfoCommand(&cobra.Command{}, []string{"unknown"}) },
			want: "provider not found",
		},
		{
			name: "set default client error",
			run:  func() error { return runEmbeddingProviderSetDefaultCommand(&cobra.Command{}, []string{"openai"}) },
			want: "connection refused",
		},
		{
			name: "set default service error",
			embed: &mockEmbeddingClient{
				SetDefaultProviderFunc: func(ctx context.Context, req *embedding.SetDefaultProviderRequest) (*embedding.SetDefaultProviderResponse, error) {
					return nil, errors.Newf(errors.ErrNotFound, "embedding.setDefault", "provider not found")
				},
			},
			run:  func() error { return runEmbeddingProviderSetDefaultCommand(&cobra.Command{}, []string{"unknown"}) },
			want: "provider not found",
		},
		{
			name: "cache stats client error",
			run:  func() error { return runEmbeddingCacheStatsCommand(&cobra.Command{}, []string{}) },
			want: "connection refused",
		},
		{
			name: "cache stats service error",
			embed: &mockEmbeddingClient{
				GetCacheStatsFunc: func(ctx context.Context, req *embedding.GetEmbeddingCacheStatsRequest) (*embedding.GetEmbeddingCacheStatsResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "embedding.cacheStats", "service unavailable")
				},
			},
			run:  func() error { return runEmbeddingCacheStatsCommand(&cobra.Command{}, []string{}) },
			want: "service unavailable",
		},
		{
			name: "cache clear client error",
			run:  func() error { return runEmbeddingCacheClearCommand(&cobra.Command{}, []string{}) },
			want: "connection refused",
		},
		{
			name: "cache clear service error",
			embed: &mockEmbeddingClient{
				ClearCacheFunc: func(ctx context.Context, req *embedding.ClearEmbeddingCacheRequest) (*embedding.ClearEmbeddingCacheResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "embedding.cacheClear", "service unavailable")
				},
			},
			run:  func() error { return runEmbeddingCacheClearCommand(&cobra.Command{}, []string{}) },
			want: "service unavailable",
		},
	}

	origFactory := sdkClientFactory
	oldTimeout := globalTimeout
	defer func() {
		sdkClientFactory = origFactory
		globalTimeout = oldTimeout
	}()
	globalTimeout = 30 * time.Second

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdkClientFactory = func() (quicbaas.Client, error) {
				if tt.embed == nil {
					return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
				}
				return &mockSDKClientWithEmbedding{
					mockSDKClient:   &mockSDKClient{},
					embeddingClient: tt.embed,
				}, nil
			}

			err := tt.run()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRunEmbeddingBatchCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		embeddingProvider = ""
		embeddingCacheBypass = false
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runEmbeddingBatchCommand(cmd, []string{`["text1", "text2", "text3"]`})
	assert.NoError(t, err)
}

func TestRunEmbeddingBatchCommand_BatchItems(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runEmbeddingBatchCommand(cmd, []string{`[{"id":"doc1","text":"Hello"},{"id":"doc2","text":"World"}]`})
	assert.NoError(t, err)
}

func TestRunEmbeddingBatchCommand_InvalidJSON(t *testing.T) {
	cmd := &cobra.Command{}
	err := runEmbeddingBatchCommand(cmd, []string{`invalid-json{`})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON input")
}

func TestRunBatchGenerate_EmptyItems(t *testing.T) {
	err := runBatchGenerate([]*embedding.BatchEmbeddingItem{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one text item is required")
}

func TestRunEmbeddingProvidersCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		embeddingIncludeUnavailable = false
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runEmbeddingProvidersCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunEmbeddingProviderInfoCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runEmbeddingProviderInfoCommand(cmd, []string{"voyage"})
	assert.NoError(t, err)
}

func TestRunEmbeddingProviderSetDefaultCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runEmbeddingProviderSetDefaultCommand(cmd, []string{"openai"})
	assert.NoError(t, err)
}

func TestRunEmbeddingCacheStatsCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	cmd := &cobra.Command{}
	err := runEmbeddingCacheStatsCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunEmbeddingCacheClearCommand_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	// Reset globals
	oldFormat := outputFormat
	oldTimeout := globalTimeout
	defer func() {
		outputFormat = oldFormat
		globalTimeout = oldTimeout
		embeddingProvider = ""
	}()
	outputFormat = "json"
	globalTimeout = 30 * time.Second

	tests := []struct {
		name     string
		provider string
	}{
		{name: "entire cache"},
		{name: "one provider", provider: "voyage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embeddingProvider = tt.provider

			err := runEmbeddingCacheClearCommand(&cobra.Command{}, []string{})
			assert.NoError(t, err)
		})
	}
}

// =============================================================================
// Format Response Tests
// =============================================================================

func TestFormatEmbeddingGenerateResponse_AllFormats(t *testing.T) {
	resp := &embedding.GenerateEmbeddingResponse{
		Embedding: []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
		Metadata: &embedding.Metadata{
			Provider:       embedding.ProviderVoyage,
			Model:          "voyage-large-2",
			Dimensions:     1024,
			Tokens:         10,
			CacheHit:       true,
			ProcessingTime: 50 * time.Millisecond,
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		wantErr  bool
		contains []string
		resp     *embedding.GenerateEmbeddingResponse // nil uses resp above
	}{
		{
			name:     "json_format",
			format:   "json",
			wantErr:  false,
			contains: []string{"embedding", "0.1"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			wantErr:  false,
			contains: []string{"embedding"},
		},
		{
			name:     "table_format",
			format:   "table",
			wantErr:  false,
			contains: []string{"EMBEDDING GENERATED", "Provider:", "Dimensions:"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			wantErr:  false,
			contains: []string{"0.1"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
		{
			// Fewer values than the table previews.
			name:   "table_short_embedding",
			format: "table",
			resp: &embedding.GenerateEmbeddingResponse{
				Embedding: []float32{0.1, 0.2, 0.3},
				Metadata: &embedding.Metadata{
					Provider:       embedding.ProviderVoyage,
					Model:          "voyage-large-2",
					Dimensions:     3,
					Tokens:         5,
					ProcessingTime: 10 * time.Millisecond,
				},
				Timestamp: time.Now(),
			},
		},
		{
			name:   "table_nil_metadata",
			format: "table",
			resp: &embedding.GenerateEmbeddingResponse{
				Embedding: []float32{0.1, 0.2, 0.3},
				Timestamp: time.Now(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			in := resp
			if tc.resp != nil {
				in = tc.resp
			}

			err := formatEmbeddingGenerateResponse(in)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatEmbeddingBatchResponse_AllFormats(t *testing.T) {
	resp := &embedding.BatchGenerateEmbeddingsResponse{
		Results: []*embedding.BatchEmbeddingResult{
			{
				ID:        "item-1",
				Embedding: []float32{0.1, 0.2, 0.3},
				Metadata: &embedding.Metadata{
					CacheHit: true,
				},
			},
			{
				ID:        "item-2",
				Embedding: []float32{0.4, 0.5, 0.6},
				Error:     "processing error",
			},
		},
		SuccessCount:   1,
		ErrorCount:     1,
		TotalTokens:    20,
		ProcessingTime: 150 * time.Millisecond,
		Timestamp:      time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		wantErr  bool
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			wantErr:  false,
			contains: []string{"results", "item-1"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			wantErr:  false,
			contains: []string{"results"},
		},
		{
			name:     "table_format",
			format:   "table",
			wantErr:  false,
			contains: []string{"BATCH EMBEDDING RESULTS", "Success Count:", "Error Count:"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatEmbeddingBatchResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatEmbeddingProvidersResponse_AllFormats(t *testing.T) {
	resp := &embedding.ListProvidersResponse{
		Providers: []*embedding.ProviderInfo{
			{
				Name:       "voyage",
				Type:       embedding.ProviderVoyage,
				Model:      "voyage-large-2",
				Dimensions: 1024,
				MaxTokens:  32000,
				Status:     embedding.StatusAvailable,
				IsDefault:  true,
			},
		},
		DefaultProvider: "voyage",
		Timestamp:       time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		wantErr  bool
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			wantErr:  false,
			contains: []string{"providers", "voyage"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			wantErr:  false,
			contains: []string{"providers"},
		},
		{
			name:     "table_format",
			format:   "table",
			wantErr:  false,
			contains: []string{"EMBEDDING PROVIDERS", "Default Provider:"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatEmbeddingProvidersResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatEmbeddingProviderInfoResponse_AllFormats(t *testing.T) {
	now := time.Now()
	resp := &embedding.GetProviderInfoResponse{
		Info: &embedding.ProviderInfo{
			Name:               "voyage",
			Type:               embedding.ProviderVoyage,
			Model:              "voyage-large-2",
			Dimensions:         1024,
			MaxTokens:          32000,
			MaxBatchSize:       128,
			Status:             embedding.StatusAvailable,
			APIURL:             "https://api.voyageai.com/v1",
			RateLimitPerMinute: 1000,
			Timeout:            30 * time.Second,
			IsDefault:          true,
			LastError:          "test error",
			LastErrorTime:      &now,
			SuccessCount:       1000,
			ErrorCount:         5,
			AvgLatency:         50 * time.Millisecond,
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		wantErr  bool
		contains []string
		resp     *embedding.GetProviderInfoResponse // nil uses resp above
	}{
		{
			name:     "json_format",
			format:   "json",
			wantErr:  false,
			contains: []string{"info", "voyage"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			wantErr:  false,
			contains: []string{"info"},
		},
		{
			name:     "table_format",
			format:   "table",
			wantErr:  false,
			contains: []string{"PROVIDER INFORMATION", "Name:", "Model:"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
		{
			name:   "table_no_rate_limit_no_last_error",
			format: "table",
			resp: &embedding.GetProviderInfoResponse{
				Info: &embedding.ProviderInfo{
					Name:         "voyage",
					Type:         embedding.ProviderVoyage,
					Model:        "voyage-large-2",
					Dimensions:   1024,
					MaxTokens:    32000,
					MaxBatchSize: 128,
					Status:       embedding.StatusAvailable,
					APIURL:       "https://api.voyageai.com/v1",
					Timeout:      30 * time.Second,
					IsDefault:    true,
					SuccessCount: 1000,
					AvgLatency:   50 * time.Millisecond,
				},
				Timestamp: time.Now(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			in := resp
			if tc.resp != nil {
				in = tc.resp
			}

			err := formatEmbeddingProviderInfoResponse(in)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatEmbeddingSetDefaultResponse_AllFormats(t *testing.T) {
	resp := &embedding.SetDefaultProviderResponse{
		Provider:         embedding.ProviderOpenAI,
		PreviousProvider: embedding.ProviderVoyage,
		Timestamp:        time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		wantErr  bool
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			wantErr:  false,
			contains: []string{"provider", "openai"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			wantErr:  false,
			contains: []string{"provider"},
		},
		{
			name:     "table_format",
			format:   "table",
			wantErr:  false,
			contains: []string{"Default provider changed"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			err := formatEmbeddingSetDefaultResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatEmbeddingCacheStatsResponse_AllFormats(t *testing.T) {
	resp := &embedding.GetEmbeddingCacheStatsResponse{
		Enabled:            true,
		Size:               500,
		Capacity:           10000,
		Hits:               4500,
		Misses:             500,
		HitRate:            0.90,
		Evictions:          50,
		MemoryBytes:        52428800,
		CompressionEnabled: true,
		CompressionRatio:   0.65,
		Timestamp:          time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		wantErr  bool
		contains []string
		resp     *embedding.GetEmbeddingCacheStatsResponse // nil uses resp above
	}{
		{
			name:     "json_format",
			format:   "json",
			wantErr:  false,
			contains: []string{"enabled", "true"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			wantErr:  false,
			contains: []string{"enabled"},
		},
		{
			name:     "table_format",
			format:   "table",
			wantErr:  false,
			contains: []string{"EMBEDDING CACHE STATISTICS", "Enabled:", "Size:"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
		{
			name:   "table_no_memory_no_compression",
			format: "table",
			resp: &embedding.GetEmbeddingCacheStatsResponse{
				Enabled:   true,
				Size:      500,
				Capacity:  10000,
				Hits:      4500,
				Misses:    500,
				HitRate:   0.90,
				Evictions: 50,
				Timestamp: time.Now(),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			in := resp
			if tc.resp != nil {
				in = tc.resp
			}

			err := formatEmbeddingCacheStatsResponse(in)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatEmbeddingCacheClearResponse_AllFormats(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		provider string
		wantErr  bool
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			provider: "",
			wantErr:  false,
			contains: []string{"entries_cleared"},
		},
		{
			name:     "yaml_format",
			format:   "yaml",
			provider: "",
			wantErr:  false,
			contains: []string{"entriescleared"},
		},
		{
			name:     "table_format_all",
			format:   "table",
			provider: "",
			wantErr:  false,
			contains: []string{"Entire cache cleared"},
		},
		{
			name:     "table_format_provider",
			format:   "table",
			provider: "voyage",
			wantErr:  false,
			contains: []string{"Cache cleared for provider"},
		},
		{
			name:    "invalid_format",
			format:  "invalid",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outputFormat = tc.format
			defer func() { outputFormat = "" }()

			resp := &embedding.ClearEmbeddingCacheResponse{
				EntriesCleared: 500,
				Provider:       embedding.Provider(tc.provider),
				Timestamp:      time.Now(),
			}

			err := formatEmbeddingCacheClearResponse(resp)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Command Help Tests
// =============================================================================

// =============================================================================
// Error Path Tests
// =============================================================================

// =============================================================================
// createEmbeddingClient Tests
// =============================================================================

func TestCreateEmbeddingClient_Success(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	mockEmbed := &mockEmbeddingClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientWithEmbedding{
			mockSDKClient:   &mockSDKClient{},
			embeddingClient: mockEmbed,
		}, nil
	}

	client, err := createEmbeddingClient()
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestCreateEmbeddingClient_FactoryError(t *testing.T) {
	// Save original factory and restore after test
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	client, err := createEmbeddingClient()
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "connection refused")
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================
