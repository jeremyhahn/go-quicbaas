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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

// start_catalog_workers_errors_tt_test.go covers the worker-registration
// warning branches of startCatalogManager: each background worker
// (replication, archive, verify, retention) is registered with the worker
// manager, and a registration failure is logged-and-tolerated rather than
// aborting startup. Injecting a failing workerMgrRegisterFn drives all four
// register-failure branches in a single pass; startCatalogManager must still
// complete and leave a usable catalog manager.

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// TestServer_StartCatalogManager_TT_WorkerRegistrationFailuresTolerated drives
// the four worker-registration error branches: every workerMgrRegisterFn call
// fails, so each Register branch logs a warning and continues. startCatalogManager
// must still succeed (worker registration is best-effort).
func TestServer_StartCatalogManager_TT_WorkerRegistrationFailuresTolerated(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.ObjectStorage.Enabled = true

	workerMgrRegisterFn = func(*catalog.WorkerManager, catalog.Worker) error {
		return errors.New("worker registration refused")
	}

	require.NoError(t, srv.startCatalogManager(),
		"worker-registration failures must be tolerated, not abort catalog startup")
	require.NotNil(t, srv.catalogManager, "catalog manager must still be set")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
