//go:build !integration && !iam

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
	"testing"

	"github.com/stretchr/testify/assert"

	ggrpc "google.golang.org/grpc"
)

// =============================================================================
// Noop Embedding Stub Tests (build tag: !vector)
// =============================================================================

func TestStartEmbedding_Noop(t *testing.T) {
	t.Run("returns nil without error", func(t *testing.T) {
		srv := withTempServer(t)
		err := srv.startEmbedding()
		assert.NoError(t, err)
	})
}

func TestStopEmbedding_Noop(t *testing.T) {
	t.Run("returns nil without error", func(t *testing.T) {
		srv := withTempServer(t)
		err := srv.stopEmbedding()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Noop RAG Stub Tests (build tag: !rag)
// =============================================================================

func TestRegisterOllamaService_Noop(t *testing.T) {
	t.Run("does not panic with nil grpc server", func(t *testing.T) {
		srv := withTempServer(t)
		// Should not panic
		srv.registerOllamaService(nil)
	})

	t.Run("does not panic with real grpc server", func(t *testing.T) {
		srv := withTempServer(t)
		grpcServer := ggrpc.NewServer()
		defer grpcServer.Stop()
		// Should not panic
		srv.registerOllamaService(grpcServer)
	})
}

// =============================================================================
// Noop IAM Stub Tests (build tag: !iam)
// =============================================================================

func TestRegisterIAMService_Noop(t *testing.T) {
	t.Run("does not panic with nil grpc server", func(t *testing.T) {
		srv := withTempServer(t)
		srv.registerIAMService(nil)
	})

	t.Run("does not panic with real grpc server", func(t *testing.T) {
		srv := withTempServer(t)
		grpcServer := ggrpc.NewServer()
		defer grpcServer.Stop()
		srv.registerIAMService(grpcServer)
	})
}

func TestInitializeIAMComponents_Noop(t *testing.T) {
	t.Run("returns nil components and nil error", func(t *testing.T) {
		srv := withTempServer(t)
		components, err := srv.initializeIAMComponents()
		assert.NoError(t, err)
		assert.Nil(t, components)
	})
}

// =============================================================================
// Server Accessor Tests
// =============================================================================

func TestGetEmbeddingManager(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetEmbeddingManager())
	})
}

func TestGetBackupManager(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetBackupManager())
	})
}

func TestGetRAGSystem(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetRAGSystem())
	})
}

func TestGetWatchBridge(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetWatchBridge())
	})
}

func TestGetCatalogManager(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetCatalogManager())
	})
}

func TestGetBackendManager(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetBackendManager())
	})
}

func TestGetWorkerManager(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetWorkerManager())
	})
}

func TestGetDevClusterManager(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.GetDevClusterManager())
	})
}

func TestService(t *testing.T) {
	t.Run("returns nil when not initialized", func(t *testing.T) {
		srv := withTempServer(t)
		assert.Nil(t, srv.Service())
	})
}
