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
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ragIndexMockService exposes both an embedding service and a vector service so
// the RAG index success paths (read -> chunk -> embed -> upsert) can execute
// end to end. The existing extendedMockService only wires embedding, leaving
// the vector half nil, which is why these paths were previously uncovered.
type ragIndexMockService struct {
	mockService
	embeddingSvc service.EmbeddingService
	vectorSvc    service.VectorService
}

func (m *ragIndexMockService) Embedding() service.EmbeddingService { return m.embeddingSvc }
func (m *ragIndexMockService) Vector() service.VectorService       { return m.vectorSvc }

func newRAGIndexFullRegistry(t *testing.T, emb service.EmbeddingService, vec service.VectorService) *ToolRegistry {
	t.Helper()
	svc := &ragIndexMockService{embeddingSvc: emb, vectorSvc: vec}
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	return NewToolRegistry(svc, log)
}

// =============================================================================
// executeRAGIndexFile — success and per-step error branches
// =============================================================================

func TestExecuteRAGIndexFile_Success(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o644))

	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, &mockVectorService{})
	result, err := reg.executeRAGIndexFile(context.Background(), map[string]interface{}{
		"file_path": file,
		"namespace": "ns",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteRAGIndexFile_ReadError(t *testing.T) {
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, &mockVectorService{})
	_, err := reg.executeRAGIndexFile(context.Background(), map[string]interface{}{
		"file_path": filepath.Join(t.TempDir(), "does-not-exist.go"),
		"namespace": "ns",
	})
	require.Error(t, err)
	var readErr *RAGIndexFileReadError
	assert.True(t, errors.As(err, &readErr))
}

func TestExecuteRAGIndexFile_EmbeddingError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o644))

	emb := &mockEmbeddingService{
		generateFunc: func(_ context.Context, _ *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
			return nil, errors.New("embed boom")
		},
	}
	reg := newRAGIndexFullRegistry(t, emb, &mockVectorService{})
	_, err := reg.executeRAGIndexFile(context.Background(), map[string]interface{}{
		"file_path": file,
		"namespace": "ns",
	})
	require.Error(t, err)
	var embErr *RAGIndexEmbeddingError
	assert.True(t, errors.As(err, &embErr))
}

func TestExecuteRAGIndexFile_UpsertError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o644))

	vec := &mockVectorService{
		upsertVectorFunc: func(_ context.Context, _ *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
			return nil, errors.New("upsert boom")
		},
	}
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, vec)
	_, err := reg.executeRAGIndexFile(context.Background(), map[string]interface{}{
		"file_path": file,
		"namespace": "ns",
	})
	require.Error(t, err)
	var upsertErr *RAGIndexUpsertError
	assert.True(t, errors.As(err, &upsertErr))
}

func TestExecuteRAGIndexFile_VectorUnavailable(t *testing.T) {
	// embedding present, vector nil -> service-unavailable for vector.
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, nil)
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o644))

	_, err := reg.executeRAGIndexFile(context.Background(), map[string]interface{}{
		"file_path": file,
		"namespace": "ns",
	})
	require.Error(t, err)
}

func TestExecuteRAGIndexFile_EmbeddingUnavailable(t *testing.T) {
	reg := newRAGIndexFullRegistry(t, nil, &mockVectorService{})
	_, err := reg.executeRAGIndexFile(context.Background(), map[string]interface{}{
		"file_path": "/tmp/whatever.go",
		"namespace": "ns",
	})
	require.Error(t, err)
}

// =============================================================================
// executeRAGIndexProject — success path over a real temp tree
// =============================================================================

func TestExecuteRAGIndexProject_Success(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\n\nvar x = 1\n"), 0o644))

	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, &mockVectorService{})
	result, err := reg.executeRAGIndexProject(context.Background(), map[string]interface{}{
		"project_path": dir,
		"namespace":    "ns",
		"file_pattern": "*.go",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteRAGIndexProject_DefaultPatternAndTolerantErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o644))

	// Both embedding and upsert fail; the project indexer tolerates per-chunk
	// errors via continue, so the call still succeeds with zero chunks.
	emb := &mockEmbeddingService{
		generateFunc: func(_ context.Context, _ *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
			return nil, errors.New("embed boom")
		},
	}
	reg := newRAGIndexFullRegistry(t, emb, &mockVectorService{})
	result, err := reg.executeRAGIndexProject(context.Background(), map[string]interface{}{
		"project_path": dir,
		"namespace":    "ns",
		// no file_pattern -> defaults to *.go
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteRAGIndexProject_VectorUnavailable(t *testing.T) {
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, nil)
	_, err := reg.executeRAGIndexProject(context.Background(), map[string]interface{}{
		"project_path": t.TempDir(),
		"namespace":    "ns",
	})
	require.Error(t, err)
}

// =============================================================================
// executeRAGIndexStatus — success and backend error
// =============================================================================

func TestExecuteRAGIndexStatus_Success(t *testing.T) {
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, &mockVectorService{})
	result, err := reg.executeRAGIndexStatus(context.Background(), map[string]interface{}{
		"namespace": "ns",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteRAGIndexStatus_BackendError(t *testing.T) {
	vec := &mockVectorService{
		getNamespaceInfoFunc: func(_ context.Context, _ *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
			return nil, errors.New("ns boom")
		},
	}
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, vec)
	_, err := reg.executeRAGIndexStatus(context.Background(), map[string]interface{}{
		"namespace": "ns",
	})
	require.Error(t, err)
}

func TestExecuteRAGIndexStatus_VectorUnavailable(t *testing.T) {
	reg := newRAGIndexFullRegistry(t, &mockEmbeddingService{}, nil)
	_, err := reg.executeRAGIndexStatus(context.Background(), map[string]interface{}{
		"namespace": "ns",
	})
	require.Error(t, err)
}

// =============================================================================
// Server-level service setters — delegate to ToolRegistry without panicking
// =============================================================================

func TestServer_SetServices_Delegation(t *testing.T) {
	server, _ := newTestServer(t)

	// Installing nil takes each surface offline; tools then return the
	// corresponding *ServiceUnavailableError. These setters were previously
	// at 0% coverage despite being part of the lifecycle wiring contract.
	require.NotPanics(t, func() {
		server.SetCeremonyService(nil)
		server.SetCredentialService(nil)
		server.SetTenantBarrierService(nil)
	})
}
