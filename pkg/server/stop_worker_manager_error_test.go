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

// stop_worker_manager_error_test.go covers the StopAll-error branch of
// stopWorkerManager (and, by extension, the worker-manager teardown error branch
// of Server.Shutdown): a registered worker whose Stop() fails makes
// WorkerManager.StopAll return an error, which stopWorkerManager wraps as a
// typed operation-failed error.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// stopFailWorker is a catalog.Worker whose Stop always fails. It reports itself
// as running after Start so WorkerManager.StopAll invokes its Stop.
type stopFailWorker struct{ running bool }

func (w *stopFailWorker) Start(context.Context) error { w.running = true; return nil }
func (w *stopFailWorker) Stop() error                 { return errors.New("worker stop refused") }
func (w *stopFailWorker) Name() string                { return "stop-failer" }
func (w *stopFailWorker) IsRunning() bool             { return w.running }
func (w *stopFailWorker) Metrics() *catalog.WorkerMetrics {
	return &catalog.WorkerMetrics{}
}

// TestStopWorkerManager_StopAllError drives the StopAll-error branch of
// stopWorkerManager via a worker whose Stop fails.
func TestStopWorkerManager_StopAllError(t *testing.T) {
	srv := withTempServer(t)

	wm := catalog.NewWorkerManager(logger.NewAdapter(srv.logger))
	require.NoError(t, wm.Register(&stopFailWorker{}))
	require.NoError(t, wm.StartAll(context.Background()))
	srv.workerManager = wm

	err := srv.stopWorkerManager()
	require.Error(t, err, "a worker whose Stop fails must surface a StopAll error")
	assert.Contains(t, err.Error(), "stop worker manager",
		"the wrapped error must identify the worker-manager teardown step")
}

// TestShutdown_WorkerManagerStopErrorCollected drives the worker-manager
// teardown error branch of Server.Shutdown: a failing worker manager surfaces in
// the aggregate ShutdownError and does not abort the rest of teardown.
func TestShutdown_WorkerManagerStopErrorCollected(t *testing.T) {
	srv := withTempServer(t)

	wm := catalog.NewWorkerManager(logger.NewAdapter(srv.logger))
	require.NoError(t, wm.Register(&stopFailWorker{}))
	require.NoError(t, wm.StartAll(context.Background()))
	srv.workerManager = wm

	err := srv.Shutdown(context.Background())
	require.Error(t, err, "a failing worker manager must surface in the aggregate shutdown error")
	assert.True(t, srv.IsClosed(), "shutdown must still mark the server closed despite the teardown error")
}
