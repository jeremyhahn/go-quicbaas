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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"encoding/json"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

func setupLeaseManager(t *testing.T) (*Manager, func()) {
	// Create logger
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Create mock storage
	store := newMockStorage()

	// Create lease manager with logger adapter
	config := &ManagerConfig{
		Store:                   store,
		Logger:                  logger.NewAdapter(log),
		ExpirationCheckInterval: 100 * time.Millisecond,
	}

	mgr, err := NewManager(config)
	require.NoError(t, err)

	cleanup := func() {
		mgr.Close()
		store.Close()
	}

	return mgr, cleanup
}

func TestLeaseManager_CreateLease(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful creation", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, lease.LeaseID)
		assert.Equal(t, "test/path", lease.SecretPath)
		assert.True(t, lease.IsValid())
		assert.True(t, lease.Renewable)
		assert.Equal(t, 5*time.Minute, lease.TTL)
	})

	t.Run("with custom options", func(t *testing.T) {
		opts := &LeaseOptions{
			Renewable:   false,
			MaxRenewals: 5,
			Metadata: map[string]string{
				"role": "test-role",
			},
		}

		lease, err := mgr.CreateLease(ctx, "test/path2", 10*time.Minute, opts)
		require.NoError(t, err)
		assert.False(t, lease.Renewable)
		assert.Equal(t, 5, lease.MaxRenewals)
		assert.Equal(t, "test-role", lease.Metadata["role"])
	})

	t.Run("empty path error", func(t *testing.T) {
		_, err := mgr.CreateLease(ctx, "", 5*time.Minute, nil)
		assert.Error(t, err)
	})

	t.Run("TTL too short error", func(t *testing.T) {
		_, err := mgr.CreateLease(ctx, "test/path", 500*time.Millisecond, nil)
		assert.Error(t, err)
	})

	t.Run("TTL too long error", func(t *testing.T) {
		_, err := mgr.CreateLease(ctx, "test/path", 400*24*time.Hour, nil)
		assert.Error(t, err)
	})
}

func TestLeaseManager_GetLease(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("get existing lease", func(t *testing.T) {
		// Create lease
		created, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		require.NoError(t, err)

		// Get lease
		retrieved, err := mgr.GetLease(ctx, created.LeaseID)
		require.NoError(t, err)
		assert.Equal(t, created.LeaseID, retrieved.LeaseID)
		assert.Equal(t, created.SecretPath, retrieved.SecretPath)
	})

	t.Run("get non-existent lease", func(t *testing.T) {
		_, err := mgr.GetLease(ctx, "lease_nonexistent")
		assert.Error(t, err)
	})

	t.Run("empty lease ID error", func(t *testing.T) {
		_, err := mgr.GetLease(ctx, "")
		assert.Error(t, err)
	})
}

func TestLeaseManager_RenewLease(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful renewal", func(t *testing.T) {
		// Create renewable lease
		opts := &LeaseOptions{
			Renewable:   true,
			MaxRenewals: 3,
		}
		lease, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, opts)
		require.NoError(t, err)

		originalExpiry := lease.ExpiresAt

		// Wait a bit
		time.Sleep(100 * time.Millisecond)

		// Renew lease
		renewed, err := mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		require.NoError(t, err)
		assert.Equal(t, 1, renewed.RenewCount)
		assert.True(t, renewed.ExpiresAt.After(originalExpiry))
	})

	t.Run("renewal with default TTL", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/path2", 10*time.Minute, nil)
		require.NoError(t, err)

		// Renew with 0 increment (should use original TTL)
		renewed, err := mgr.RenewLease(ctx, lease.LeaseID, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, renewed.RenewCount)
	})

	t.Run("cannot renew non-renewable lease", func(t *testing.T) {
		opts := &LeaseOptions{
			Renewable: false,
		}
		lease, err := mgr.CreateLease(ctx, "test/path3", 5*time.Minute, opts)
		require.NoError(t, err)

		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		assert.Error(t, err)
	})

	t.Run("cannot exceed max renewals", func(t *testing.T) {
		opts := &LeaseOptions{
			Renewable:   true,
			MaxRenewals: 2,
		}
		lease, err := mgr.CreateLease(ctx, "test/path4", 5*time.Minute, opts)
		require.NoError(t, err)

		// First renewal
		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		require.NoError(t, err)

		// Second renewal
		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		require.NoError(t, err)

		// Third renewal should fail
		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		assert.Error(t, err)
	})
}

func TestLeaseManager_RevokeLease(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("successful revocation", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		require.NoError(t, err)

		err = mgr.RevokeLease(ctx, lease.LeaseID)
		require.NoError(t, err)

		// Verify lease is gone
		_, err = mgr.GetLease(ctx, lease.LeaseID)
		assert.Error(t, err)
	})

	t.Run("revoke non-existent lease", func(t *testing.T) {
		// RevokeLease is idempotent - revoking non-existent lease should succeed
		err := mgr.RevokeLease(ctx, "lease_nonexistent")
		assert.NoError(t, err)
	})
}

func TestLeaseManager_RevokePrefix(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("revoke by prefix", func(t *testing.T) {
		// Create multiple leases
		_, err := mgr.CreateLease(ctx, "app1/db/creds", 5*time.Minute, nil)
		require.NoError(t, err)
		_, err = mgr.CreateLease(ctx, "app1/db/creds", 5*time.Minute, nil)
		require.NoError(t, err)
		_, err = mgr.CreateLease(ctx, "app2/db/creds", 5*time.Minute, nil)
		require.NoError(t, err)

		// Revoke app1 leases
		count, err := mgr.RevokePrefix(ctx, "app1/")
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Verify app2 lease still exists
		leases, err := mgr.ListLeases(ctx, "app2/")
		require.NoError(t, err)
		assert.Equal(t, 1, len(leases))
	})
}

func TestLeaseManager_ListLeases(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("list all leases", func(t *testing.T) {
		// Create leases
		_, err := mgr.CreateLease(ctx, "app1/path", 5*time.Minute, nil)
		require.NoError(t, err)
		_, err = mgr.CreateLease(ctx, "app2/path", 5*time.Minute, nil)
		require.NoError(t, err)

		leases, err := mgr.ListLeases(ctx, "")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(leases), 2)
	})

	t.Run("list with prefix filter", func(t *testing.T) {
		leases, err := mgr.ListLeases(ctx, "app1/")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(leases), 1)
		for _, lease := range leases {
			assert.Contains(t, lease.SecretPath, "app1/")
		}
	})
}

func TestLeaseManager_ExpirationLoop(t *testing.T) {
	t.Run("automatic expiration", func(t *testing.T) {
		mgr, cleanup := setupLeaseManager(t)
		defer cleanup()

		ctx := context.Background()

		// Start expiration loop
		err := mgr.StartExpirationLoop(context.Background())
		require.NoError(t, err)

		// Create a lease with minimum TTL (1 second)
		lease, err := mgr.CreateLease(ctx, "test/expire", 1*time.Second, nil)
		require.NoError(t, err)

		// Wait for the lease to expire (1 second)
		time.Sleep(1100 * time.Millisecond)

		// Manually trigger expiration check since the loop may not run in time
		// This verifies the expiration mechanism works correctly
		mgr.checkExpiredLeases()

		// Verify lease was revoked
		_, leaseErr := mgr.GetLease(ctx, lease.LeaseID)
		assert.Error(t, leaseErr, "lease should be revoked after expiration")
	})

	t.Run("cannot start twice", func(t *testing.T) {
		mgr, cleanup := setupLeaseManager(t)
		defer cleanup()

		// First start should succeed
		err := mgr.StartExpirationLoop(context.Background())
		require.NoError(t, err)

		// Second start should fail
		err = mgr.StartExpirationLoop(context.Background())
		assert.Error(t, err)
	})
}

func TestLease_State(t *testing.T) {
	now := time.Now()

	t.Run("active lease", func(t *testing.T) {
		lease := &Lease{
			IssuedAt:  now,
			ExpiresAt: now.Add(5 * time.Minute),
		}
		assert.Equal(t, LeaseStateActive, lease.State())
		assert.True(t, lease.IsValid())
	})

	t.Run("expired lease", func(t *testing.T) {
		lease := &Lease{
			IssuedAt:  now.Add(-10 * time.Minute),
			ExpiresAt: now.Add(-5 * time.Minute),
		}
		assert.Equal(t, LeaseStateExpired, lease.State())
		assert.False(t, lease.IsValid())
	})

	t.Run("revoked lease", func(t *testing.T) {
		revoked := now
		lease := &Lease{
			IssuedAt:  now,
			ExpiresAt: now.Add(5 * time.Minute),
			RevokedAt: &revoked,
		}
		assert.Equal(t, LeaseStateRevoked, lease.State())
		assert.False(t, lease.IsValid())
	})
}

func TestLease_RemainingTTL(t *testing.T) {
	now := time.Now()

	t.Run("active lease", func(t *testing.T) {
		lease := &Lease{
			ExpiresAt: now.Add(5 * time.Minute),
		}
		remaining := lease.RemainingTTL()
		assert.Greater(t, remaining, 4*time.Minute)
		assert.LessOrEqual(t, remaining, 5*time.Minute)
	})

	t.Run("expired lease", func(t *testing.T) {
		lease := &Lease{
			ExpiresAt: now.Add(-5 * time.Minute),
		}
		remaining := lease.RemainingTTL()
		assert.Equal(t, time.Duration(0), remaining)
	})
}

// Additional tests for edge cases and increased coverage

func TestNewManager_ValidationErrors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("nil config", func(t *testing.T) {
		_, err := NewManager(nil)
		assert.Error(t, err)
	})

	t.Run("nil store", func(t *testing.T) {
		config := &ManagerConfig{
			Logger: logger.NewAdapter(log),
		}
		_, err := NewManager(config)
		assert.Error(t, err)
	})

	t.Run("nil logger", func(t *testing.T) {
		store := newMockStorage()
		defer store.Close()

		config := &ManagerConfig{
			Store: store,
		}
		_, err := NewManager(config)
		assert.Error(t, err)
	})
}

func TestLeaseManager_ContextCancellation(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	t.Run("create with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		assert.Error(t, err)
	})

	t.Run("renew with cancelled context", func(t *testing.T) {
		// Create lease first
		lease, err := mgr.CreateLease(context.Background(), "test/path", 5*time.Minute, nil)
		require.NoError(t, err)

		// Try to renew with cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		assert.Error(t, err)
	})

	t.Run("get with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := mgr.GetLease(ctx, "lease_test")
		assert.Error(t, err)
	})

	t.Run("revoke with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := mgr.RevokeLease(ctx, "lease_test")
		assert.Error(t, err)
	})

	t.Run("revoke prefix with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := mgr.RevokePrefix(ctx, "test/")
		assert.Error(t, err)
	})

	t.Run("list with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := mgr.ListLeases(ctx, "")
		assert.Error(t, err)
	})
}

func TestLeaseManager_ClosedManager(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	cleanup() // Close the manager immediately

	ctx := context.Background()

	t.Run("create on closed manager", func(t *testing.T) {
		_, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		assert.Error(t, err)
	})

	t.Run("renew on closed manager", func(t *testing.T) {
		_, err := mgr.RenewLease(ctx, "lease_test", 5*time.Minute)
		assert.Error(t, err)
	})

	t.Run("get on closed manager", func(t *testing.T) {
		_, err := mgr.GetLease(ctx, "lease_test")
		assert.Error(t, err)
	})

	t.Run("revoke on closed manager", func(t *testing.T) {
		err := mgr.RevokeLease(ctx, "lease_test")
		assert.Error(t, err)
	})

	t.Run("revoke prefix on closed manager", func(t *testing.T) {
		_, err := mgr.RevokePrefix(ctx, "test/")
		assert.Error(t, err)
	})

	t.Run("list on closed manager", func(t *testing.T) {
		_, err := mgr.ListLeases(ctx, "")
		assert.Error(t, err)
	})

	t.Run("start expiration loop on closed manager", func(t *testing.T) {
		err := mgr.StartExpirationLoop(ctx)
		assert.Error(t, err)
	})
}

func TestLeaseManager_RenewLeaseEdgeCases(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("empty lease ID", func(t *testing.T) {
		_, err := mgr.RenewLease(ctx, "", 5*time.Minute)
		assert.Error(t, err)
	})

	t.Run("non-existent lease", func(t *testing.T) {
		_, err := mgr.RenewLease(ctx, "lease_nonexistent", 5*time.Minute)
		assert.Error(t, err)
	})
}

func TestLeaseManager_RevokePrefixEdgeCases(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("revoke with empty prefix", func(t *testing.T) {
		// Create some leases
		_, err := mgr.CreateLease(ctx, "test/path1", 5*time.Minute, nil)
		require.NoError(t, err)
		_, err = mgr.CreateLease(ctx, "test/path2", 5*time.Minute, nil)
		require.NoError(t, err)

		// Revoke all leases (empty prefix)
		count, err := mgr.RevokePrefix(ctx, "")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 2)
	})

	t.Run("revoke non-matching prefix", func(t *testing.T) {
		count, err := mgr.RevokePrefix(ctx, "nonexistent/prefix/")
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestLeaseManager_ListLeasesEdgeCases(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("list with no matching leases", func(t *testing.T) {
		leases, err := mgr.ListLeases(ctx, "nonexistent/prefix/")
		require.NoError(t, err)
		assert.Equal(t, 0, len(leases))
	})

	t.Run("list empty database", func(t *testing.T) {
		// Use a fresh manager
		log, err := logger.New(logger.LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		store := newMockStorage()
		defer store.Close()

		config := &ManagerConfig{
			Store:  store,
			Logger: logger.NewAdapter(log),
		}

		mgr2, err := NewManager(config)
		require.NoError(t, err)
		defer mgr2.Close()

		leases, err := mgr2.ListLeases(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, 0, len(leases))
	})
}

func TestLease_CanRenewEdgeCases(t *testing.T) {
	now := time.Now()

	t.Run("expired lease cannot renew", func(t *testing.T) {
		lease := &Lease{
			Renewable: true,
			IssuedAt:  now.Add(-10 * time.Minute),
			ExpiresAt: now.Add(-5 * time.Minute),
		}
		assert.False(t, lease.CanRenew())
	})

	t.Run("revoked lease cannot renew", func(t *testing.T) {
		revoked := now
		lease := &Lease{
			Renewable: true,
			IssuedAt:  now,
			ExpiresAt: now.Add(5 * time.Minute),
			RevokedAt: &revoked,
		}
		assert.False(t, lease.CanRenew())
	})

	t.Run("max renewals reached", func(t *testing.T) {
		lease := &Lease{
			Renewable:   true,
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
			MaxRenewals: 5,
			RenewCount:  5,
		}
		assert.False(t, lease.CanRenew())
	})

	t.Run("unlimited renewals", func(t *testing.T) {
		lease := &Lease{
			Renewable:   true,
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
			MaxRenewals: 0, // unlimited
			RenewCount:  100,
		}
		assert.True(t, lease.CanRenew())
	})
}

func TestLeaseState_String(t *testing.T) {
	t.Run("active state string", func(t *testing.T) {
		assert.Equal(t, "active", LeaseStateActive.String())
	})

	t.Run("expired state string", func(t *testing.T) {
		assert.Equal(t, "expired", LeaseStateExpired.String())
	})

	t.Run("revoked state string", func(t *testing.T) {
		assert.Equal(t, "revoked", LeaseStateRevoked.String())
	})

	t.Run("unknown state string", func(t *testing.T) {
		var unknownState LeaseState = 999
		assert.Equal(t, "unknown", unknownState.String())
	})
}

func TestDefaultLeaseOptions(t *testing.T) {
	opts := DefaultLeaseOptions()
	assert.True(t, opts.Renewable)
	assert.Equal(t, 0, opts.MaxRenewals)
	assert.NotNil(t, opts.Metadata)
	assert.Equal(t, 0, len(opts.Metadata))
}

func TestLeaseManager_RevokeLeaseTwice(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	lease, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
	require.NoError(t, err)

	// First revoke
	err = mgr.RevokeLease(ctx, lease.LeaseID)
	require.NoError(t, err)

	// Second revoke (should be idempotent)
	err = mgr.RevokeLease(ctx, lease.LeaseID)
	assert.NoError(t, err)
}

func TestLeaseManager_RevokeLeaseEmptyID(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	err := mgr.RevokeLease(ctx, "")
	assert.Error(t, err)
}

func TestLeaseManager_ConcurrentOperations(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("concurrent creates", func(t *testing.T) {
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func(idx int) {
				_, err := mgr.CreateLease(ctx, "concurrent/test", 5*time.Minute, nil)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("concurrent renewals", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/renew", 10*time.Minute, nil)
		require.NoError(t, err)

		done := make(chan bool, 5)
		for i := 0; i < 5; i++ {
			go func() {
				_, _ = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
				done <- true
			}()
		}

		for i := 0; i < 5; i++ {
			<-done
		}
	})

	t.Run("concurrent revokes", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/revoke", 5*time.Minute, nil)
		require.NoError(t, err)

		done := make(chan bool, 5)
		for i := 0; i < 5; i++ {
			go func() {
				_ = mgr.RevokeLease(ctx, lease.LeaseID)
				done <- true
			}()
		}

		for i := 0; i < 5; i++ {
			<-done
		}
	})
}

func TestLeaseManager_CheckExpiredLeases(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("check expired leases manually", func(t *testing.T) {
		// Create an expired lease manually by manipulating the data
		lease := &Lease{
			LeaseID:    "lease_expired_test",
			SecretPath: "test/expired",
			IssuedAt:   time.Now().Add(-10 * time.Minute),
			ExpiresAt:  time.Now().Add(-5 * time.Minute),
			TTL:        5 * time.Minute,
			Renewable:  true,
		}

		// Store it directly
		err := mgr.storeLease(lease)
		require.NoError(t, err)

		// Manually trigger expiration check
		mgr.checkExpiredLeases()

		// Verify the lease was revoked
		_, err = mgr.GetLease(ctx, lease.LeaseID)
		assert.Error(t, err)
	})
}

// NOTE: Timing-sensitive expiration loop tests are covered by
// TestLeaseManager_ExpirationLoop and TestLeaseManager_CheckExpiredLeases

func TestLeaseManager_MultipleClose(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)

	// First close
	cleanup()

	// Second close (should be idempotent via closeOnce)
	err := mgr.Close()
	assert.NoError(t, err)
}

func TestLeaseManager_BoundaryTTLValues(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("minimum TTL", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/min-ttl", MinLeaseTTL, nil)
		require.NoError(t, err)
		assert.Equal(t, MinLeaseTTL, lease.TTL)
	})

	t.Run("maximum TTL", func(t *testing.T) {
		lease, err := mgr.CreateLease(ctx, "test/max-ttl", MaxLeaseTTL, nil)
		require.NoError(t, err)
		assert.Equal(t, MaxLeaseTTL, lease.TTL)
	})

	t.Run("just below minimum", func(t *testing.T) {
		_, err := mgr.CreateLease(ctx, "test/below-min", MinLeaseTTL-1, nil)
		assert.Error(t, err)
	})

	t.Run("just above maximum", func(t *testing.T) {
		_, err := mgr.CreateLease(ctx, "test/above-max", MaxLeaseTTL+1, nil)
		assert.Error(t, err)
	})
}

func TestLeaseManager_StorageErrors(t *testing.T) {
	t.Run("invalid lease data in storage", func(t *testing.T) {
		log, err := logger.New(logger.LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		store := newMockStorage()
		defer store.Close()

		// Store invalid JSON
		invalidData := []byte("this is not valid json")
		err = store.Set([]byte("/_leases/lease_invalid"), invalidData)
		require.NoError(t, err)

		config := &ManagerConfig{
			Store:  store,
			Logger: logger.NewAdapter(log),
		}

		mgr, err := NewManager(config)
		require.NoError(t, err)
		defer mgr.Close()

		// List should skip invalid lease
		leases, err := mgr.ListLeases(context.Background(), "")
		require.NoError(t, err)
		// Should not include the invalid lease
		for _, l := range leases {
			assert.NotEqual(t, "lease_invalid", l.LeaseID)
		}
	})
}

func TestLeaseManager_ExpirationLoopWithMultipleLeases(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("multiple expired leases", func(t *testing.T) {
		// Create multiple expired leases
		for i := 0; i < 5; i++ {
			lease := &Lease{
				LeaseID:    fmt.Sprintf("lease_expired_%d", i),
				SecretPath: fmt.Sprintf("test/expired/%d", i),
				IssuedAt:   time.Now().Add(-10 * time.Minute),
				ExpiresAt:  time.Now().Add(-5 * time.Minute),
				TTL:        5 * time.Minute,
				Renewable:  true,
			}
			err := mgr.storeLease(lease)
			require.NoError(t, err)
		}

		// Manually trigger expiration check
		mgr.checkExpiredLeases()

		// Verify all leases were revoked
		for i := 0; i < 5; i++ {
			_, err := mgr.GetLease(ctx, fmt.Sprintf("lease_expired_%d", i))
			assert.Error(t, err)
		}
	})
}

// =============================================================================
// Consolidated from manager_additional_test.go
// =============================================================================

// TestMockIterator_AdditionalMethods tests uncovered iterator methods
func TestMockIterator_AdditionalMethods(t *testing.T) {
	store := newMockStorage()
	defer store.Close()

	// Add test data
	store.Set([]byte("key1"), []byte("value1"))
	store.Set([]byte("key2"), []byte("value2"))
	store.Set([]byte("key3"), []byte("value3"))

	t.Run("SeekToFirst", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		ok := iter.SeekToFirst()
		assert.True(t, ok)
		assert.Equal(t, []byte("key1"), iter.Key())
		assert.Equal(t, []byte("value1"), iter.Value())
	})

	t.Run("SeekToLast", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		ok := iter.SeekToLast()
		assert.True(t, ok)
		assert.Equal(t, []byte("key3"), iter.Key())
		assert.Equal(t, []byte("value3"), iter.Value())
	})

	t.Run("Seek", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		// Seek to existing key
		ok := iter.Seek([]byte("key2"))
		assert.True(t, ok)
		assert.Equal(t, []byte("key2"), iter.Key())
		assert.Equal(t, []byte("value2"), iter.Value())

		// Seek to non-existing key (should go to next)
		ok = iter.Seek([]byte("key1.5"))
		assert.True(t, ok)
		assert.Equal(t, []byte("key2"), iter.Key())

		// Seek past all keys
		ok = iter.Seek([]byte("key9"))
		assert.False(t, ok)
	})

	t.Run("Prev", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		// Move to last
		iter.SeekToLast()
		assert.Equal(t, []byte("key3"), iter.Key())

		// Go back
		ok := iter.Prev()
		assert.True(t, ok)
		assert.Equal(t, []byte("key2"), iter.Key())

		// Go back again
		ok = iter.Prev()
		assert.True(t, ok)
		assert.Equal(t, []byte("key1"), iter.Key())

		// Can't go back further
		ok = iter.Prev()
		assert.False(t, ok)
	})

	t.Run("Key on invalid iterator", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		// Before first position
		key := iter.Key()
		assert.Nil(t, key)
	})

	t.Run("Value on invalid iterator", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		// Before first position
		value := iter.Value()
		assert.Nil(t, value)
	})

	t.Run("Error always returns nil", func(t *testing.T) {
		iter := store.NewIterator(nil)
		defer iter.Close()

		err := iter.Error()
		assert.NoError(t, err)
	})

	t.Run("SeekToFirst on empty store", func(t *testing.T) {
		emptyStore := newMockStorage()
		defer emptyStore.Close()

		iter := emptyStore.NewIterator(nil)
		defer iter.Close()

		ok := iter.SeekToFirst()
		assert.False(t, ok)
	})

	t.Run("SeekToLast on empty store", func(t *testing.T) {
		emptyStore := newMockStorage()
		defer emptyStore.Close()

		iter := emptyStore.NewIterator(nil)
		defer iter.Close()

		ok := iter.SeekToLast()
		assert.False(t, ok)
	})
}

// TestMockBatch_Operations tests batch operations
func TestMockBatch_Operations(t *testing.T) {
	store := newMockStorage()
	defer store.Close()

	t.Run("batch set and commit", func(t *testing.T) {
		batch := store.Batch()
		defer batch.Close()

		err := batch.Set([]byte("batch_key1"), []byte("batch_value1"))
		assert.NoError(t, err)

		err = batch.Set([]byte("batch_key2"), []byte("batch_value2"))
		assert.NoError(t, err)

		// Values shouldn't be in store yet
		_, err = store.Get([]byte("batch_key1"))
		assert.Error(t, err)

		// Commit batch
		err = batch.Commit()
		assert.NoError(t, err)

		// Values should now be in store
		val, err := store.Get([]byte("batch_key1"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("batch_value1"), val)

		val, err = store.Get([]byte("batch_key2"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("batch_value2"), val)
	})

	t.Run("batch delete and commit", func(t *testing.T) {
		// Add a key first
		store.Set([]byte("delete_key"), []byte("delete_value"))

		batch := store.Batch()
		defer batch.Close()

		err := batch.Delete([]byte("delete_key"))
		assert.NoError(t, err)

		// Key should still exist before commit
		_, err = store.Get([]byte("delete_key"))
		assert.NoError(t, err)

		// Commit batch
		err = batch.Commit()
		assert.NoError(t, err)

		// Key should now be deleted
		_, err = store.Get([]byte("delete_key"))
		assert.Error(t, err)
	})

	t.Run("batch rollback", func(t *testing.T) {
		batch := store.Batch()
		defer batch.Close()

		err := batch.Set([]byte("rollback_key"), []byte("rollback_value"))
		assert.NoError(t, err)

		// Rollback
		err = batch.Rollback()
		assert.NoError(t, err)

		// Value should not be in store
		_, err = store.Get([]byte("rollback_key"))
		assert.Error(t, err)
	})

	t.Run("batch close without commit", func(t *testing.T) {
		batch := store.Batch()

		err := batch.Set([]byte("close_key"), []byte("close_value"))
		assert.NoError(t, err)

		// Close without commit (should rollback)
		err = batch.Close()
		assert.NoError(t, err)

		// Value should not be in store
		_, err = store.Get([]byte("close_key"))
		assert.Error(t, err)
	})

	t.Run("multiple commits", func(t *testing.T) {
		batch := store.Batch()
		defer batch.Close()

		err := batch.Set([]byte("multi_key"), []byte("multi_value"))
		assert.NoError(t, err)

		err = batch.Commit()
		assert.NoError(t, err)

		// Second commit should work (ops cleared)
		err = batch.Commit()
		assert.NoError(t, err)
	})
}

// TestMockStorage_Snapshot tests snapshot functionality
func TestMockStorage_Snapshot(t *testing.T) {
	store := newMockStorage()
	defer store.Close()

	snapshot, err := store.Snapshot()
	assert.NoError(t, err)
	assert.Nil(t, snapshot) // Mock implementation returns nil
}

// TestStoreLease_Errors tests error paths in storeLease
func TestStoreLease_Errors(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	t.Run("store lease successfully", func(t *testing.T) {
		lease := &Lease{
			LeaseID:    "test_lease",
			SecretPath: "test/path",
			IssuedAt:   time.Now(),
			ExpiresAt:  time.Now().Add(5 * time.Minute),
			TTL:        5 * time.Minute,
			Renewable:  true,
		}

		err := mgr.storeLease(lease)
		assert.NoError(t, err)

		// Verify it can be loaded
		loaded, err := mgr.loadLease("test_lease")
		assert.NoError(t, err)
		assert.Equal(t, lease.LeaseID, loaded.LeaseID)
	})
}

// TestLoadLease_Errors tests error paths in loadLease
func TestLoadLease_Errors(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	t.Run("load non-existent lease", func(t *testing.T) {
		_, err := mgr.loadLease("nonexistent_lease")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get lease")
	})

	t.Run("load corrupted lease data", func(t *testing.T) {
		// Directly store invalid JSON
		key := []byte(LeasePrefix + "corrupted_lease")
		err := mgr.store.Set(key, []byte("invalid json data"))
		require.NoError(t, err)

		_, err = mgr.loadLease("corrupted_lease")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal lease")
	})
}

// TestGenerateLeaseID tests lease ID generation
func TestGenerateLeaseID(t *testing.T) {
	t.Run("successful ID generation", func(t *testing.T) {
		id, err := generateLeaseID()
		assert.NoError(t, err)
		assert.NotEmpty(t, id)
		assert.Contains(t, id, "lease_")
		// Should be lease_ + 32 hex characters (16 bytes * 2)
		assert.Len(t, id, len("lease_")+32)
	})

	t.Run("unique IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id, err := generateLeaseID()
			assert.NoError(t, err)
			assert.False(t, ids[id], "duplicate ID generated")
			ids[id] = true
		}
	})
}

// TestCheckExpiredLeases_Scenarios tests various expiration scenarios
func TestCheckExpiredLeases_Scenarios(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("mixed active and expired leases", func(t *testing.T) {
		// Create active leases
		_, err := mgr.CreateLease(ctx, "active/lease1", 10*time.Minute, nil)
		require.NoError(t, err)
		_, err = mgr.CreateLease(ctx, "active/lease2", 10*time.Minute, nil)
		require.NoError(t, err)

		// Create expired leases
		expiredLease1 := &Lease{
			LeaseID:    "lease_expired_1",
			SecretPath: "expired/lease1",
			IssuedAt:   time.Now().Add(-10 * time.Minute),
			ExpiresAt:  time.Now().Add(-5 * time.Minute),
			TTL:        5 * time.Minute,
			Renewable:  true,
		}
		err = mgr.storeLease(expiredLease1)
		require.NoError(t, err)

		expiredLease2 := &Lease{
			LeaseID:    "lease_expired_2",
			SecretPath: "expired/lease2",
			IssuedAt:   time.Now().Add(-10 * time.Minute),
			ExpiresAt:  time.Now().Add(-5 * time.Minute),
			TTL:        5 * time.Minute,
			Renewable:  true,
		}
		err = mgr.storeLease(expiredLease2)
		require.NoError(t, err)

		// Check expired leases
		mgr.checkExpiredLeases()

		// Verify expired leases were removed
		_, err = mgr.GetLease(ctx, "lease_expired_1")
		assert.Error(t, err)
		_, err = mgr.GetLease(ctx, "lease_expired_2")
		assert.Error(t, err)

		// Verify active leases still exist
		leases, err := mgr.ListLeases(ctx, "active/")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(leases), 2)
	})

	t.Run("revoked leases are not expired", func(t *testing.T) {
		now := time.Now()
		revokedLease := &Lease{
			LeaseID:    "lease_revoked",
			SecretPath: "revoked/lease",
			IssuedAt:   now.Add(-10 * time.Minute),
			ExpiresAt:  now.Add(-5 * time.Minute),
			TTL:        5 * time.Minute,
			Renewable:  true,
			RevokedAt:  &now,
		}
		err := mgr.storeLease(revokedLease)
		require.NoError(t, err)

		// Revoked leases have RevokedAt set, so State() returns LeaseStateRevoked
		// not LeaseStateExpired, so checkExpiredLeases won't touch them
		mgr.checkExpiredLeases()

		// Lease should still exist (wasn't removed by expiration check)
		retrieved, err := mgr.GetLease(ctx, "lease_revoked")
		require.NoError(t, err)
		assert.Equal(t, LeaseStateRevoked, retrieved.State())
	})
}

// TestExpirationLoop_EdgeCases tests expiration loop edge cases
func TestExpirationLoop_EdgeCases(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	store := newMockStorage()
	defer store.Close()

	config := &ManagerConfig{
		Store:                   store,
		Logger:                  logger.NewAdapter(log),
		ExpirationCheckInterval: 50 * time.Millisecond,
	}

	mgr, err := NewManager(config)
	require.NoError(t, err)
	defer mgr.Close()

	t.Run("expiration loop stops on context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		err := mgr.StartExpirationLoop(ctx)
		require.NoError(t, err)

		// Cancel context after a short time
		time.Sleep(100 * time.Millisecond)
		cancel()

		// Wait for loop to stop
		time.Sleep(100 * time.Millisecond)

		// Loop should have stopped gracefully
		// No good way to verify this other than ensuring no panic/deadlock
	})

	t.Run("expiration loop handles corrupted lease data", func(t *testing.T) {
		// Store invalid lease data
		key := []byte(LeasePrefix + "invalid_lease")
		err := store.Set(key, []byte("not valid json"))
		require.NoError(t, err)

		// Run expiration check - should log warning and continue
		mgr.checkExpiredLeases()

		// Shouldn't crash or panic
		leases, err := mgr.ListLeases(context.Background(), "")
		require.NoError(t, err)
		// Invalid lease should be skipped
		for _, l := range leases {
			assert.NotEqual(t, "invalid_lease", l.LeaseID)
		}
	})
}

// TestRevokePrefix_ErrorHandling tests error handling in RevokePrefix
func TestRevokePrefix_ErrorHandling(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("partial revoke failures", func(t *testing.T) {
		// Create leases
		lease1, err := mgr.CreateLease(ctx, "partial/lease1", 5*time.Minute, nil)
		require.NoError(t, err)
		lease2, err := mgr.CreateLease(ctx, "partial/lease2", 5*time.Minute, nil)
		require.NoError(t, err)

		// Manually delete one lease from storage to simulate error
		key := []byte(LeasePrefix + lease1.LeaseID)
		err = mgr.store.Delete(key)
		require.NoError(t, err)

		// RevokePrefix should continue despite one failure
		count, err := mgr.RevokePrefix(ctx, "partial/")
		require.NoError(t, err)
		// Only the second lease should be successfully revoked
		assert.Equal(t, 1, count)

		// Verify second lease was revoked
		_, err = mgr.GetLease(ctx, lease2.LeaseID)
		assert.Error(t, err)
	})
}

// errorStorage is a mock storage that returns errors for testing
type errorStorage struct {
	*mockStorage
	getError    error
	setError    error
	deleteError error
}

func (e *errorStorage) Get(key []byte) ([]byte, error) {
	if e.getError != nil {
		return nil, e.getError
	}
	return e.mockStorage.Get(key)
}

func (e *errorStorage) Set(key, value []byte) error {
	if e.setError != nil {
		return e.setError
	}
	return e.mockStorage.Set(key, value)
}

func (e *errorStorage) Delete(key []byte) error {
	if e.deleteError != nil {
		return e.deleteError
	}
	return e.mockStorage.Delete(key)
}

// TestCreateLease_StorageErrors tests storage error handling
func TestCreateLease_StorageErrors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("storage set error", func(t *testing.T) {
		errStore := &errorStorage{
			mockStorage: newMockStorage(),
			setError:    &dberrors.MessageError{Message: "storage write failed"},
		}
		defer errStore.Close()

		config := &ManagerConfig{
			Store:  errStore,
			Logger: logger.NewAdapter(log),
		}

		mgr, err := NewManager(config)
		require.NoError(t, err)
		defer mgr.Close()

		ctx := context.Background()
		_, err = mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to store lease")
	})
}

// TestRevokeLease_StorageErrors tests revoke with storage errors
func TestRevokeLease_StorageErrors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("storage delete error", func(t *testing.T) {
		errStore := &errorStorage{
			mockStorage: newMockStorage(),
			deleteError: &dberrors.MessageError{Message: "storage delete failed"},
		}
		defer errStore.Close()

		config := &ManagerConfig{
			Store:  errStore,
			Logger: logger.NewAdapter(log),
		}

		mgr, err := NewManager(config)
		require.NoError(t, err)
		defer mgr.Close()

		ctx := context.Background()
		err = mgr.RevokeLease(ctx, "test_lease")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete lease")
	})
}

// TestRenewLease_StorageErrors tests renew with storage errors
func TestRenewLease_StorageErrors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	store := newMockStorage()
	defer store.Close()

	config := &ManagerConfig{
		Store:  store,
		Logger: logger.NewAdapter(log),
	}

	mgr, err := NewManager(config)
	require.NoError(t, err)
	defer mgr.Close()

	ctx := context.Background()

	t.Run("update lease storage error", func(t *testing.T) {
		// Create a lease
		lease, err := mgr.CreateLease(ctx, "test/path", 5*time.Minute, nil)
		require.NoError(t, err)

		// Replace store with error store
		errStore := &errorStorage{
			mockStorage: store,
			setError:    &dberrors.MessageError{Message: "storage write failed"},
		}
		mgr.store = errStore

		// Try to renew - should fail
		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update lease")
	})
}

// TestLeaseWithMetadata tests lease metadata handling
func TestLeaseWithMetadata(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("lease with complex metadata", func(t *testing.T) {
		opts := &LeaseOptions{
			Renewable:   true,
			MaxRenewals: 10,
			Metadata: map[string]string{
				"user_id":     "user123",
				"role":        "admin",
				"environment": "production",
				"service":     "api-gateway",
			},
		}

		lease, err := mgr.CreateLease(ctx, "api/credentials", 30*time.Minute, opts)
		require.NoError(t, err)

		// Verify metadata is preserved
		assert.Equal(t, "user123", lease.Metadata["user_id"])
		assert.Equal(t, "admin", lease.Metadata["role"])
		assert.Equal(t, "production", lease.Metadata["environment"])
		assert.Equal(t, "api-gateway", lease.Metadata["service"])

		// Reload and verify persistence
		retrieved, err := mgr.GetLease(ctx, lease.LeaseID)
		require.NoError(t, err)
		assert.Equal(t, opts.Metadata, retrieved.Metadata)
	})

	t.Run("lease with nil metadata", func(t *testing.T) {
		opts := &LeaseOptions{
			Renewable:   true,
			MaxRenewals: 5,
			Metadata:    nil, // Explicitly nil
		}

		lease, err := mgr.CreateLease(ctx, "api/credentials2", 30*time.Minute, opts)
		require.NoError(t, err)

		// Metadata can be nil
		assert.Nil(t, lease.Metadata)
	})
}

// TestLeaseJSONSerialization tests JSON marshaling/unmarshaling
func TestLeaseJSONSerialization(t *testing.T) {
	now := time.Now()
	revokedTime := now.Add(-1 * time.Hour)

	t.Run("marshal and unmarshal complete lease", func(t *testing.T) {
		original := &Lease{
			LeaseID:     "lease_test_123",
			SecretPath:  "database/creds/myapp",
			IssuedAt:    now,
			ExpiresAt:   now.Add(24 * time.Hour),
			TTL:         24 * time.Hour,
			RenewCount:  3,
			MaxRenewals: 10,
			Renewable:   true,
			RevokedAt:   &revokedTime,
			Metadata: map[string]string{
				"role": "readonly",
			},
		}

		// Marshal to JSON
		data, err := json.Marshal(original)
		require.NoError(t, err)

		// Unmarshal back
		restored := &Lease{}
		err = json.Unmarshal(data, restored)
		require.NoError(t, err)

		// Verify all fields
		assert.Equal(t, original.LeaseID, restored.LeaseID)
		assert.Equal(t, original.SecretPath, restored.SecretPath)
		assert.Equal(t, original.RenewCount, restored.RenewCount)
		assert.Equal(t, original.MaxRenewals, restored.MaxRenewals)
		assert.Equal(t, original.Renewable, restored.Renewable)
		assert.NotNil(t, restored.RevokedAt)
		assert.Equal(t, original.Metadata, restored.Metadata)
	})

	t.Run("marshal lease without revocation", func(t *testing.T) {
		original := &Lease{
			LeaseID:     "lease_test_456",
			SecretPath:  "secret/data/myapp",
			IssuedAt:    now,
			ExpiresAt:   now.Add(1 * time.Hour),
			TTL:         1 * time.Hour,
			RenewCount:  0,
			MaxRenewals: 0,
			Renewable:   true,
			RevokedAt:   nil,
			Metadata:    map[string]string{},
		}

		// Marshal to JSON
		data, err := json.Marshal(original)
		require.NoError(t, err)

		// Unmarshal back
		restored := &Lease{}
		err = json.Unmarshal(data, restored)
		require.NoError(t, err)

		// Verify RevokedAt is nil
		assert.Nil(t, restored.RevokedAt)
	})
}

// TestIteratorBoundaries tests iterator edge cases
func TestIteratorBoundaries(t *testing.T) {
	store := newMockStorage()
	defer store.Close()

	t.Run("iterator on empty range", func(t *testing.T) {
		// Add some data
		store.Set([]byte("a"), []byte("1"))
		store.Set([]byte("c"), []byte("3"))

		// Create iterator with range that excludes all keys
		iter := store.NewIterator(&storage.IteratorOptions{
			LowerBound: []byte("d"),
			UpperBound: []byte("z"),
		})
		defer iter.Close()

		ok := iter.Next()
		assert.False(t, ok)
	})

	t.Run("iterator with nil bounds", func(t *testing.T) {
		store.Set([]byte("x"), []byte("24"))
		store.Set([]byte("y"), []byte("25"))

		iter := store.NewIterator(nil)
		defer iter.Close()

		count := 0
		for iter.Next() {
			count++
		}
		assert.Greater(t, count, 0)
	})
}

// TestManagerMetrics tests that metrics are updated correctly
func TestManagerMetrics(t *testing.T) {
	mgr, cleanup := setupLeaseManager(t)
	defer cleanup()

	ctx := context.Background()

	// Note: We can't easily verify Prometheus metrics values in unit tests
	// but we can verify the operations don't panic and metrics exist

	t.Run("metrics initialized", func(t *testing.T) {
		assert.NotNil(t, mgr.metrics)
		assert.NotNil(t, mgr.metrics.leasesCreated)
		assert.NotNil(t, mgr.metrics.leasesRenewed)
		assert.NotNil(t, mgr.metrics.leasesRevoked)
		assert.NotNil(t, mgr.metrics.leasesExpired)
		assert.NotNil(t, mgr.metrics.activeLeasesCount)
		assert.NotNil(t, mgr.metrics.leaseOpDuration)
	})

	t.Run("operations update metrics", func(t *testing.T) {
		// Create lease
		lease, err := mgr.CreateLease(ctx, "metrics/test", 5*time.Minute, nil)
		require.NoError(t, err)

		// Renew lease
		_, err = mgr.RenewLease(ctx, lease.LeaseID, 5*time.Minute)
		require.NoError(t, err)

		// Revoke lease
		err = mgr.RevokeLease(ctx, lease.LeaseID)
		require.NoError(t, err)

		// Operations should have updated metrics (no panic)
	})
}
