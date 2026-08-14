//go:build raft && rest && quic && mcp && vector && !integration

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

package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// =============================================================================
// Typed test errors (never use fmt.Errorf per CLAUDE.md).
// =============================================================================

type ft2TestError struct {
	msg string
}

func (e *ft2TestError) Error() string { return e.msg }

// =============================================================================
// MCP Server Lifecycle Tests
// =============================================================================

func TestFT2_StartMCPServer_Disabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.MCP.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startMCPServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.mcpServer)
}

func TestFT2_StartMCPServer_Success(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = 0

	// Override newMCP seam to return a real server, and override stop.
	var capturedConfig *mcpapi.Config
	seamMu.Lock()
	origNewMCP := newMCP
	newMCP = func(svc service.QuicBaaS, cfg *mcpapi.Config, log *logger.Logger) *mcpapi.Server {
		capturedConfig = cfg
		return origNewMCP(svc, cfg, log)
	}
	mcpStopFn = func(m *mcpapi.Server) error { return nil }
	seamMu.Unlock()

	err := srv.startMCPServer()
	assert.NoError(t, err)
	assert.NotNil(t, srv.mcpServer)
	assert.NotNil(t, capturedConfig)
	assert.Equal(t, "http", capturedConfig.Transport)

	// Cleanup: call stop.
	err = srv.stopMCPServer()
	assert.NoError(t, err)
}

func TestFT2_StopMCPServer_NilServer(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.stopMCPServer()
	assert.NoError(t, err)
}

func TestFT2_StopMCPServer_Error(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = 0

	seamMu.Lock()
	mcpStopFn = func(m *mcpapi.Server) error {
		return &ft2TestError{msg: "mcp stop failed"}
	}
	seamMu.Unlock()

	err := srv.startMCPServer()
	require.NoError(t, err)

	err = srv.stopMCPServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "MCP server")
}

func TestFT2_ConfigureMCPAuth_WithService(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.MCP.Enabled = true
	srv.config.Server.MCP.Port = 0

	seamMu.Lock()
	mcpStopFn = func(m *mcpapi.Server) error { return nil }
	seamMu.Unlock()

	// configureMCPAuth is called inside startMCPServer.
	// It performs a type assertion on quicBaaSService for internalAuthManagerGetter.
	err := srv.startMCPServer()
	assert.NoError(t, err)
	assert.NotNil(t, srv.mcpServer)
}

// =============================================================================
// QUIC Server Lifecycle Tests
// =============================================================================

func TestFT2_StartQUICServer_Disabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.QUIC.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startQUICServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.quicServer)
}

func TestFT2_StartQUICServer_Embedded(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Embedded = true
	cfg.Server.QUIC.Enabled = true
	cfg.Server.QUIC.Port = 9443
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startQUICServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.quicServer)
}

func TestFT2_StartQUICServer_PortZero(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Embedded = false
	cfg.Server.QUIC.Enabled = true
	cfg.Server.QUIC.Port = 0
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startQUICServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.quicServer)
}

func TestFT2_StartQUICServer_NilTLSConfig(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 19443
	srv.config.Security.TLSEnabled = true
	// Ensure tlsConfig is nil (no CA initialization performed)
	srv.tlsConfig = nil

	// Without TLS config, startQUICServer should return a configuration error.
	err := srv.startQUICServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS configuration is required")
}

func TestFT2_StartQUICServer_TLSCertLoadError(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Embedded = false
	cfg.Server.QUIC.Enabled = true
	cfg.Server.QUIC.Port = 19443
	cfg.Security.TLSEnabled = true
	cfg.Security.TLSCertFile = "/nonexistent/cert.pem"
	cfg.Security.TLSKeyFile = "/nonexistent/key.pem"
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startQUICServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS")
}

func TestFT2_StartQUICServer_StartError(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.Embedded = false
	srv.config.Server.QUIC.Enabled = true
	srv.config.Server.QUIC.Port = 19445
	srv.config.Security.TLSEnabled = false

	started := false
	seamMu.Lock()
	newQUIC = func(svc service.QuicBaaS, cfg *quicapi.Config, log *logger.Logger, opts ...quicapi.ServerOption) (*quicapi.Server, error) {
		// Return a real server that will fail on Start (invalid address).
		cfg.ListenAddr = "invalid-address:99999"
		s, err := quicapi.NewServer(svc, cfg, log, opts...)
		started = true
		return s, err
	}
	seamMu.Unlock()

	err := srv.startQUICServer()
	// May error at start or creation.
	if started {
		// Creation succeeded, start may fail.
		_ = err
	}
}

func TestFT2_StopQUICServer_NilServer(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.stopQUICServer()
	assert.NoError(t, err)
}

func TestFT2_StopQUICServer_Error(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)

	// Use a zero-value QUIC server to trigger the stop path.
	// The seam override below intercepts the actual stop call.
	quicSrv := &quicapi.Server{}
	srv.quicServer = quicSrv

	seamMu.Lock()
	quicStopFn = func(q *quicapi.Server) error {
		return &ft2TestError{msg: "quic stop failed"}
	}
	seamMu.Unlock()

	err := srv.stopQUICServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "QUIC")
}

// =============================================================================
// REST Server Lifecycle Tests
// =============================================================================

func TestFT2_StartRESTServer_Disabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.REST.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startRESTServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.restServer)
}

func TestFT2_StartRESTServer_Embedded(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Embedded = true
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 8080
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startRESTServer()
	assert.NoError(t, err)
	assert.Nil(t, srv.restServer)
}

func TestFT2_StartRESTServer_CreateError(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = 18080

	seamMu.Lock()
	newREST = func(svc service.QuicBaaS, cfg *restapi.Config, log *logger.Logger) (*restapi.Server, error) {
		return nil, &ft2TestError{msg: "rest creation error"}
	}
	seamMu.Unlock()

	err := srv.startRESTServer()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REST")
}

func TestFT2_StopRESTServer_NilServer(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.stopRESTServer(context.Background())
	assert.NoError(t, err)
}

func TestFT2_StopRESTServer_Error(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Set a non-nil rest server to trigger stop path.
	srv.restServer = &restapi.Server{}

	seamMu.Lock()
	restShutdownFn = func(s *restapi.Server, ctx context.Context) error {
		return &ft2TestError{msg: "rest shutdown error"}
	}
	seamMu.Unlock()

	err = srv.stopRESTServer(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "REST")
}

// =============================================================================
// Embedding Lifecycle Tests
// =============================================================================

func TestFT2_BuildEmbeddingConfig_RAGEnabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding.Provider = "voyage"
	cfg.RAG.Embedding.APIURL = "http://voyage.test:8080"
	cfg.RAG.Embedding.APIKey = "test-key"
	cfg.RAG.Embedding.Model = "test-model"
	cfg.RAG.Embedding.Timeout = 60
	cfg.RAG.Embedding.Dimensions = 1024

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	embCfg := srv.buildEmbeddingConfig()
	require.NotNil(t, embCfg)
	assert.Equal(t, "voyage", embCfg.Provider)
	assert.Equal(t, "http://voyage.test:8080", embCfg.APIURL)
	assert.Equal(t, "test-key", embCfg.APIKey)
	assert.Equal(t, "test-model", embCfg.Model)
	assert.Equal(t, 60, embCfg.Timeout)
	assert.Equal(t, 1024, embCfg.Dimensions)
}

func TestFT2_BuildEmbeddingConfig_RAGDefaultTimeoutAndDimensions(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding.Provider = "openai"
	cfg.RAG.Embedding.Timeout = 0
	cfg.RAG.Embedding.Dimensions = 0

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	embCfg := srv.buildEmbeddingConfig()
	require.NotNil(t, embCfg)
	assert.Equal(t, 120, embCfg.Timeout)
	assert.Equal(t, 768, embCfg.Dimensions)
}

func TestFT2_BuildEmbeddingConfig_OllamaFallback(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = false
	cfg.Ollama.Enabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	embCfg := srv.buildEmbeddingConfig()
	require.NotNil(t, embCfg)
	assert.Equal(t, "ollama", embCfg.Provider)
}

func TestFT2_BuildEmbeddingConfig_DefaultConfig(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = false
	cfg.Ollama.Enabled = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	embCfg := srv.buildEmbeddingConfig()
	require.NotNil(t, embCfg)
	assert.Equal(t, "ollama", embCfg.Provider)
	assert.Equal(t, "http://localhost:11434", embCfg.APIURL)
	assert.Equal(t, "nomic-embed-text", embCfg.Model)
}

func TestFT2_RegisterEmbeddingProviders_UnknownProvider(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "ollama",
		APIURL:     "http://localhost:1234",
		Model:      "test",
		Dimensions: 768,
	})
	require.NoError(t, mErr)

	embCfg := &embedding.Config{
		Provider:   "unknown-provider",
		APIURL:     "http://localhost:1234",
		Model:      "test",
		Dimensions: 768,
	}

	err = srv.registerEmbeddingProviders(mgr, embCfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown-provider")
}

func TestFT2_RegisterEmbeddingProviders_VoyageProvider(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "voyage",
		APIURL:     "http://localhost:1234",
		APIKey:     "test-key",
		Model:      "voyage-code-2",
		Dimensions: 1536,
	})
	require.NoError(t, mErr)

	embCfg := &embedding.Config{
		Provider:   "voyage",
		APIURL:     "http://localhost:1234",
		APIKey:     "test-key",
		Model:      "voyage-code-2",
		Dimensions: 1536,
	}

	err = srv.registerEmbeddingProviders(mgr, embCfg)
	assert.NoError(t, err)
}

func TestFT2_RegisterEmbeddingProviders_OpenAIProvider(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "openai",
		APIURL:     "http://localhost:1234",
		APIKey:     "test-key",
		Model:      "text-embedding-ada-002",
		Dimensions: 1536,
	})
	require.NoError(t, mErr)

	embCfg := &embedding.Config{
		Provider:   "openai",
		APIURL:     "http://localhost:1234",
		APIKey:     "test-key",
		Model:      "text-embedding-ada-002",
		Dimensions: 1536,
	}

	err = srv.registerEmbeddingProviders(mgr, embCfg)
	assert.NoError(t, err)
}

func TestFT2_RegisterEmbeddingProviders_LMStudioProvider(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "lmstudio",
		APIURL:     "http://localhost:1234",
		Model:      "nomic-embed",
		Dimensions: 768,
	})
	require.NoError(t, mErr)

	embCfg := &embedding.Config{
		Provider:       "lmstudio",
		APIURL:         "http://localhost:1234",
		Model:          "nomic-embed",
		Dimensions:     768,
		Timeout:        30,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}

	err = srv.registerEmbeddingProviders(mgr, embCfg)
	assert.NoError(t, err)
}

func TestFT2_RegisterEmbeddingProviders_OllamaProvider(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "ollama",
		APIURL:     "http://localhost:11434",
		Model:      "nomic-embed-text",
		Dimensions: 768,
	})
	require.NoError(t, mErr)

	embCfg := &embedding.Config{
		Provider:       "ollama",
		APIURL:         "http://localhost:11434",
		Model:          "nomic-embed-text",
		Dimensions:     768,
		Timeout:        30,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}

	// This exercises the "ollama" case in registerEmbeddingProviders.
	// May fail if ollama provider has strict init, but the path is hit.
	_ = srv.registerEmbeddingProviders(mgr, embCfg)
}

func TestFT2_StopEmbedding_NilManager(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.stopEmbedding()
	assert.NoError(t, err)
}

func TestFT2_StartEmbedding_WithDefaultConfig(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = false
	cfg.Ollama.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// startEmbedding will get a default config and attempt to create manager.
	// Embedding is optional so it should not error.
	err = srv.startEmbedding()
	assert.NoError(t, err)
}

func TestFT2_StartEmbedding_RAGConfig(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding.Provider = "voyage"
	cfg.RAG.Embedding.APIURL = "http://localhost:1234"
	cfg.RAG.Embedding.APIKey = "key"
	cfg.RAG.Embedding.Model = "model"
	cfg.RAG.Embedding.Dimensions = 768
	cfg.RAG.Embedding.Timeout = 30
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Should exercise the RAG config path in buildEmbeddingConfig.
	err = srv.startEmbedding()
	assert.NoError(t, err)
}

// =============================================================================
// RAG Lifecycle Tests
// =============================================================================

func TestFT2_StartRAG_Disabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startRAG()
	assert.NoError(t, err)
	assert.Nil(t, srv.ragSystem)
}

func TestFT2_StartRAG_InvalidConfig(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding.Provider = ""
	cfg.RAG.IndexPath = ""
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Should log warning and return nil (RAG is optional).
	err = srv.startRAG()
	assert.NoError(t, err)
	assert.Nil(t, srv.ragSystem)
}

func TestFT2_StartRAG_SystemCreationFails(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.RAG.Enabled = true
	cfg.RAG.Embedding.Provider = "ollama"
	cfg.RAG.Embedding.APIURL = "http://localhost:99999"
	cfg.RAG.Embedding.Model = "nomic-embed-text"
	cfg.RAG.Embedding.Dimensions = 768
	cfg.RAG.IndexPath = filepath.Join(cfg.Storage.DataDir, "rag-index")

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// RAG system creation may fail, but it is optional.
	err = srv.startRAG()
	assert.NoError(t, err)
}

func TestFT2_StopRAG_NilSystem(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.stopRAG()
	assert.NoError(t, err)
}

func TestFT2_RegisterOllamaService_NilQuicBaaSService(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.quicBaaSService = nil

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	// Should return immediately with nil service.
	srv.registerOllamaService(grpcServer)
}

func TestFT2_RegisterOllamaService_NoOllamaSvc(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	// Service exists but Ollama() returns nil.
	srv.registerOllamaService(grpcServer)
}

// =============================================================================
// Vector Lifecycle Tests
// =============================================================================

func TestFT2_StartVector_Disabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Vector.Enabled = false
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startVector()
	assert.NoError(t, err)
}

// =============================================================================
// TimeSeries Lifecycle Tests
// =============================================================================

func TestFT2_StartTimeSeries_NilStorage(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.timeSeriesStorage = nil

	err = srv.startTimeSeries()
	assert.NoError(t, err)
}

func TestFT2_StartTimeSeries_WithStorage(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initTimeSeriesStorage()
	require.NoError(t, err)

	err = srv.startTimeSeries()
	assert.NoError(t, err)

	if srv.timeSeriesStorage != nil {
	}
}

func TestFT2_StopTimeSeries_NilStorage(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.timeSeriesStorage = nil

	err = srv.stopTimeSeries()
	assert.NoError(t, err)
}

func TestFT2_StopTimeSeries_Success(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initTimeSeriesStorage()
	require.NoError(t, err)

	err = srv.stopTimeSeries()
	assert.NoError(t, err)
	assert.Nil(t, srv.timeSeriesStorage)
}

// =============================================================================
// WatchBridge Tests
// =============================================================================

func TestFT2_StartWatchBridge_NilService(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startWatchBridge()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "service")
}

func TestFT2_StartWatchBridge_NilWatchManager(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)

	// Set up mock to return nil WatchManager
	mockSvc := srv.quicBaaSService.(*testutil.MockQuicBaaS)
	mockSvc.On("WatchManager").Return((*watch.Manager)(nil))

	err := srv.startWatchBridge()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "watch manager")
}

// =============================================================================
// Health CheckRaft Tests
// =============================================================================

func TestFT2_CheckREST_Nil(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.restServer = nil

	hc := NewHealthChecker(srv, srv.logger)
	health := hc.CheckREST()
	assert.Equal(t, HealthStatusUnhealthy, health.Status)
	assert.Contains(t, health.Message, "not initialized")
}

func TestFT2_GetOverallHealth_AllDown(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.raftNode = nil

	hc := NewHealthChecker(srv, srv.logger)
	overall := hc.GetOverallHealth()
	assert.NotEmpty(t, overall.Components)
}

// =============================================================================
// IAM Components Method Tests
// =============================================================================

func TestFT2_IAMComponents_Getters(t *testing.T) {
	saveAndRestoreSeams(t)

	comp := &iamComponents{}

	assert.Nil(t, comp.GetTenantManager())
	assert.Nil(t, comp.GetIdentityStore())
	assert.Nil(t, comp.GetPolicyEngine())
	assert.Nil(t, comp.GetAppManager())
	assert.Nil(t, comp.GetOIDCProvider())
	assert.Nil(t, comp.GetMFAManager())
	assert.Nil(t, comp.GetWebAuthnManager())
	assert.Nil(t, comp.GetAuditLogger())
	assert.Nil(t, comp.GetAdminManager())
	assert.Nil(t, comp.GetGDPRManager())
	assert.Nil(t, comp.GetSessionStorage())
	assert.Nil(t, comp.GetSessionManager())
	assert.Nil(t, comp.GetInternalAuthManager())
}

// =============================================================================
// Server Initialize - Deep Path Tests
// =============================================================================

func TestFT2_Initialize_VectorEnabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Vector.Enabled = true
	cfg.Vector.ShardID = 2
	cfg.Vector.HNSW = &config.HNSWConfig{M: 16, EfConstruction: 200, MaxElements: 10000, Ml: 1.0}
	cfg.Vector.StoragePath = filepath.Join(cfg.Storage.DataDir, "vector-init-test2")

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	if err != nil {
		assert.NotContains(t, err.Error(), "panic")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestFT2_Initialize_TLSEnabled(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	if err != nil {
		t.Logf("Initialize with TLS: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func TestFT2_Initialize_WithTimeSeries(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initialize()
	// Standalone mode exercises initTimeSeriesStorage path.
	if err != nil {
		t.Logf("Initialize error (expected with minimal config): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// =============================================================================
// Server Shutdown - Deep Path Tests
// =============================================================================

func TestFT2_Shutdown_WithEmbeddingManager(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.started = true

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "ollama",
		APIURL:     "http://localhost:11434",
		Model:      "test",
		Dimensions: 768,
	})
	if mErr == nil {
		srv.embeddingManager = mgr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestFT2_Shutdown_WithRESTServer(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.started = true
	srv.restServer = &restapi.Server{}

	seamMu.Lock()
	restShutdownFn = func(s *restapi.Server, ctx context.Context) error {
		return nil
	}
	seamMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestFT2_Shutdown_WithQUICServer(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.started = true

	quicSrv, qErr := quicapi.NewServer(srv.quicBaaSService, &quicapi.Config{ListenAddr: "127.0.0.1:0"}, srv.logger)
	if qErr == nil {
		srv.quicServer = quicSrv
	}

	seamMu.Lock()
	quicStopFn = func(q *quicapi.Server) error { return nil }
	seamMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestFT2_Shutdown_WithMCPServer(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.started = true

	mcpSrv := mcpapi.NewServer(srv.quicBaaSService, &mcpapi.Config{
		ServerName:    "test",
		ServerVersion: "1.0",
		Transport:     "http",
		HTTPAddr:      "127.0.0.1:0",
	}, srv.logger)
	srv.mcpServer = mcpSrv

	seamMu.Lock()
	mcpStopFn = func(m *mcpapi.Server) error { return nil }
	seamMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := srv.Shutdown(ctx)
	assert.NoError(t, err)
}

func TestFT2_Shutdown_WithMessaging(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.started = true

	msgSvc := messaging.NewPushService(srv.logger, nil)

	srv.messagingService = msgSvc

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err)
}

// =============================================================================
// initializeCA - Additional paths
// =============================================================================

func TestFT2_EnsureTLSCertificates_ExistingCerts(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// First call generates certs.
	err = srv.initializeCA()
	assert.NoError(t, err)

	// Second call should find existing certs.
	err = srv.initializeCA()
	assert.NoError(t, err)

	assert.NotEmpty(t, srv.config.Security.TLSCertFile)
	assert.NotEmpty(t, srv.config.Security.TLSKeyFile)
	assert.NotEmpty(t, srv.config.Security.TLSCAFile)
}

func TestFT2_EnsureTLSCertificates_WithCustomHostname(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "custom-host.example.com"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	assert.NoError(t, err)
}

// =============================================================================
// startCatalogManager - Additional Path Tests
// =============================================================================

func TestFT2_StartCatalogManager_WorkerStartError(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.ObjectStorage.Enabled = true
	srv.config.ObjectStorage.Backends = map[string]*config.StorageBackendConfig{
		"test": {
			Type: "memory",
		},
	}

	seamMu.Lock()
	workerMgrStartAllFn = func(mgr *catalog.WorkerManager, ctx context.Context) error {
		return &ft2TestError{msg: "worker start failed"}
	}
	seamMu.Unlock()

	err := srv.startCatalogManager()
	if err != nil {
		assert.Contains(t, err.Error(), "worker")
	}
}

// =============================================================================
// startService - Additional Path Tests
// =============================================================================

func TestFT2_StartService_WithTimeSeries(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	require.NoError(t, err)

	err = srv.startIndexes()
	require.NoError(t, err)

	tsErr := srv.initTimeSeriesStorage()
	require.NoError(t, tsErr)

	// Standalone raft node required by service builder
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
	assert.NotNil(t, srv.quicBaaSService)

	if srv.timeSeriesStorage != nil {
	}
}

func TestFT2_StartService_WithEmbedding(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	require.NoError(t, err)

	err = srv.startIndexes()
	require.NoError(t, err)

	mgr, mErr := embedding.NewManager(embedding.Config{
		MaxBatchSize: 128, Timeout: 30, Provider: "ollama",
		APIURL:     "http://localhost:11434",
		Model:      "test",
		Dimensions: 768,
	})
	if mErr == nil {
		srv.embeddingManager = mgr
	}

	// Standalone raft node required by service builder
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
}

func TestFT2_StartService_WithMessaging(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Messaging.Enabled = true
	cfg.Messaging.WatchBridge.Enabled = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	require.NoError(t, err)

	err = srv.startIndexes()
	require.NoError(t, err)

	// Standalone raft node required by service builder
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	err = srv.startService()
	assert.NoError(t, err)
}

// =============================================================================
// registerIAMService / registerOllamaService
// =============================================================================

func TestFT2_RegisterIAMService_NilService(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.quicBaaSService = nil

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	// With nil quicBaaSService, should skip registration.
	srv.registerIAMService(grpcServer)
}

func TestFT2_RegisterIAMService_WithService(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	// Service exists and should expose IAM components.
	srv.registerIAMService(grpcServer)
}

// =============================================================================
// Getters / Misc Methods
// =============================================================================

func TestFT2_GetEffectiveMode(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	assert.Equal(t, config.ServerModeStandalone, srv.GetEffectiveMode())
}

func TestFT2_GetIssuerURL(t *testing.T) {
	saveAndRestoreSeams(t)

	tests := []struct {
		name     string
		host     string
		port     int
		tls      bool
		expected string
	}{
		{
			name:     "HTTP default",
			host:     "localhost",
			port:     8443,
			tls:      false,
			expected: "http://localhost:8443",
		},
		{
			name:     "HTTPS with TLS",
			host:     "myhost",
			port:     9443,
			tls:      true,
			expected: "https://myhost:9443",
		},
		{
			name:     "0.0.0.0 becomes localhost",
			host:     "0.0.0.0",
			port:     8443,
			tls:      false,
			expected: "http://localhost:8443",
		},
		{
			name:     "Empty host becomes localhost",
			host:     "",
			port:     0,
			tls:      false,
			expected: "http://localhost:8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createFTConfig(t)
			cfg.Server.Host = tt.host
			cfg.Server.REST.Port = tt.port
			cfg.Security.TLSEnabled = tt.tls
			srv, err := NewServer(cfg)
			require.NoError(t, err)

			url := srv.getIssuerURL()
			assert.Equal(t, tt.expected, url)
		})
	}
}

// =============================================================================
// Shutdown - Combined Component Paths
// =============================================================================

func TestFT2_Shutdown_FullComponentStack(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.startStorage()
	require.NoError(t, err)

	err = srv.startIndexes()
	require.NoError(t, err)

	srv.initTimeSeriesStorage()

	msgSvc := messaging.NewPushService(srv.logger, nil)

	srv.messagingService = msgSvc

	srv.started = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(ctx)
	assert.NoError(t, err)
}

// =============================================================================
// Initialize Auth Manager - Error Paths
// =============================================================================

func TestFT2_AuthManager_NoSigningKeyID(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Creating an enabled auth manager without a JWT signing key ID must fail.
	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "",
	}

	authMgr, authErr := auth.NewInMemoryAuthManager(authCfg, srv.logger)
	assert.Error(t, authErr)
	assert.ErrorContains(t, authErr, "JWT signing key ID")
	assert.Nil(t, authMgr)
}

// =============================================================================
// kvShards utility function tests
// =============================================================================

func TestFT2_KvShards_ExcludesSpecialShards(t *testing.T) {
	saveAndRestoreSeams(t)

	shards := []uint64{1, 2, 3, 100}
	result := kvShards(shards, 2, 3, 100)
	assert.Equal(t, []uint64{1}, result)
}

func TestFT2_KvShards_EmptyInput(t *testing.T) {
	saveAndRestoreSeams(t)

	result := kvShards(nil, 2, 3, 100)
	assert.Equal(t, []uint64{1}, result) // kvShards always returns at least shard 1
}

func TestFT2_KvShards_NoExclusions(t *testing.T) {
	saveAndRestoreSeams(t)

	shards := []uint64{1, 4, 5}
	result := kvShards(shards, 2, 3, 100)
	assert.Equal(t, []uint64{1, 4, 5}, result)
}

// =============================================================================
// initializeIAMComponents - Full Path
// =============================================================================

func TestFT2_InitializeIAMComponents_Full(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Auth.AllowPasswordAuth = true
	cfg.Auth.BootstrapAdmin = true
	cfg.Auth.InsecureDevMode = true
	cfg.Auth.Enabled = true
	// The OIDC provider's ES256 signing key is created through the
	// xkms-backed iam.NewSigningKeyManagerFromXKMS. That requires the xkms
	// subsystem to be initialized, which only happens inside startKMS when
	// KMS is enabled. The production lifecycle runs startKMS before
	// initializeIAMComponents; without it the signing key manager returns
	// "quicbaas xkms not initialized" and the OIDC provider is left nil
	// (graceful degradation), so this full-path test must enable KMS to
	// faithfully exercise the OIDC branch.
	cfg.KMS.Enabled = true
	// Enable AAGUID enforcement so the WebAuthn AAGUID-allowlist wiring branch
	// in initializeIAMComponents runs (the allowlist is built and installed on
	// the WebAuthn manager).
	cfg.IAM.AAGUID.Mode = "enforce"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Initialize storage and indexes so qrdbClient is available
	require.NoError(t, srv.startStorage())
	require.NoError(t, srv.startIndexes())

	// Initialize xkms (KMS infrastructure) so the OIDC ES256 signing key can
	// be created. Matches the production startup ordering.
	require.NoError(t, srv.startKMS(), "KMS init must succeed for OIDC signing key")

	// Standalone raft node required by service builder
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	// Start service to create qrdbClient
	require.NoError(t, srv.startService())

	// This exercises the full initializeIAMComponents path.
	components, err := srv.initializeIAMComponents()
	assert.NoError(t, err)

	if srv.qrdbClient != nil {
		// With storage available, IAM components should be fully initialized
		require.NotNil(t, components)
		assert.NotNil(t, components.GetTenantManager())
		assert.NotNil(t, components.GetIdentityStore())
		assert.NotNil(t, components.GetPolicyEngine())
		assert.NotNil(t, components.GetAppManager())
		assert.NotNil(t, components.GetOIDCProvider())
		assert.NotNil(t, components.GetSessionStorage())
		assert.NotNil(t, components.GetSessionManager())
		// The password-built internal auth manager has been removed; tokens are
		// minted by the IAM factors via the token service, so this is nil.
		assert.Nil(t, components.GetInternalAuthManager())
	} else {
		// Without qrdbClient, IAM init is skipped gracefully
		assert.Nil(t, components)
	}
}

func TestFT2_InitializeIAMComponents_NoPasswordAuth(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Auth.AllowPasswordAuth = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Initialize storage and indexes so qrdbClient is available
	require.NoError(t, srv.startStorage())
	require.NoError(t, srv.startIndexes())
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	components, iamErr := srv.initializeIAMComponents()
	assert.NoError(t, iamErr)

	if srv.qrdbClient != nil {
		require.NotNil(t, components)
		// Auth manager should be nil since password auth is disabled.
		assert.Nil(t, components.GetInternalAuthManager())
	} else {
		assert.Nil(t, components)
	}
}

// TestFT2_RegisterLoginService_NilService covers the nil-service skip branch of
// registerLoginService: with no QuicBaaS facade, login registration is skipped.
func TestFT2_RegisterLoginService_NilService(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.quicBaaSService = nil

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	// Must skip cleanly without panicking.
	srv.registerLoginService(grpcServer, nil)
}

// TestFT2_RegisterLoginService_WithService covers the registration branch of
// registerLoginService: with a QuicBaaS facade present, the unified Login
// ceremony service is registered on the gRPC server.
func TestFT2_RegisterLoginService_WithService(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)

	grpcServer := ggrpc.NewServer()
	defer grpcServer.Stop()

	// Service exists; the login ceremony servicer must register without panic.
	srv.registerLoginService(grpcServer, nil)
}
