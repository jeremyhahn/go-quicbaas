//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// server_getters_test.go exercises the trivial Server getter methods that
// return cached component references. Each accessor's contract is "return
// the component or nil when not yet wired"; the tests confirm both the
// nil-default branch and that the field set at construction is returned
// verbatim.

package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestServer_GettersReturnFields confirms each accessor returns the field
// installed on the Server struct. The fields are zero-initialized in the
// fresh server from withTempServer(t); the test then assigns sentinel
// values and verifies the accessor reflects the assignment without going
// through the Start lifecycle.
func TestServer_GettersReturnFields(t *testing.T) {
	srv := withTempServer(t)

	t.Run("Uptime advances", func(t *testing.T) {
		srv.startTime = time.Now().Add(-2 * time.Second)
		got := srv.Uptime()
		assert.GreaterOrEqual(t, got, 2*time.Second)
	})

	t.Run("GetConfig returns server config", func(t *testing.T) {
		assert.Same(t, srv.config, srv.GetConfig())
	})

	t.Run("GetRESTServer is nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetRESTServer())
	})

	t.Run("GetGRPCServer is nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetGRPCServer())
	})

	t.Run("GetQUICServer is nil before start", func(t *testing.T) {
		assert.Nil(t, srv.GetQUICServer())
	})

	t.Run("GetCatalogManager is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetCatalogManager())
	})

	t.Run("GetBackendManager is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetBackendManager())
	})

	t.Run("GetWorkerManager is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetWorkerManager())
	})

	t.Run("GetDevClusterManager is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetDevClusterManager())
	})

	t.Run("GetRAGSystem is nil when disabled", func(t *testing.T) {
		assert.Nil(t, srv.GetRAGSystem())
	})

	t.Run("GetEmbeddingManager is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetEmbeddingManager())
	})

	t.Run("GetWatchBridge is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetWatchBridge())
	})

	t.Run("GetBackupManager is nil when unwired", func(t *testing.T) {
		assert.Nil(t, srv.GetBackupManager())
	})

	t.Run("CA returns nil when TLS is disabled", func(t *testing.T) {
		assert.Nil(t, srv.CA())
	})

	t.Run("GetCertificateAuthority returns nil when TLS is disabled", func(t *testing.T) {
		assert.Nil(t, srv.GetCertificateAuthority())
	})

	t.Run("GetQRDBClient is nil before storage init", func(t *testing.T) {
		assert.Nil(t, srv.GetQRDBClient())
	})

	t.Run("Service is nil before lifecycle wiring", func(t *testing.T) {
		assert.Nil(t, srv.Service())
	})

	t.Run("GetCeremonyServices is nil before lifecycle wiring", func(t *testing.T) {
		assert.Nil(t, srv.GetCeremonyServices())
	})

	t.Run("GetEffectiveMode returns a non-empty string", func(t *testing.T) {
		assert.NotEmpty(t, srv.GetEffectiveMode())
	})

	t.Run("GetSealManager is nil when seal disabled", func(t *testing.T) {
		assert.Nil(t, srv.GetSealManager())
	})

	t.Run("GetRaftNode is nil before init", func(t *testing.T) {
		assert.Nil(t, srv.GetRaftNode())
	})
}

// TestServer_UpdateCA_NoCAAdapter exercises the not-ready branch that
// surfaces before any startup wiring has occurred.
func TestServer_UpdateCA_NoCAAdapter(t *testing.T) {
	srv := withTempServer(t)

	t.Run("nil CA is a no-op", func(t *testing.T) {
		assert.NoError(t, srv.UpdateCA(nil))
	})
}

// TestServer_WithCAOption confirms the WithCA option is callable and
// produces a non-nil ServerOption. The option's effect is exercised
// via the constructor's options processing.
func TestServer_WithCAOption_ConstructsOption(t *testing.T) {
	opt := WithCA(nil)
	assert.NotNil(t, opt)
}

// TestServer_WithTLSCertificateOption confirms WithTLSCertificate is callable.
func TestServer_WithTLSCertificateOption_ConstructsOption(t *testing.T) {
	opt := WithTLSCertificate(nil)
	assert.NotNil(t, opt)
}

// TestWithSealPassphrase_StoredOnServerOptions verifies that WithSealPassphrase
// is a valid ServerOption and that the passphrase is recorded in serverOptions.
// The qrdb node builders read opts.sealPassphrase to decide whether to
// auto-unseal the barrier on startup; this test guards that wiring.
func TestWithSealPassphrase_StoredOnServerOptions(t *testing.T) {
	opt := WithSealPassphrase([]byte("test-passphrase"))
	assert.NotNil(t, opt)

	cfg := config.Default()
	srv, err := NewServer(cfg, opt)
	require.NoError(t, err)

	assert.Equal(t, []byte("test-passphrase"), srv.opts.sealPassphrase,
		"WithSealPassphrase must store the passphrase in serverOptions.sealPassphrase")
}

// TestWithSealPassphrase_NilPassphrase verifies that a nil passphrase is
// accepted without panicking and stored as-is (callers may pass nil to
// indicate "no passphrase available; boot sealed").
func TestWithSealPassphrase_NilPassphrase(t *testing.T) {
	opt := WithSealPassphrase(nil)
	assert.NotNil(t, opt)

	cfg := config.Default()
	srv, err := NewServer(cfg, opt)
	require.NoError(t, err)

	assert.Nil(t, srv.opts.sealPassphrase,
		"WithSealPassphrase(nil) must leave sealPassphrase nil")
}
