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
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testLogger returns the interfaces.Logger every manager under test writes to.
func testLogger(t *testing.T) interfaces.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return logger.NewAdapter(log)
}

// newTestManager builds a manager over a real qrdb memory engine.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return newTestManagerOn(t, kvstore.NewMemory(), 0)
}

// newTestManagerOn builds a manager over store with an explicit tombstone
// retention (zero selects the default).
func newTestManagerOn(t *testing.T, store interfaces.KVStore, retention time.Duration) *Manager {
	t.Helper()
	mgr, err := NewManager(&ManagerConfig{
		Store:              store,
		Logger:             testLogger(t),
		TombstoneRetention: retention,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mgr.Close()) })
	return mgr
}

// =============================================================================
// Construction
// =============================================================================

func TestNewManager_Defaults(t *testing.T) {
	mgr, err := NewManager(&ManagerConfig{Store: kvstore.NewMemory(), Logger: testLogger(t)})
	require.NoError(t, err)
	defer func() { require.NoError(t, mgr.Close()) }()

	assert.Equal(t, DefaultExpirationCheckInterval, mgr.checkInterval)
	assert.Equal(t, DefaultTombstoneRetention, mgr.tombstoneRetention)
}

func TestNewManager_HonoursExplicitIntervals(t *testing.T) {
	mgr, err := NewManager(&ManagerConfig{
		Store:                   kvstore.NewMemory(),
		Logger:                  testLogger(t),
		ExpirationCheckInterval: 3 * time.Second,
		TombstoneRetention:      90 * time.Minute,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, mgr.Close()) }()

	assert.Equal(t, 3*time.Second, mgr.checkInterval)
	assert.Equal(t, 90*time.Minute, mgr.tombstoneRetention)
}

func TestNewManager_InvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		config *ManagerConfig
	}{
		{name: "nil config", config: nil},
		{name: "nil store", config: &ManagerConfig{Logger: testLogger(t)}},
		{name: "nil logger", config: &ManagerConfig{Store: kvstore.NewMemory()}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := NewManager(tc.config)
			require.Error(t, err)
			assert.Nil(t, mgr)
			assert.True(t, dberrors.IsInvalidInput(err), "want invalid-input classification, got %v", err)
		})
	}
}

// =============================================================================
// CreateLease
// =============================================================================

func TestCreateLease(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	lease, err := mgr.CreateLease(ctx, "credentials/db/reader", 10*time.Minute, nil)
	require.NoError(t, err)

	assert.NotEmpty(t, lease.LeaseID)
	assert.Equal(t, "credentials/db/reader", lease.SecretPath)
	assert.Equal(t, 10*time.Minute, lease.TTL)
	assert.True(t, lease.IsValid())
	assert.True(t, lease.Renewable, "default options are renewable")
	assert.Nil(t, lease.RevokedAt)
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), lease.ExpiresAt, 5*time.Second)

	// The lease is durable, not merely returned.
	stored, err := mgr.GetLease(ctx, lease.LeaseID)
	require.NoError(t, err)
	assert.Equal(t, lease.LeaseID, stored.LeaseID)
}

func TestCreateLease_CarriesOptions(t *testing.T) {
	mgr := newTestManager(t)

	lease, err := mgr.CreateLease(context.Background(), "p", time.Minute, &LeaseOptions{
		Renewable:   false,
		MaxRenewals: 2,
		Metadata:    map[string]string{"account_id": "acct-1"},
	})
	require.NoError(t, err)

	assert.False(t, lease.Renewable)
	assert.Equal(t, 2, lease.MaxRenewals)
	assert.Equal(t, "acct-1", lease.Metadata["account_id"])
}

func TestCreateLease_BadInput(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	tests := []struct {
		name string
		path string
		ttl  time.Duration
	}{
		{name: "empty path", path: "", ttl: time.Minute},
		{name: "ttl below minimum", path: "p", ttl: MinLeaseTTL - time.Nanosecond},
		{name: "ttl above maximum", path: "p", ttl: MaxLeaseTTL + time.Second},
		{name: "negative ttl", path: "p", ttl: -time.Minute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lease, err := mgr.CreateLease(ctx, tc.path, tc.ttl, nil)
			require.Error(t, err)
			assert.Nil(t, lease)
			assert.True(t, dberrors.IsInvalidInput(err), "want invalid-input, got %v", err)
		})
	}
}

func TestCreateLease_StoreFault(t *testing.T) {
	store := newFaultKV()
	store.putErr = &dberrors.MessageError{Message: "kv unavailable"}
	mgr := newTestManagerOn(t, store, 0)

	lease, err := mgr.CreateLease(context.Background(), "p", time.Minute, nil)
	require.Error(t, err)
	assert.Nil(t, lease)

	var storeErr *LeaseStoreError
	require.ErrorAs(t, err, &storeErr)
	assert.Equal(t, "put", storeErr.Operation)
}

func TestCreateLease_CancelledContext(t *testing.T) {
	mgr := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lease, err := mgr.CreateLease(ctx, "p", time.Minute, nil)
	require.Error(t, err)
	assert.Nil(t, lease)
}

// =============================================================================
// GetLease / Validate — the observability verb and the enforcement verb
// =============================================================================

func TestGetLease(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", time.Minute, nil)
	require.NoError(t, err)

	got, err := mgr.GetLease(ctx, created.LeaseID)
	require.NoError(t, err)
	assert.Equal(t, created.SecretPath, got.SecretPath)
	assert.Equal(t, created.TTL, got.TTL)
}

func TestGetLease_BadInput(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	t.Run("empty id", func(t *testing.T) {
		_, err := mgr.GetLease(ctx, "")
		require.Error(t, err)
		assert.True(t, dberrors.IsInvalidInput(err))
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := mgr.GetLease(ctx, "lease_does_not_exist")
		require.Error(t, err)
		assert.True(t, IsLeaseNotFound(err), "want not-found, got %v", err)
	})

	t.Run("undecodable record", func(t *testing.T) {
		store := kvstore.NewMemory()
		require.NoError(t, store.Put(ctx, leaseKey("lease_bad"), []byte("{not json")))
		corrupt := newTestManagerOn(t, store, 0)

		_, err := corrupt.GetLease(ctx, "lease_bad")
		require.Error(t, err)
		var corruptErr *LeaseCorruptError
		assert.ErrorAs(t, err, &corruptErr)
	})
}

func TestValidate_ActiveLease(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", time.Minute, nil)
	require.NoError(t, err)

	valid, err := mgr.Validate(ctx, created.LeaseID)
	require.NoError(t, err)
	assert.Equal(t, created.LeaseID, valid.LeaseID)
}

// TestValidate_RefusesRevoked is the load-bearing assertion of the whole
// package: once revoked, the grant is refused SERVER-SIDE, from the store,
// with a reason — not merely reported as absent and not left to a sweep.
func TestValidate_RefusesRevoked(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", time.Hour, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.RevokeLease(ctx, created.LeaseID))

	_, err = mgr.Validate(ctx, created.LeaseID)
	require.Error(t, err)
	assert.True(t, IsLeaseRevoked(err), "want revoked refusal, got %v", err)

	var revoked *LeaseRevokedError
	require.ErrorAs(t, err, &revoked)
	assert.Equal(t, created.LeaseID, revoked.LeaseID)
	assert.False(t, revoked.RevokedAt.IsZero(), "the refusal names when the grant was taken away")

	// The tombstone is still readable for audit, and says revoked.
	tombstone, err := mgr.GetLease(ctx, created.LeaseID)
	require.NoError(t, err)
	assert.Equal(t, LeaseStateRevoked, tombstone.State())
	assert.False(t, tombstone.IsValid())
}

// TestValidate_RefusesExpiredWithoutSweep proves the TTL is a RULE: the grant
// is refused the instant it lapses, with no expiration pass having run.
func TestValidate_RefusesExpiredWithoutSweep(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	expired := &Lease{
		LeaseID:    "lease_expired",
		SecretPath: "p",
		IssuedAt:   time.Now().Add(-2 * time.Hour),
		ExpiresAt:  time.Now().Add(-time.Hour),
		TTL:        time.Hour,
		Renewable:  true,
	}
	require.NoError(t, mgr.storeLease(ctx, "test", expired))

	_, err := mgr.Validate(ctx, expired.LeaseID)
	require.Error(t, err)
	assert.True(t, IsLeaseExpired(err), "want expired refusal, got %v", err)

	var expiredErr *LeaseExpiredError
	require.ErrorAs(t, err, &expiredErr)
	assert.Equal(t, expired.ExpiresAt.Unix(), expiredErr.ExpiredAt.Unix())
}

func TestValidate_BadInput(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	t.Run("empty id", func(t *testing.T) {
		_, err := mgr.Validate(ctx, "")
		require.Error(t, err)
		assert.True(t, dberrors.IsInvalidInput(err))
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := mgr.Validate(ctx, "lease_nope")
		require.Error(t, err)
		assert.True(t, IsLeaseNotFound(err))
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := mgr.Validate(cancelled, "lease_anything")
		require.Error(t, err)
	})

	t.Run("store fault is not a refusal", func(t *testing.T) {
		store := newFaultKV()
		store.getErr = &dberrors.MessageError{Message: "kv unavailable"}
		faulted := newTestManagerOn(t, store, 0)

		_, err := faulted.Validate(ctx, "lease_anything")
		require.Error(t, err)
		assert.False(t, IsLeaseNotFound(err), "an outage must not be reported as an absence")
		var storeErr *LeaseStoreError
		assert.ErrorAs(t, err, &storeErr)
	})
}

// =============================================================================
// RenewLease
// =============================================================================

func TestRenewLease(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", time.Minute, nil)
	require.NoError(t, err)

	renewed, err := mgr.RenewLease(ctx, created.LeaseID, 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, renewed.RenewCount)
	assert.True(t, renewed.ExpiresAt.After(created.ExpiresAt))

	// Persisted, not just returned.
	stored, err := mgr.GetLease(ctx, created.LeaseID)
	require.NoError(t, err)
	assert.Equal(t, 1, stored.RenewCount)
}

func TestRenewLease_ZeroIncrementUsesOriginalTTL(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", 30*time.Minute, nil)
	require.NoError(t, err)

	renewed, err := mgr.RenewLease(ctx, created.LeaseID, 0)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(30*time.Minute), renewed.ExpiresAt, 5*time.Second)
}

func TestRenewLease_BadInput(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	t.Run("empty id", func(t *testing.T) {
		_, err := mgr.RenewLease(ctx, "", time.Minute)
		require.Error(t, err)
		assert.True(t, dberrors.IsInvalidInput(err))
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := mgr.RenewLease(ctx, "lease_nope", time.Minute)
		require.Error(t, err)
		assert.True(t, IsLeaseNotFound(err))
	})

	t.Run("revoked lease", func(t *testing.T) {
		created, err := mgr.CreateLease(ctx, "p", time.Hour, nil)
		require.NoError(t, err)
		require.NoError(t, mgr.RevokeLease(ctx, created.LeaseID))

		_, err = mgr.RenewLease(ctx, created.LeaseID, time.Minute)
		require.Error(t, err)
		assert.True(t, IsLeaseRevoked(err), "want revoked refusal, got %v", err)
	})

	t.Run("expired lease", func(t *testing.T) {
		expired := &Lease{
			LeaseID:   "lease_renew_expired",
			ExpiresAt: time.Now().Add(-time.Minute),
			TTL:       time.Minute,
			Renewable: true,
		}
		require.NoError(t, mgr.storeLease(ctx, "test", expired))

		_, err := mgr.RenewLease(ctx, expired.LeaseID, time.Minute)
		require.Error(t, err)
		assert.True(t, IsLeaseExpired(err), "want expired refusal, got %v", err)
	})

	t.Run("non-renewable lease", func(t *testing.T) {
		created, err := mgr.CreateLease(ctx, "p", time.Hour, &LeaseOptions{Renewable: false})
		require.NoError(t, err)

		_, err = mgr.RenewLease(ctx, created.LeaseID, time.Minute)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be renewed")
	})

	t.Run("renewal budget exhausted", func(t *testing.T) {
		created, err := mgr.CreateLease(ctx, "p", time.Hour, &LeaseOptions{Renewable: true, MaxRenewals: 1})
		require.NoError(t, err)

		_, err = mgr.RenewLease(ctx, created.LeaseID, time.Hour)
		require.NoError(t, err)

		_, err = mgr.RenewLease(ctx, created.LeaseID, time.Hour)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be renewed")
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := mgr.RenewLease(cancelled, "lease_anything", time.Minute)
		require.Error(t, err)
	})
}

func TestRenewLease_StoreFault(t *testing.T) {
	store := newFaultKV()
	mgr := newTestManagerOn(t, store, 0)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", time.Hour, nil)
	require.NoError(t, err)

	store.putErr = &dberrors.MessageError{Message: "kv unavailable"}
	_, err = mgr.RenewLease(ctx, created.LeaseID, time.Hour)
	require.Error(t, err)

	var storeErr *LeaseStoreError
	require.ErrorAs(t, err, &storeErr)
	assert.Equal(t, "put", storeErr.Operation)
}

// =============================================================================
// RevokeLease
// =============================================================================

func TestRevokeLease_IsIdempotentAndKeepsTheFirstTime(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	created, err := mgr.CreateLease(ctx, "p", time.Hour, nil)
	require.NoError(t, err)

	require.NoError(t, mgr.RevokeLease(ctx, created.LeaseID))
	first, err := mgr.GetLease(ctx, created.LeaseID)
	require.NoError(t, err)
	require.NotNil(t, first.RevokedAt)

	// A retrying conclusion must converge, not fail — and must not rewrite the
	// revocation time, which is the audit fact.
	require.NoError(t, mgr.RevokeLease(ctx, created.LeaseID))
	second, err := mgr.GetLease(ctx, created.LeaseID)
	require.NoError(t, err)
	require.NotNil(t, second.RevokedAt)
	assert.Equal(t, first.RevokedAt.UnixNano(), second.RevokedAt.UnixNano())
}

func TestRevokeLease_AbsentLeaseIsNotAFailure(t *testing.T) {
	mgr := newTestManager(t)

	// A tombstone purged between two conclusion attempts must not make the
	// second attempt report a failed revocation.
	assert.NoError(t, mgr.RevokeLease(context.Background(), "lease_already_purged"))
}

func TestRevokeLease_BadInput(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	t.Run("empty id", func(t *testing.T) {
		err := mgr.RevokeLease(ctx, "")
		require.Error(t, err)
		assert.True(t, dberrors.IsInvalidInput(err))
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		assert.Error(t, mgr.RevokeLease(cancelled, "lease_anything"))
	})

	t.Run("store fault surfaces", func(t *testing.T) {
		store := newFaultKV()
		faulted := newTestManagerOn(t, store, 0)
		created, err := faulted.CreateLease(ctx, "p", time.Hour, nil)
		require.NoError(t, err)

		store.putErr = &dberrors.MessageError{Message: "kv unavailable"}
		err = faulted.RevokeLease(ctx, created.LeaseID)
		require.Error(t, err)
		var storeErr *LeaseStoreError
		assert.ErrorAs(t, err, &storeErr)
	})
}

// =============================================================================
// RevokePrefix
// =============================================================================

func TestRevokePrefix(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	keep, err := mgr.CreateLease(ctx, "other/path", time.Hour, nil)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err := mgr.CreateLease(ctx, "credentials/db/"+strconv.Itoa(i), time.Hour, nil)
		require.NoError(t, err)
	}

	count, err := mgr.RevokePrefix(ctx, "credentials/db/")
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// The unmatched lease is untouched.
	_, err = mgr.Validate(ctx, keep.LeaseID)
	assert.NoError(t, err)

	// A second pass has nothing left to revoke: tombstones are skipped.
	count, err = mgr.RevokePrefix(ctx, "credentials/db/")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRevokePrefix_BadInput(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	t.Run("no matches", func(t *testing.T) {
		count, err := mgr.RevokePrefix(ctx, "nothing/here/")
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := mgr.RevokePrefix(cancelled, "credentials/")
		require.Error(t, err)
	})

	t.Run("scan fault", func(t *testing.T) {
		store := newFaultKV()
		store.scanErr = &dberrors.MessageError{Message: "kv unavailable"}
		faulted := newTestManagerOn(t, store, 0)

		_, err := faulted.RevokePrefix(ctx, "credentials/")
		require.Error(t, err)
		var storeErr *LeaseStoreError
		assert.ErrorAs(t, err, &storeErr)
	})
}

// =============================================================================
// ListLeases
// =============================================================================

func TestListLeases(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	_, err := mgr.CreateLease(ctx, "credentials/db/one", time.Hour, nil)
	require.NoError(t, err)
	_, err = mgr.CreateLease(ctx, "credentials/db/two", time.Hour, nil)
	require.NoError(t, err)
	_, err = mgr.CreateLease(ctx, "accounts/anthropic/seat", time.Hour, nil)
	require.NoError(t, err)

	all, err := mgr.ListLeases(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	scoped, err := mgr.ListLeases(ctx, "credentials/db/")
	require.NoError(t, err)
	assert.Len(t, scoped, 2)

	none, err := mgr.ListLeases(ctx, "nope/")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestListLeases_SkipsUndecodableRecords(t *testing.T) {
	ctx := context.Background()
	store := kvstore.NewMemory()
	mgr := newTestManagerOn(t, store, 0)

	good, err := mgr.CreateLease(ctx, "p", time.Hour, nil)
	require.NoError(t, err)
	require.NoError(t, store.Put(ctx, leaseKey("lease_corrupt"), []byte("{oops")))

	// One bad record must not hide the rest of the listing.
	leases, err := mgr.ListLeases(ctx, "")
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, good.LeaseID, leases[0].LeaseID)
}

func TestListLeases_BadInput(t *testing.T) {
	ctx := context.Background()

	t.Run("scan fault", func(t *testing.T) {
		store := newFaultKV()
		store.scanErr = &dberrors.MessageError{Message: "kv unavailable"}
		mgr := newTestManagerOn(t, store, 0)

		_, err := mgr.ListLeases(ctx, "")
		require.Error(t, err)
		var storeErr *LeaseStoreError
		assert.ErrorAs(t, err, &storeErr)
	})

	t.Run("cancelled context", func(t *testing.T) {
		mgr := newTestManager(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := mgr.ListLeases(cancelled, "")
		require.Error(t, err)
	})
}

// =============================================================================
// ReapExpired — expiry ENFORCED, tombstones bounded
// =============================================================================

func TestReapExpired_RevokesLapsedLeases(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	live, err := mgr.CreateLease(ctx, "live", time.Hour, nil)
	require.NoError(t, err)
	lapsed := &Lease{LeaseID: "lease_lapsed", SecretPath: "lapsed", ExpiresAt: time.Now().Add(-time.Second), TTL: time.Second}
	require.NoError(t, mgr.storeLease(ctx, "test", lapsed))

	revoked, purged, err := mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, revoked)
	assert.Equal(t, 0, purged)

	_, err = mgr.Validate(ctx, lapsed.LeaseID)
	assert.True(t, IsLeaseRevoked(err), "an expired lease is revoked by the sweep, got %v", err)

	_, err = mgr.Validate(ctx, live.LeaseID)
	assert.NoError(t, err, "a live lease survives the sweep")
}

func TestReapExpired_PurgesTombstonesPastRetention(t *testing.T) {
	ctx := context.Background()
	mgr := newTestManagerOn(t, kvstore.NewMemory(), time.Hour)

	fresh, err := mgr.CreateLease(ctx, "fresh", time.Hour, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.RevokeLease(ctx, fresh.LeaseID))

	old := time.Now().Add(-2 * time.Hour)
	stale := &Lease{LeaseID: "lease_stale", SecretPath: "stale", ExpiresAt: old, TTL: time.Minute, RevokedAt: &old}
	require.NoError(t, mgr.storeLease(ctx, "test", stale))

	revoked, purged, err := mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, revoked)
	assert.Equal(t, 1, purged)

	_, err = mgr.GetLease(ctx, stale.LeaseID)
	assert.True(t, IsLeaseNotFound(err), "an aged-out tombstone is gone, got %v", err)

	_, err = mgr.GetLease(ctx, fresh.LeaseID)
	assert.NoError(t, err, "a tombstone inside the retention window is kept for audit")
}

func TestReapExpired_BadInput(t *testing.T) {
	ctx := context.Background()

	t.Run("scan fault", func(t *testing.T) {
		store := newFaultKV()
		store.scanErr = &dberrors.MessageError{Message: "kv unavailable"}
		mgr := newTestManagerOn(t, store, 0)

		_, _, err := mgr.ReapExpired(ctx)
		require.Error(t, err)
	})

	t.Run("closed manager", func(t *testing.T) {
		mgr, err := NewManager(&ManagerConfig{Store: kvstore.NewMemory(), Logger: testLogger(t)})
		require.NoError(t, err)
		require.NoError(t, mgr.Close())

		_, _, err = mgr.ReapExpired(ctx)
		require.Error(t, err)
	})
}

// TestStartExpirationLoop_ReapsWithoutSleeping drives the loop on a fast tick
// and waits on a CONDITION (the lease becoming refused), never on a clock.
func TestStartExpirationLoop_ReapsWithoutSleeping(t *testing.T) {
	ctx := context.Background()
	store := kvstore.NewMemory()
	mgr, err := NewManager(&ManagerConfig{
		Store:                   store,
		Logger:                  testLogger(t),
		ExpirationCheckInterval: time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, mgr.Close()) }()

	lapsed := &Lease{LeaseID: "lease_loop", SecretPath: "loop", ExpiresAt: time.Now().Add(-time.Second), TTL: time.Second}
	require.NoError(t, mgr.storeLease(ctx, "test", lapsed))

	require.NoError(t, mgr.StartExpirationLoop(ctx))

	require.Eventually(t, func() bool {
		_, err := mgr.Validate(ctx, lapsed.LeaseID)
		return IsLeaseRevoked(err)
	}, 5*time.Second, time.Millisecond, "the loop must revoke an expired lease")
}

func TestStartExpirationLoop_BadInput(t *testing.T) {
	ctx := context.Background()

	t.Run("already running", func(t *testing.T) {
		mgr := newTestManager(t)
		require.NoError(t, mgr.StartExpirationLoop(ctx))
		err := mgr.StartExpirationLoop(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already running")
	})

	t.Run("closed manager", func(t *testing.T) {
		mgr, err := NewManager(&ManagerConfig{Store: kvstore.NewMemory(), Logger: testLogger(t)})
		require.NoError(t, err)
		require.NoError(t, mgr.Close())

		require.Error(t, mgr.StartExpirationLoop(ctx))
	})
}

// =============================================================================
// Lifecycle
// =============================================================================

func TestClose_IsIdempotentAndRefusesEveryVerb(t *testing.T) {
	ctx := context.Background()
	mgr, err := NewManager(&ManagerConfig{Store: kvstore.NewMemory(), Logger: testLogger(t)})
	require.NoError(t, err)

	require.NoError(t, mgr.Close())
	require.NoError(t, mgr.Close(), "Close is idempotent")

	_, err = mgr.CreateLease(ctx, "p", time.Minute, nil)
	assert.Error(t, err)
	_, err = mgr.GetLease(ctx, "lease_x")
	assert.Error(t, err)
	_, err = mgr.Validate(ctx, "lease_x")
	assert.Error(t, err)
	_, err = mgr.RenewLease(ctx, "lease_x", time.Minute)
	assert.Error(t, err)
	assert.Error(t, mgr.RevokeLease(ctx, "lease_x"))
	_, err = mgr.RevokePrefix(ctx, "p")
	assert.Error(t, err)
	_, err = mgr.ListLeases(ctx, "")
	assert.Error(t, err)
}

func TestClose_StopsTheExpirationLoop(t *testing.T) {
	ctx := context.Background()
	mgr, err := NewManager(&ManagerConfig{
		Store:                   kvstore.NewMemory(),
		Logger:                  testLogger(t),
		ExpirationCheckInterval: time.Millisecond,
	})
	require.NoError(t, err)
	require.NoError(t, mgr.StartExpirationLoop(ctx))
	require.NoError(t, mgr.Close())

	// The loop's own context is cancelled, so a subsequent pass cannot run.
	require.Error(t, mgr.StartExpirationLoop(ctx))
}

func TestConcurrentOperations(t *testing.T) {
	mgr := newTestManager(t)
	ctx := context.Background()

	const workers = 16
	ids := make([]string, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lease, err := mgr.CreateLease(ctx, "concurrent/"+strconv.Itoa(i), time.Hour, nil)
			if err == nil {
				ids[i] = lease.LeaseID
			}
		}(i)
	}
	wg.Wait()

	for _, id := range ids {
		require.NotEmpty(t, id)
	}

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = mgr.RevokeLease(ctx, id)
		}(id)
	}
	wg.Wait()

	for _, id := range ids {
		_, err := mgr.Validate(ctx, id)
		assert.True(t, IsLeaseRevoked(err), "every concurrently revoked lease is refused")
	}
}

// =============================================================================
// Lease value semantics
// =============================================================================

func TestLease_State(t *testing.T) {
	now := time.Now()
	revoked := now.Add(-time.Minute)

	tests := []struct {
		name  string
		lease *Lease
		want  LeaseState
	}{
		{name: "active", lease: &Lease{ExpiresAt: now.Add(time.Hour)}, want: LeaseStateActive},
		{name: "expired", lease: &Lease{ExpiresAt: now.Add(-time.Hour)}, want: LeaseStateExpired},
		{name: "revoked beats expired", lease: &Lease{ExpiresAt: now.Add(-time.Hour), RevokedAt: &revoked}, want: LeaseStateRevoked},
		{name: "revoked beats active", lease: &Lease{ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}, want: LeaseStateRevoked},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.lease.State())
			assert.Equal(t, tc.want == LeaseStateActive, tc.lease.IsValid())
		})
	}
}

func TestLeaseState_String(t *testing.T) {
	assert.Equal(t, "active", LeaseStateActive.String())
	assert.Equal(t, "expired", LeaseStateExpired.String())
	assert.Equal(t, "revoked", LeaseStateRevoked.String())
	assert.Equal(t, "unknown", LeaseState(99).String())
}

func TestLease_CanRenew(t *testing.T) {
	now := time.Now()
	revoked := now

	tests := []struct {
		name  string
		lease *Lease
		want  bool
	}{
		{name: "renewable and active", lease: &Lease{Renewable: true, ExpiresAt: now.Add(time.Hour)}, want: true},
		{name: "not renewable", lease: &Lease{Renewable: false, ExpiresAt: now.Add(time.Hour)}, want: false},
		{name: "expired", lease: &Lease{Renewable: true, ExpiresAt: now.Add(-time.Hour)}, want: false},
		{name: "revoked", lease: &Lease{Renewable: true, ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}, want: false},
		{name: "budget exhausted", lease: &Lease{Renewable: true, ExpiresAt: now.Add(time.Hour), MaxRenewals: 2, RenewCount: 2}, want: false},
		{name: "budget remaining", lease: &Lease{Renewable: true, ExpiresAt: now.Add(time.Hour), MaxRenewals: 2, RenewCount: 1}, want: true},
		{name: "unlimited renewals", lease: &Lease{Renewable: true, ExpiresAt: now.Add(time.Hour), MaxRenewals: 0, RenewCount: 99}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.lease.CanRenew())
		})
	}
}

func TestLease_RemainingTTL(t *testing.T) {
	future := &Lease{ExpiresAt: time.Now().Add(time.Hour)}
	assert.Greater(t, future.RemainingTTL(), 50*time.Minute)

	past := &Lease{ExpiresAt: time.Now().Add(-time.Hour)}
	assert.Equal(t, time.Duration(0), past.RemainingTTL())
}

func TestDefaultLeaseOptions(t *testing.T) {
	opts := DefaultLeaseOptions()
	assert.True(t, opts.Renewable)
	assert.Equal(t, 0, opts.MaxRenewals)
	assert.NotNil(t, opts.Metadata)
}

func TestLease_JSONRoundTrip(t *testing.T) {
	revoked := time.Now().UTC().Truncate(time.Second)
	original := &Lease{
		LeaseID:     "lease_json",
		SecretPath:  "credentials/db/reader",
		IssuedAt:    revoked.Add(-time.Hour),
		ExpiresAt:   revoked.Add(time.Hour),
		TTL:         2 * time.Hour,
		RenewCount:  3,
		MaxRenewals: 10,
		Renewable:   true,
		RevokedAt:   &revoked,
		Metadata:    map[string]string{"account_id": "acct-1"},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Lease
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.LeaseID, decoded.LeaseID)
	assert.Equal(t, original.SecretPath, decoded.SecretPath)
	assert.Equal(t, original.TTL, decoded.TTL)
	assert.Equal(t, original.RenewCount, decoded.RenewCount)
	require.NotNil(t, decoded.RevokedAt)
	assert.Equal(t, revoked.Unix(), decoded.RevokedAt.Unix())
	assert.Equal(t, "acct-1", decoded.Metadata["account_id"])
}

func TestGenerateLeaseID(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		id, err := generateLeaseID()
		require.NoError(t, err)
		assert.Contains(t, id, "lease_")
		assert.Len(t, id, len("lease_")+32)
		_, dup := seen[id]
		require.False(t, dup, "lease IDs must be unique")
		seen[id] = struct{}{}
	}
}

// =============================================================================
// Error vocabulary
// =============================================================================

func TestLeaseErrors_Messages(t *testing.T) {
	at := time.Date(2026, 9, 12, 14, 2, 0, 0, time.UTC)

	assert.Contains(t, (&LeaseNotFoundError{LeaseID: "lease_a"}).Error(), "lease_a")
	assert.Contains(t, (&LeaseRevokedError{LeaseID: "lease_b", RevokedAt: at}).Error(), "2026-09-12T14:02:00Z")
	assert.Contains(t, (&LeaseExpiredError{LeaseID: "lease_c", ExpiredAt: at}).Error(), "lease_c")

	cause := &dberrors.MessageError{Message: "boom"}
	corrupt := &LeaseCorruptError{LeaseID: "lease_d", Cause: cause}
	assert.Contains(t, corrupt.Error(), "boom")
	assert.Equal(t, cause, corrupt.Unwrap())

	storeErr := &LeaseStoreError{Operation: "put", LeaseID: "lease_e", Cause: cause}
	assert.Contains(t, storeErr.Error(), "put")
	assert.Contains(t, storeErr.Error(), "lease_e")
	assert.Equal(t, cause, storeErr.Unwrap())

	anonymous := &LeaseStoreError{Operation: "scan", Cause: cause}
	assert.NotContains(t, anonymous.Error(), " for ")
}

func TestLeaseErrors_Classifiers(t *testing.T) {
	at := time.Now()

	assert.True(t, IsLeaseNotFound(NewLeaseNotFoundError("op", "lease_a")))
	assert.True(t, IsLeaseRevoked(NewLeaseRevokedError("op", "lease_b", at)))
	assert.True(t, IsLeaseExpired(NewLeaseExpiredError("op", "lease_c", at)))

	// Each classifier answers only for its own fact.
	assert.False(t, IsLeaseRevoked(NewLeaseNotFoundError("op", "lease_a")))
	assert.False(t, IsLeaseExpired(NewLeaseRevokedError("op", "lease_b", at)))
	assert.False(t, IsLeaseNotFound(NewLeaseExpiredError("op", "lease_c", at)))

	// And none of them claims a nil or unrelated error.
	assert.False(t, IsLeaseNotFound(nil))
	assert.False(t, IsLeaseRevoked(&dberrors.MessageError{Message: "unrelated"}))
	assert.False(t, IsLeaseExpired(&dberrors.MessageError{Message: "unrelated"}))
}

func TestLeaseErrors_CarryTransportCodes(t *testing.T) {
	assert.True(t, dberrors.IsNotFound(NewLeaseNotFoundError("op", "lease_a")))

	revoked := NewLeaseRevokedError("op", "lease_b", time.Now())
	assert.Equal(t, dberrors.ErrNotAllowed, revoked.Code)

	expired := NewLeaseExpiredError("op", "lease_c", time.Now())
	assert.Equal(t, dberrors.ErrNotAllowed, expired.Code)

	corrupt := NewLeaseCorruptError("op", "lease_d", &dberrors.MessageError{Message: "boom"})
	assert.Equal(t, dberrors.ErrInternal, corrupt.Code)

	storeErr := NewLeaseStoreError("op", "put", "lease_e", &dberrors.MessageError{Message: "boom"})
	assert.Equal(t, dberrors.ErrInternal, storeErr.Code)
}

// =============================================================================
// Store faults on the paths a sweep and a prefix revocation take
// =============================================================================

func TestReapExpired_PurgeFaultDoesNotStopThePass(t *testing.T) {
	ctx := context.Background()
	store := newFaultKV()
	mgr := newTestManagerOn(t, store, time.Hour)

	// Two aged-out tombstones; deletes are armed to fail, so neither is purged
	// and the pass still reports cleanly rather than aborting.
	old := time.Now().Add(-2 * time.Hour)
	for _, id := range []string{"lease_stale_a", "lease_stale_b"} {
		require.NoError(t, mgr.storeLease(ctx, "test", &Lease{
			LeaseID: id, SecretPath: "p", ExpiresAt: old, TTL: time.Minute, RevokedAt: &old,
		}))
	}
	store.deleteErr = &dberrors.MessageError{Message: "kv unavailable"}

	revoked, purged, err := mgr.ReapExpired(ctx)
	require.NoError(t, err, "a purge fault is bookkeeping, not an outage of the pass")
	assert.Equal(t, 0, revoked)
	assert.Equal(t, 0, purged)

	// The tombstones are still there, so a later pass retries the purge.
	leases, err := mgr.ListLeases(ctx, "")
	require.NoError(t, err)
	assert.Len(t, leases, 2)
}

func TestReapExpired_RevokeFaultDoesNotStopThePass(t *testing.T) {
	ctx := context.Background()
	store := newFaultKV()
	mgr := newTestManagerOn(t, store, 0)

	require.NoError(t, mgr.storeLease(ctx, "test", &Lease{
		LeaseID: "lease_lapsed_a", SecretPath: "p", ExpiresAt: time.Now().Add(-time.Minute), TTL: time.Minute,
	}))
	require.NoError(t, mgr.storeLease(ctx, "test", &Lease{
		LeaseID: "lease_lapsed_b", SecretPath: "p", ExpiresAt: time.Now().Add(-time.Minute), TTL: time.Minute,
	}))
	store.putErr = &dberrors.MessageError{Message: "kv unavailable"}

	revoked, _, err := mgr.ReapExpired(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, revoked, "a tombstone that could not be written is not a revocation")
}

func TestRevokePrefix_ReportsOnlyWhatItWithdrew(t *testing.T) {
	ctx := context.Background()
	store := newFaultKV()
	mgr := newTestManagerOn(t, store, 0)

	for i := 0; i < 2; i++ {
		_, err := mgr.CreateLease(ctx, "credentials/db/"+strconv.Itoa(i), time.Hour, nil)
		require.NoError(t, err)
	}
	store.putErr = &dberrors.MessageError{Message: "kv unavailable"}

	count, err := mgr.RevokePrefix(ctx, "credentials/db/")
	require.NoError(t, err, "a prefix revocation reports what it managed, it does not abort")
	assert.Equal(t, 0, count)
}

func TestRevoke_StoreFaultIsNotAnAbsence(t *testing.T) {
	ctx := context.Background()
	store := newFaultKV()
	mgr := newTestManagerOn(t, store, 0)

	created, err := mgr.CreateLease(ctx, "p", time.Hour, nil)
	require.NoError(t, err)

	// A read that FAILED must not be mistaken for "the grant is already gone":
	// that would report a successful revocation for a grant still live.
	store.getErr = &dberrors.MessageError{Message: "kv unavailable"}
	err = mgr.RevokeLease(ctx, created.LeaseID)
	require.Error(t, err)
	var storeErr *LeaseStoreError
	require.ErrorAs(t, err, &storeErr)
	assert.Equal(t, "get", storeErr.Operation)
}
