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

package server

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// shutdownOrderFakeService embeds the service.QuicBaaS interface (nil — only
// Close is invoked by Server.Shutdown) and records whether the sharded qrdb
// client was still open at the moment the facade was closed. This pins the
// shutdown ordering: the facade owns background pollers (scheduler engine,
// KMS rotation scanner) whose storage transport is the sharded client, so
// the facade MUST close while the transport is still open.
type shutdownOrderFakeService struct {
	service.QuicBaaS
	sc                *ShardedClient
	closeCalled       atomic.Bool
	clientOpenAtClose atomic.Bool
}

// Agent reports no orchestration service, so Server.Shutdown's job drain
// finds nothing to drain rather than dereferencing the embedded nil facade.
func (f *shutdownOrderFakeService) Agent() orchestration.AgentService { return nil }

// Close records the ordering observation and reports success.
func (f *shutdownOrderFakeService) Close() error {
	f.closeCalled.Store(true)
	f.clientOpenAtClose.Store(!f.sc.closed.Load())
	return nil
}

// TestServerShutdown_ServiceFacadeClosesBeforeShardedClient proves that
// Server.Shutdown closes the QuicBaaS service facade (which stops and drains
// the scheduler poll loop and the KMS rotation scanner) BEFORE the sharded
// qrdb client is closed. Regression test for the shutdown leak where the
// scheduler poller outlived the storage transport and spammed
// "transport: connection is closed" errors after node teardown.
func TestServerShutdown_ServiceFacadeClosesBeforeShardedClient(t *testing.T) {
	sc := newDetachedShardedClient()
	fake := &shutdownOrderFakeService{sc: sc}

	s := &Server{
		logger:          buildTestLogger(t),
		quicBaaSService: fake,
		qrdbClient:      sc,
	}

	require.NoError(t, s.Shutdown(context.Background()))

	assert.True(t, fake.closeCalled.Load(),
		"Server.Shutdown must close the QuicBaaS service facade")
	assert.True(t, fake.clientOpenAtClose.Load(),
		"service facade must close BEFORE the sharded qrdb client so pollers drain while their storage transport is still open")
	assert.True(t, sc.closed.Load(),
		"sharded qrdb client must be closed by the end of Shutdown")
}

// TestServerShutdown_ServiceFacadeCloseErrorCollected proves that a facade
// close failure is surfaced in the aggregated shutdown error rather than
// silently dropped.
func TestServerShutdown_ServiceFacadeCloseErrorCollected(t *testing.T) {
	fake := &shutdownOrderFailingService{}

	s := &Server{
		logger:          buildTestLogger(t),
		quicBaaSService: fake,
	}

	err := s.Shutdown(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "facade close failed")
}

// shutdownOrderFailingService fails Close to exercise error aggregation.
type shutdownOrderFailingService struct {
	service.QuicBaaS
}

// Agent reports no orchestration service; see shutdownOrderFakeService.Agent.
func (f *shutdownOrderFailingService) Agent() orchestration.AgentService { return nil }

// Close returns a typed error for the aggregation test.
func (f *shutdownOrderFailingService) Close() error {
	return &shutdownOrderCloseError{}
}

// shutdownOrderCloseError is the typed error returned by the failing fake.
type shutdownOrderCloseError struct{}

// Error returns a human-readable description.
func (e *shutdownOrderCloseError) Error() string {
	return "facade close failed"
}
