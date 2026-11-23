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

package lease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func storeLapsedLease(t *testing.T, mgr *Manager, id string) *Lease {
	t.Helper()
	lapsed := &Lease{LeaseID: id, SecretPath: id, ExpiresAt: time.Now().Add(-time.Second), TTL: time.Second}
	require.NoError(t, mgr.storeLease(context.Background(), "test", lapsed))
	return lapsed
}

func TestReapExpired_ExpireHookRunsBeforeRevoke(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()
	lapsed := storeLapsedLease(t, mgr, "lease_hooked")

	var seen []string
	mgr.SetExpireHook(func(hctx context.Context, l *Lease) error {
		assert.Nil(t, l.RevokedAt, "the hook runs before the revoke")
		stored, err := mgr.GetLease(hctx, l.LeaseID)
		if assert.NoError(t, err) {
			assert.Nil(t, stored.RevokedAt, "the stored lease is still unrevoked inside the hook")
		}
		seen = append(seen, l.LeaseID)
		return nil
	})

	revoked, _, err := mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	assert.Equal(t, []string{lapsed.LeaseID}, seen)

	_, err = mgr.Validate(ctx, lapsed.LeaseID)
	assert.True(t, IsLeaseRevoked(err), "the lease is revoked after the hook, got %v", err)
}

func TestReapExpired_ExpireHookErrorKeepsLeaseForRetry(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()
	lapsed := storeLapsedLease(t, mgr, "lease_retry")

	fail := true
	calls := 0
	mgr.SetExpireHook(func(context.Context, *Lease) error {
		calls++
		if fail {
			return errors.New("teardown refused")
		}
		return nil
	})

	revoked, _, err := mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, revoked)
	stored, err := mgr.GetLease(ctx, lapsed.LeaseID)
	require.NoError(t, err)
	assert.Nil(t, stored.RevokedAt, "a failed hook leaves the lease unrevoked")

	fail = false
	revoked, _, err = mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	assert.Equal(t, 2, calls)
	_, err = mgr.Validate(ctx, lapsed.LeaseID)
	assert.True(t, IsLeaseRevoked(err), "the retried lease is revoked, got %v", err)
}

func TestReapExpired_ExpireHookNotCalledForLiveOrRevokedLeases(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	live, err := mgr.CreateLease(ctx, "live", time.Hour, nil)
	require.NoError(t, err)
	gone := storeLapsedLease(t, mgr, "lease_gone")
	require.NoError(t, mgr.RevokeLease(ctx, gone.LeaseID))

	var mu sync.Mutex
	var seen []string
	mgr.SetExpireHook(func(_ context.Context, l *Lease) error {
		mu.Lock()
		seen = append(seen, l.LeaseID)
		mu.Unlock()
		return nil
	})

	revoked, _, err := mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, revoked)
	assert.Empty(t, seen)
	_, err = mgr.Validate(ctx, live.LeaseID)
	assert.NoError(t, err)

	// A cleared hook is not called either.
	mgr.SetExpireHook(nil)
	storeLapsedLease(t, mgr, "lease_unhooked")
	revoked, _, err = mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	assert.Empty(t, seen)
}
