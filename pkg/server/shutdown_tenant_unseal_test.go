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
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/api/errclass"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// shutdownDrainWait bounds each wait on the Shutdown goroutine. It is longer
// than orchestration.DefaultShutdownDrainTimeout, so a drain that is never
// released still ends inside it.
const shutdownDrainWait = 30 * time.Second

// blockingJobDrainer is an orchestration service whose shutdown drain holds
// until the test releases it. Only Shutdown is called: the embedded service is
// nil.
type blockingJobDrainer struct {
	orchestration.AgentService
	entered chan struct{}
	release chan struct{}
}

// Shutdown reports that the drain was entered and then holds until released
// or until the drain window ends.
func (d *blockingJobDrainer) Shutdown(ctx context.Context) error {
	close(d.entered)
	select {
	case <-d.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown refuses a new tenant unseal from the moment it marks the server
// closed: while the running jobs are still draining, and the node and its
// native transports are still up, the installed router and a facade a
// transport took at boot both answer unavailable and no unseal observer runs.
// The client that was already built stays open until the node has stopped.
func TestShutdown_RefusesANewTenantUnsealWhileJobsDrain(t *testing.T) {
	node := buildMemoryStandaloneNode(t)

	drainer := &blockingJobDrainer{entered: make(chan struct{}), release: make(chan struct{})}
	facadeMock := testutil.NewMockQuicBaaS()
	facadeMock.AgentServiceOverride = drainer

	s := &Server{logger: buildTestLogger(t), qrdbNode: node, quicBaaSService: facadeMock}
	// Shutdown closes the node; this closes it only when Shutdown did not.
	t.Cleanup(func() {
		if left := s.getQRDBNode(); left != nil {
			_ = left.Close()
		}
	})
	// The router the node's native transports call, built as
	// unifiedModuleServerOptions builds it.
	s.tenantUnsealRouter = newTenantUnsealRouter(s.rawQRDBTenantServicer, s.buildTenantBarrierService)

	var observed atomic.Int32
	s.tenantUnsealHooks.Register(tenantUnsealFunc(func(context.Context, string) { observed.Add(1) }))

	bootFacade := s.buildTenantBarrierService()
	require.NotNil(t, bootFacade, "before shutdown the facade is available")
	require.NotNil(t, s.currentTenantClient())

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- s.Shutdown(context.Background()) }()
	released := false
	defer func() {
		if released {
			return
		}
		close(drainer.release)
		select {
		case <-shutdownErr:
		case <-time.After(shutdownDrainWait):
		}
	}()

	select {
	case <-drainer.entered:
	case err := <-shutdownErr:
		t.Fatalf("Shutdown returned without draining the running jobs: %v", err)
	case <-time.After(shutdownDrainWait):
		t.Fatal("Shutdown did not reach the job drain")
	}

	// Shutdown is now held in the drain step: nothing has been torn down.
	require.NotNil(t, s.getQRDBNode(), "the node is still running while the jobs drain")
	require.NotNil(t, s.rawQRDBTenantServicer(), "the node's tenant servicer is still there")

	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "admin", TenantID: "acme"})
	err := s.tenantUnsealRouter.TenantBarrierUnseal(ctx, "acme", []byte("key"))
	require.ErrorIs(t, err, errTenantServiceUnavailable)
	require.Equal(t, errclass.Unavailable, errclass.Classify(err))

	progress, err := s.tenantUnsealRouter.TenantBarrierUnsealShare(ctx, "acme", "share")
	require.ErrorIs(t, err, errTenantServiceUnavailable)
	require.Equal(t, errclass.Unavailable, errclass.Classify(err))
	require.Nil(t, progress)

	err = bootFacade.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key"))
	require.True(t, hasErrorCode(err, pkgerrors.ErrUnavailable), "expected ErrUnavailable, got %v", err)
	require.Equal(t, errclass.Unavailable, errclass.Classify(classTenantBarrierError(err)))

	require.Nil(t, s.buildTenantBarrierService(), "no facade is built once shutdown has begun")
	require.Zero(t, observed.Load(), "no unseal observer may run during shutdown")

	s.tenantSDKMu.Lock()
	held := s.tenantSDKClient
	s.tenantSDKMu.Unlock()
	require.NotNil(t, held, "the built client stays open until the node has stopped")

	close(drainer.release)
	released = true
	select {
	case err := <-shutdownErr:
		require.NoError(t, err)
	case <-time.After(shutdownDrainWait):
		t.Fatal("Shutdown did not return after the drain was released")
	}

	require.Nil(t, s.getQRDBNode())
	s.tenantSDKMu.Lock()
	held = s.tenantSDKClient
	s.tenantSDKMu.Unlock()
	require.Nil(t, held, "the client is closed once the node has stopped")
	require.Zero(t, observed.Load())
}
