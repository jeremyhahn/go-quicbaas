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

//go:build !integration

// stop_catalog_backend_errors_test.go covers the Close-error branches of
// stopCatalogManager and stopBackendManager (and their teardown error-collection
// branches in Server.Shutdown) by injecting interface-typed managers whose Close
// fails. Both managers are catalog interfaces, so a nil-embedding fake that only
// overrides Close is sufficient — no other method is reached during teardown.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// failCatalogManager embeds catalog.CatalogManager (nil) and fails Close.
type failCatalogManager struct{ catalog.CatalogManager }

func (failCatalogManager) Close() error { return errors.New("catalog close refused") }

// failBackendManager embeds catalog.BackendManager (nil) and fails Close.
type failBackendManager struct{ catalog.BackendManager }

func (failBackendManager) Close() error { return errors.New("backend close refused") }

// TestStopCatalogManager_CloseError drives the Close-error branch of
// stopCatalogManager.
func TestStopCatalogManager_CloseError(t *testing.T) {
	srv := withTempServer(t)
	srv.catalogManager = failCatalogManager{}

	err := srv.stopCatalogManager()
	require.Error(t, err, "a failing catalog-manager Close must surface an error")
	assert.Contains(t, err.Error(), "catalog manager")
}

// TestStopBackendManager_CloseError drives the Close-error branch of
// stopBackendManager.
func TestStopBackendManager_CloseError(t *testing.T) {
	srv := withTempServer(t)
	srv.backendManager = failBackendManager{}

	err := srv.stopBackendManager()
	require.Error(t, err, "a failing backend-manager Close must surface an error")
	assert.Contains(t, err.Error(), "backend manager")
}

// TestShutdown_CatalogAndBackendCloseErrorsCollected drives both teardown
// error-collection branches in Server.Shutdown: failing catalog and backend
// managers are both collected into the aggregate ShutdownError, and shutdown
// still completes.
func TestShutdown_CatalogAndBackendCloseErrorsCollected(t *testing.T) {
	srv := withTempServer(t)
	srv.catalogManager = failCatalogManager{}
	srv.backendManager = failBackendManager{}

	err := srv.Shutdown(context.Background())
	require.Error(t, err, "failing manager Closes must surface in the aggregate shutdown error")
	assert.True(t, srv.IsClosed(), "shutdown must still mark the server closed")
}
