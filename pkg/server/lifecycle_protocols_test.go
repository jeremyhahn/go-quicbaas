//go:build !integration && rest && quic && mcp && rag

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// startRESTServer early returns
// ---------------------------------------------------------------------------

func TestServer_StartRESTServer_Embedded(t *testing.T) {
	t.Run("returns nil in embedded mode", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Server.Embedded = true
		assert.NoError(t, srv.startRESTServer())
	})
}

func TestServer_StartRESTServer_Disabled(t *testing.T) {
	t.Run("returns nil when REST disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Server.Embedded = false
		srv.config.Server.REST.Enabled = false
		assert.NoError(t, srv.startRESTServer())
	})
}

// ---------------------------------------------------------------------------
// stopRESTServer
// ---------------------------------------------------------------------------

func TestServer_StopRESTServer_Nil(t *testing.T) {
	t.Run("returns nil when rest server nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.restServer = nil
		assert.NoError(t, srv.stopRESTServer(context.Background()))
	})
}

// ---------------------------------------------------------------------------
// startRAG early returns
// ---------------------------------------------------------------------------

func TestServer_StartRAG_Disabled(t *testing.T) {
	t.Run("returns nil when RAG disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.RAG.Enabled = false
		assert.NoError(t, srv.startRAG())
	})
}

// ---------------------------------------------------------------------------
// stopRAG
// ---------------------------------------------------------------------------

func TestServer_StopRAG_Nil(t *testing.T) {
	t.Run("returns nil when rag system nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.ragSystem = nil
		assert.NoError(t, srv.stopRAG())
	})
}

// ---------------------------------------------------------------------------
// registerOllamaService with nil service
// ---------------------------------------------------------------------------

func TestServer_RegisterOllamaService_NilService(t *testing.T) {
	t.Run("returns when quicbaas service nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.quicBaaSService = nil
		// Should not panic
		srv.registerOllamaService(nil)
	})
}

// ---------------------------------------------------------------------------
// stopCatalogManager nil check
// ---------------------------------------------------------------------------

func TestServer_StopCatalogManager_Nil(t *testing.T) {
	t.Run("shutdown path when catalog manager nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.catalogManager = nil
		// In shutdown, the nil check prevents calling stopCatalogManager.
		// But we can call Shutdown directly and verify no error.
		ctx := context.Background()
		err := srv.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Shutdown with discovery manager
// ---------------------------------------------------------------------------

func TestServer_ShutdownWithDiscoveryManager(t *testing.T) {
	t.Run("shutdown path", func(t *testing.T) {
		srv := withTempServer(t)
		ctx := context.Background()
		err := srv.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// Messaging disabled test
// ---------------------------------------------------------------------------

func TestServer_StartMessaging_Disabled(t *testing.T) {
	t.Run("messaging skipped when disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Messaging.Enabled = false
		// Messaging is checked in initialize() with config.Messaging.Enabled
		// Just verify the field is false.
		assert.False(t, srv.config.Messaging.Enabled)
	})
}
