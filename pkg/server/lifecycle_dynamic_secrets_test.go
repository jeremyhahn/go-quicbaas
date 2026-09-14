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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// The wiring that was missing (go-quicbaas#902).
//
// Security().DynamicSecrets() was nil in every deployment because nothing ever
// composed it. These tests pin the composition itself — which is where the
// decision lives — and the two lifecycle answers around it: a server with no
// durable store leaves the capability unconfigured rather than accepting grants
// it could not revoke, and shutdown stops the reaper without destroying the
// grants.
// =============================================================================

func TestComposeDynamicSecrets(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	ctx := context.Background()

	handler, leaseManager, generators, err := composeDynamicSecrets(ctx, kvstore.NewMemory(), logger.NewAdapter(log))
	require.NoError(t, err)
	require.NotNil(t, handler)
	require.NotNil(t, leaseManager)
	require.NotNil(t, generators)
	defer func() { require.NoError(t, leaseManager.Close()) }()

	// The lease verbs are live against the durable store, which is what the
	// agent credential lifecycle is built on.
	created, err := leaseManager.CreateLease(ctx, "accounts/delivery/t/_none/_none/acct-1", time.Hour, nil)
	require.NoError(t, err)

	listed, err := handler.ListLeases(ctx, &service.ListLeasesRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, listed.Count)
	assert.Equal(t, created.LeaseID, listed.Leases[0].LeaseID)

	// Revocation through the SDK surface really revokes.
	_, err = handler.RevokeLease(ctx, &service.RevokeLeaseRequest{LeaseID: created.LeaseID})
	require.NoError(t, err)
	_, err = leaseManager.Validate(ctx, created.LeaseID)
	assert.True(t, lease.IsLeaseRevoked(err), "want a revoked refusal, got %v", err)
}

func TestComposeDynamicSecrets_EmptyRegistryRefusesAnUnknownGenerator(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	ctx := context.Background()

	handler, leaseManager, _, err := composeDynamicSecrets(ctx, kvstore.NewMemory(), logger.NewAdapter(log))
	require.NoError(t, err)
	defer func() { require.NoError(t, leaseManager.Close()) }()

	// No DBConnector exists in this repository, so no database generator is
	// registered and a role naming one is refused at CREATION time — which is
	// the honest answer, rather than a role that fails when it is first used.
	_, err = handler.CreateRole(ctx, &service.CreateRoleRequest{
		Name:          "readonly",
		GeneratorType: "postgres",
		DefaultTTL:    time.Hour,
		MaxTTL:        2 * time.Hour,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generator")
}

func TestComposeDynamicSecrets_BadInput(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	t.Run("nil store", func(t *testing.T) {
		_, _, _, err := composeDynamicSecrets(context.Background(), nil, logger.NewAdapter(log))
		require.Error(t, err)
	})

	t.Run("nil logger", func(t *testing.T) {
		_, _, _, err := composeDynamicSecrets(context.Background(), kvstore.NewMemory(), nil)
		require.Error(t, err)
	})
}

// TestStartDynamicSecrets_NoDurableStoreLeavesItUnconfigured is the fail-closed
// answer: a lease manager over a store that is not there would hand out grants
// nothing could revoke, which is worse than having no grants at all.
func TestStartDynamicSecrets_NoDurableStoreLeavesItUnconfigured(t *testing.T) {
	srv := newTestServer(t)

	require.NoError(t, srv.startDynamicSecrets(), "a storage-less boot must not fail on this")
	assert.Nil(t, srv.LeaseManager(), "no lease authority may exist without a durable store")
}

func TestStopDynamicSecrets_IsSafeBeforeAndAfterStart(t *testing.T) {
	srv := newTestServer(t)

	// Never started.
	require.NoError(t, srv.stopDynamicSecrets())

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	manager, err := lease.NewManager(&lease.ManagerConfig{Store: kvstore.NewMemory(), Logger: logger.NewAdapter(log)})
	require.NoError(t, err)

	srv.mu.Lock()
	srv.leaseManager = manager
	srv.mu.Unlock()
	require.NotNil(t, srv.LeaseManager())

	require.NoError(t, srv.stopDynamicSecrets())
	assert.Nil(t, srv.LeaseManager(), "the authority is released on shutdown")
	// Idempotent: shutdown may be reached twice.
	require.NoError(t, srv.stopDynamicSecrets())
}

// scanFaultKV is a fault injector over a real memory engine whose prefix scan
// fails, which is what an unreachable store looks like to the handler's
// role-loading step.
type scanFaultKV struct {
	interfaces.KVStore
}

func (s scanFaultKV) Scan(context.Context, string, func(string, []byte) error) error {
	return &dberrors.QBError{Code: dberrors.ErrUnavailable, Op: "scanFaultKV.Scan"}
}

// TestComposeDynamicSecrets_ReleasesTheLeaseManagerWhenTheHandlerFails is the
// bad-input twin that matters for resources: a half-composed capability must not
// leave a reaper goroutine's owner behind.
func TestComposeDynamicSecrets_ReleasesTheLeaseManagerWhenTheHandlerFails(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	handler, leaseManager, generators, err := composeDynamicSecrets(
		context.Background(), scanFaultKV{KVStore: kvstore.NewMemory()}, logger.NewAdapter(log))
	require.Error(t, err, "a store that cannot be scanned must not yield a capability")
	assert.Nil(t, handler)
	assert.Nil(t, leaseManager)
	assert.Nil(t, generators)
}
