//go:build local && !raft && !rest && !quic && !mcp && !rag && !iam && !integration

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

// lifecycle_seam_errors_local_test.go covers the error-propagation branches of
// the server lifecycle by injecting failures through the package-level seam
// function variables (initializeCAFn, pushServiceStartFn, watchBridgeStartFn,
// validateRoutingFn). Each seam is overridden for the duration of one test and
// restored via t.Cleanup so the swaps never leak across tests. These exercise
// the "if err != nil { return ... }" paths that the happy-path lifecycle tests
// cannot reach.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// TestServer_Initialize_Local_CAFailurePropagates verifies initialize returns
// the error from the CA-bootstrap seam without proceeding to start storage.
func TestServer_Initialize_Local_CAFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	sentinel := errors.New("ca bootstrap exploded")
	orig := initializeCAFn
	initializeCAFn = func(_ *Server) error { return sentinel }
	t.Cleanup(func() { initializeCAFn = orig })

	err := srv.initialize()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel,
		"initialize must surface the CA-bootstrap failure verbatim")
}

// TestServer_StartMessaging_Local_StartFailurePropagates verifies startMessaging
// wraps a PushService start failure as an operation-failed error.
func TestServer_StartMessaging_Local_StartFailurePropagates(t *testing.T) {
	srv := withTempServer(t)

	sentinel := errors.New("push service refused to start")
	orig := pushServiceStartFn
	pushServiceStartFn = func(_ *messaging.PushService, _ context.Context) error { return sentinel }
	t.Cleanup(func() { pushServiceStartFn = orig })

	err := srv.startMessaging()
	require.Error(t, err, "a push-service start failure must abort startMessaging")
	assert.Contains(t, err.Error(), "messaging",
		"the wrapped error must identify the messaging subsystem")
}

// TestServer_StartWatchBridge_Local_StartFailurePropagates verifies that a
// watch-bridge start failure aborts startWatchBridge. The watch bridge requires
// the QuicBaaS facade and its watch manager, so the service facade is brought up
// first via startService.
func TestServer_StartWatchBridge_Local_StartFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	// startService wires the QuicBaaS facade (and thus the watch manager) that
	// startWatchBridge depends on. A raft node is required by the builder.
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	// startWatchBridge reads s.messagingService for the bridge wiring; provide a
	// started push service so the bridge constructor has a sink to attach to.
	require.NoError(t, srv.startMessaging())

	sentinel := errors.New("watch bridge start failed")
	orig := watchBridgeStartFn
	watchBridgeStartFn = func(_ *watchbridge.WatchBridge, _ context.Context) error { return sentinel }
	t.Cleanup(func() { watchBridgeStartFn = orig })

	err := srv.startWatchBridge()
	require.Error(t, err, "a watch-bridge start failure must abort startWatchBridge")
	assert.Contains(t, err.Error(), "watch bridge",
		"the wrapped error must identify the watch bridge")
}

// TestServer_StartGRPCServer_Local_RoutingValidationFailurePropagates verifies
// startGRPCServer aborts when the shard-routing validation seam reports an
// invalid configuration. The server is non-embedded so the listener body runs
// up to the routing check; the service facade is wired first.
func TestServer_StartGRPCServer_Local_RoutingValidationFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = false

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	require.NoError(t, srv.startService())

	sentinel := errors.New("invalid shard routing")
	orig := validateRoutingFn
	validateRoutingFn = func(_ []uint64, _ map[string]uint64) error { return sentinel }
	t.Cleanup(func() { validateRoutingFn = orig })

	err := srv.startGRPCServer()
	require.Error(t, err, "an invalid routing config must abort startGRPCServer")
	assert.Contains(t, err.Error(), "routing",
		"the wrapped error must identify the routing-config validation step")
}

// TestServer_Initialize_Local_StorageFailurePropagates verifies initialize
// returns the storage-init error when the pebble data directory cannot be
// created. This covers both the startStorage error branch and the
// corresponding error return inside initialize.
func TestServer_Initialize_Local_StorageFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Storage.Backend = "pebble"
	// A path under /proc cannot be created, so MkdirAll fails.
	srv.config.Storage.DataDir = "/proc/cannot-create-quicbaas-storage"

	err := srv.initialize()
	require.Error(t, err, "an uncreatable storage dir must abort initialize")
	var storageErr *StorageInitError
	require.ErrorAs(t, err, &storageErr,
		"initialize must surface a typed StorageInitError from startStorage")
}

// TestServer_StartCatalogManager_Local_StorageFailurePropagates verifies that a
// failure constructing the catalog storage backend aborts startCatalogManager
// with a wrapped error rather than leaving a half-initialized manager.
func TestServer_StartCatalogManager_Local_StorageFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = true

	sentinel := errors.New("catalog storage open failed")
	orig := newCatalogStore
	newCatalogStore = func(_ *catalog.PebbleStorageConfig) (catalog.PebbleStorage, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { newCatalogStore = orig })

	err := srv.startCatalogManager()
	require.Error(t, err, "catalog storage construction failure must abort startCatalogManager")
	assert.Contains(t, err.Error(), "catalog storage",
		"the wrapped error must identify the failing catalog-storage step")
	assert.Nil(t, srv.catalogManager, "no catalog manager may be left set on failure")
}

// TestServer_Initialize_Local_CatalogFailurePropagates verifies the catalog
// failure surfaces through the top-level initialize as well.
func TestServer_Initialize_Local_CatalogFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.ObjectStorage.Enabled = true

	sentinel := errors.New("catalog storage open failed")
	orig := newCatalogStore
	newCatalogStore = func(_ *catalog.PebbleStorageConfig) (catalog.PebbleStorage, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { newCatalogStore = orig })

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when the catalog manager fails to start")
}

// TestServer_StartKMS_Local_SoftwareBackendFailurePropagates verifies that a
// failure constructing the KMS software backend aborts startKMS with a wrapped
// error. The software-backend constructor seam is overridden to fail.
func TestServer_StartKMS_Local_SoftwareBackendFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.KMS.Enabled = true

	sentinel := errors.New("software backend unavailable")
	orig := newSoftwareBackend
	newSoftwareBackend = func(_ *xkmssdk.SoftwareBackendConfig) (xkmssdk.SymmetricKeyProvider, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { newSoftwareBackend = orig })

	err := srv.startKMS()
	require.Error(t, err, "a software-backend construction failure must abort startKMS")
	assert.Contains(t, err.Error(), "software backend",
		"the wrapped error must identify the failing KMS step")
}

// TestServer_StartSecrets_Local_ManagerFailurePropagates verifies startSecrets
// wraps a secrets-manager construction failure as an operation-failed error.
func TestServer_StartSecrets_Local_ManagerFailurePropagates(t *testing.T) {
	srv := withTempServer(t)

	sentinel := errors.New("secrets manager init failed")
	orig := newSecretsMgr
	newSecretsMgr = func(_ *secrets.ManagerOptions) (*secrets.Manager, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { newSecretsMgr = orig })

	err := srv.startSecrets()
	require.Error(t, err, "a secrets-manager construction failure must abort startSecrets")
	assert.Nil(t, srv.secretsManager, "no secrets manager may be left set on failure")
}

// TestServer_Initialize_Local_KMSFailurePropagates verifies the KMS startup
// failure surfaces through initialize (covering the KMS error return inside the
// component startup sequence).
func TestServer_Initialize_Local_KMSFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.KMS.Enabled = true

	sentinel := errors.New("software backend unavailable")
	orig := newSoftwareBackend
	newSoftwareBackend = func(_ *xkmssdk.SoftwareBackendConfig) (xkmssdk.SymmetricKeyProvider, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { newSoftwareBackend = orig })

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when KMS startup fails")
}

// TestServer_Initialize_Local_SecretsFailurePropagates verifies the secrets
// startup failure surfaces through initialize.
func TestServer_Initialize_Local_SecretsFailurePropagates(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	sentinel := errors.New("secrets manager init failed")
	orig := newSecretsMgr
	newSecretsMgr = func(_ *secrets.ManagerOptions) (*secrets.Manager, error) {
		return nil, sentinel
	}
	t.Cleanup(func() { newSecretsMgr = orig })

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when the secrets manager fails to start")
}

// TestServer_StartBackupManager_Local_InvalidEncryptionKeySize covers the
// encryption-key-size validation branch: AES-256-GCM requires a 32-byte key, so
// a short key aborts startBackupManager with the typed key-size error.
func TestServer_StartBackupManager_Local_InvalidEncryptionKeySize(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Backup.Enabled = true
	srv.config.Backup.DefaultEncryption = "aes-256-gcm"
	// A 5-byte key (decoded from a non-base64 literal) is not 32 bytes.
	srv.config.Backup.EncryptionKey = "short"

	err := srv.startBackupManager()
	require.Error(t, err, "an undersized AES-256-GCM key must abort startBackupManager")
}
