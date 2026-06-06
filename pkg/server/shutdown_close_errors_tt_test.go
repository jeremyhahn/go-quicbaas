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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag && iam

// shutdown_close_errors_tt_test.go drives the Close()/Stop() error-collection
// branches of Server.Shutdown that are otherwise unreachable on the clean
// node-backed path. Each branch follows the same shape:
//
//	if err := component.Close(); err != nil {
//	    errs = append(errs, err)   // <- the branch under test
//	    ...log...
//	}
//
// Shutdown collects per-component teardown failures into an aggregate
// ShutdownError rather than aborting, so a single failing component must (a)
// surface in the returned error and (b) NOT prevent the remaining components
// from being torn down. The fakes here inject a failing Close on an injectable
// interface-typed field (vectorStorage) so the error branch runs hermetically.

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// failingVectorStorage embeds interfaces.VectorStorage (nil) and overrides only
// Close to fail. Shutdown invokes Close on s.vectorStorage; no other method is
// reached during teardown, so the nil embedding is safe and keeps the double
// minimal while satisfying the full interface.
type failingVectorStorage struct {
	interfaces.VectorStorage
	closed bool
}

func (f *failingVectorStorage) Close() error {
	f.closed = true
	return errors.New("vector storage close failed")
}

// TestServer_Shutdown_TT_VectorStorageCloseFailure covers the vector-storage
// teardown error branch of Server.Shutdown: a failing Close is collected into
// the aggregate ShutdownError, and the rest of the shutdown sequence still
// runs to completion (the node and barrier are torn down cleanly afterwards).
func TestServer_Shutdown_TT_VectorStorageCloseFailure(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.initialize())

	fvs := &failingVectorStorage{}
	srv.vectorStorage = fvs

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := srv.Shutdown(ctx)
	require.Error(t, err, "a failing vector-storage Close must surface in the aggregate shutdown error")
	assert.True(t, fvs.closed, "vector storage Close must have been invoked during shutdown")
	assert.True(t, srv.IsClosed(), "shutdown must still mark the server closed despite the teardown error")
}
