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

//go:build integration

package seal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// TestSealFullLifecycle runs the complete seal lifecycle as a single test
// with ordered subtests. This ensures proper state management between operations
// and avoids the skip issues with standalone tests that depend on specific states.
//
// Test order:
// 1. Check initial status
// 2. Initialize seal (if not already initialized)
// 3. Verify sealed state after init
// 4. Unseal with threshold shares
// 5. Verify unsealed state
// 6. Seal the database
// 7. Verify sealed state
// 8. Unseal again
// 9. Rekey with new configuration
// 10. Verify new configuration
// 11. Final unseal to leave system in usable state
func TestSealFullLifecycle(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// State tracking across subtests
	var shares [][]byte
	var threshold int32 = 2
	var totalShares int32 = 3

	// Helper to unseal with stored shares
	unsealWithShares := func(t *testing.T) {
		t.Helper()
		if len(shares) == 0 {
			t.Fatal("No shares available for unseal")
		}

		// Provide threshold shares to unseal
		for i := 0; i < int(threshold); i++ {
			resp, err := client.Unseal(ctx, &pb.UnsealRequest{Share: shares[i]})
			require.NoError(t, err, "Unseal with share %d should succeed", i+1)
			if !resp.Sealed {
				t.Logf("Unsealed after %d shares", i+1)
				return
			}
		}
	}

	// Step 1: Check initial status
	t.Run("01_initial_status", func(t *testing.T) {
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
			t.Fatal("Seal service not implemented - cannot continue lifecycle test")
		}
		require.NoError(t, err, "SealStatus should succeed")
		require.NotNil(t, resp)

		t.Logf("Initial status: initialized=%v, sealed=%v, threshold=%d, total=%d, version=%d",
			resp.Initialized, resp.Sealed, resp.Threshold, resp.TotalShares, resp.Version)

		// If already initialized, we need to get to unsealed state
		// to be able to run rekey tests later
		if resp.Initialized {
			t.Log("Seal already initialized - will work with existing configuration")
			threshold = resp.Threshold
			totalShares = resp.TotalShares
		}
	})

	// Step 2: Initialize seal (if not already initialized)
	t.Run("02_initialize", func(t *testing.T) {
		// Check current status
		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if statusResp.Initialized {
			t.Log("Seal already initialized - acquiring shares via state manager")

			// Use state manager to acquire shares (via rekey if unsealed, or from file)
			mgr := getStateManager()
			require.NotNil(t, mgr, "State manager should be available")

			// Update local config to match server
			threshold = statusResp.Threshold
			totalShares = statusResp.TotalShares

			// First check if we already have shares in the state manager
			if existingShares := mgr.GetShares(); len(existingShares) > 0 {
				shares = existingShares
				t.Logf("Loaded %d shares from state manager", len(shares))

				// If sealed, try to unseal with existing shares
				if statusResp.Sealed {
					t.Log("System is sealed - unsealing with saved shares")
					if err := mgr.EnsureUnsealed(ctx, client); err != nil {
						t.Fatalf("Failed to unseal with saved shares: %v", err)
					}
					t.Log("System unsealed successfully")
				}
				mgr.UpdateConfig(threshold, totalShares, statusResp.Version)
				return
			}

			// No shares in state manager - try to acquire via rekey (only works if unsealed)
			if statusResp.Sealed {
				// Sealed with no shares - this is a stuck state
				// The test environment may need to be reset
				t.Fatalf("System is sealed and no shares are available. " +
					"This test requires either: (1) an uninitialized seal, " +
					"(2) an unsealed system, or (3) shares saved from a previous init. " +
					"Please reset the test environment or ensure TestSealInitAndUnsealStandalone runs first.")
			}

			// System is unsealed - acquire shares via rekey
			acquiredShares, err := mgr.AcquireShares(ctx, client)
			if err != nil {
				t.Fatalf("Cannot acquire shares via rekey: %v", err)
			}
			shares = acquiredShares
			t.Logf("Acquired %d shares via rekey, threshold=%d", len(shares), threshold)

			// Update state manager with current config
			mgr.UpdateConfig(threshold, totalShares, statusResp.Version)
			return
		}

		// Initialize with 2-of-3 threshold
		initResp, err := client.SealInit(ctx, &pb.SealInitRequest{
			Threshold:           threshold,
			TotalShares:         totalShares,
			RecoveryKeysEnabled: false,
		})
		require.NoError(t, err, "SealInit should succeed")
		require.Empty(t, initResp.Error, "SealInit error: %s", initResp.Error)

		// Store shares for subsequent operations
		shares = initResp.Shares
		require.Len(t, shares, int(totalShares), "Should receive %d shares", totalShares)

		for i, share := range shares {
			assert.NotEmpty(t, share, "Share %d should not be empty", i+1)
		}

		// Save shares to state manager for persistence
		mgr := getStateManager()
		if mgr != nil {
			mgr.UpdateConfig(threshold, totalShares, initResp.Version)
			if err := mgr.SaveShares(shares); err != nil {
				t.Logf("Warning: Failed to save shares: %v", err)
			}
		}

		t.Logf("Seal initialized with %d shares, threshold %d, version %d",
			len(shares), threshold, initResp.Version)
	})

	// Step 3: Verify sealed state after init
	t.Run("03_verify_sealed_after_init", func(t *testing.T) {
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		// After init (if we did init), system should be sealed
		// If already initialized, it could be sealed or unsealed
		assert.True(t, resp.Initialized, "System should be initialized")
		t.Logf("Status: sealed=%v, shares_provided=%d/%d",
			resp.Sealed, resp.SharesProvided, resp.Threshold)
	})

	// Step 4: Unseal with threshold shares
	t.Run("04_unseal", func(t *testing.T) {
		// Check if already unsealed
		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if !statusResp.Sealed {
			t.Log("System already unsealed")
			return
		}

		// Shares should be available from 02_initialize
		require.NotEmpty(t, shares, "Shares should be available from initialization step")

		// Unseal with threshold shares
		for i := 0; i < int(threshold); i++ {
			resp, err := client.Unseal(ctx, &pb.UnsealRequest{Share: shares[i]})
			require.NoError(t, err, "Unseal with share %d should succeed", i+1)
			require.Empty(t, resp.Error, "Unseal error: %s", resp.Error)

			t.Logf("After share %d: sealed=%v, shares_provided=%d/%d",
				i+1, resp.Sealed, resp.SharesProvided, resp.Threshold)

			if !resp.Sealed {
				t.Logf("Unsealed successfully after %d shares", i+1)
				break
			}
		}
	})

	// Step 5: Verify unsealed state
	t.Run("05_verify_unsealed", func(t *testing.T) {
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		assert.False(t, resp.Sealed, "System should be unsealed")
		assert.True(t, resp.Initialized, "System should still be initialized")

		t.Logf("Verified unsealed: version=%d, threshold=%d, total=%d",
			resp.Version, resp.Threshold, resp.TotalShares)
	})

	// Step 6: Seal the database
	t.Run("06_seal", func(t *testing.T) {
		// First check if we're unsealed
		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if statusResp.Sealed {
			t.Log("System is already sealed - skipping seal operation")
			return
		}

		// Shares should be available from 02_initialize
		require.NotEmpty(t, shares, "Shares should be available for unsealing after seal")

		sealResp, err := client.Seal(ctx, &pb.SealRequest{Force: false})
		require.NoError(t, err, "Seal should succeed")
		require.Empty(t, sealResp.Error, "Seal error: %s", sealResp.Error)
		assert.True(t, sealResp.Sealed, "Response should indicate sealed")

		t.Log("System sealed successfully")
	})

	// Step 7: Verify sealed state
	t.Run("07_verify_sealed", func(t *testing.T) {
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		assert.True(t, resp.Sealed, "System should be sealed")
		assert.Equal(t, int32(0), resp.SharesProvided, "SharesProvided should be 0 after seal")

		t.Log("Verified sealed state")
	})

	// Step 8: Unseal again
	t.Run("08_unseal_again", func(t *testing.T) {
		// Shares should be available from 02_initialize
		require.NotEmpty(t, shares, "Shares should be available from initialization step")

		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if !statusResp.Sealed {
			t.Log("System already unsealed")
			return
		}

		unsealWithShares(t)

		// Verify unsealed
		statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)
		assert.False(t, statusResp.Sealed, "System should be unsealed")

		t.Log("System unsealed again successfully")
	})

	// Step 9: Rekey with new configuration
	t.Run("09_rekey", func(t *testing.T) {
		// Shares should be available from 02_initialize
		require.NotEmpty(t, shares, "Shares should be available from initialization step")

		// Verify we're unsealed (required for rekey)
		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if statusResp.Sealed {
			t.Log("System is sealed, unsealing for rekey test")
			unsealWithShares(t)
		}

		// Rekey to 3-of-5
		newThreshold := int32(3)
		newTotal := int32(5)

		rekeyResp, err := client.SealRekey(ctx, &pb.SealRekeyRequest{
			NewThreshold: newThreshold,
			NewTotal:     newTotal,
		})
		require.NoError(t, err, "SealRekey should succeed")
		require.Empty(t, rekeyResp.Error, "SealRekey error: %s", rekeyResp.Error)

		// Update stored shares
		require.Len(t, rekeyResp.Shares, int(newTotal), "Should receive %d new shares", newTotal)
		shares = rekeyResp.Shares
		threshold = newThreshold
		totalShares = newTotal

		// Save new shares to state manager for persistence
		mgr := getStateManager()
		if mgr != nil {
			mgr.UpdateConfig(threshold, totalShares, rekeyResp.Version)
			if err := mgr.SaveShares(shares); err != nil {
				t.Logf("Warning: Failed to save new shares: %v", err)
			}
		}

		t.Logf("Rekey successful: new threshold=%d, new total=%d, version=%d",
			newThreshold, newTotal, rekeyResp.Version)
	})

	// Step 10: Verify new configuration
	t.Run("10_verify_new_config", func(t *testing.T) {
		// Shares should be available from rekey in step 09
		require.NotEmpty(t, shares, "Shares should be available from rekey step")

		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		assert.Equal(t, threshold, resp.Threshold, "Threshold should match new value")
		assert.Equal(t, totalShares, resp.TotalShares, "TotalShares should match new value")
		assert.False(t, resp.Sealed, "System should still be unsealed after rekey")

		t.Logf("Verified new config: threshold=%d, total=%d", resp.Threshold, resp.TotalShares)
	})

	// Step 11: Final state - leave system unsealed
	t.Run("11_final_unsealed", func(t *testing.T) {
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if resp.Sealed && len(shares) > 0 {
			t.Log("Unsealing system for next test run")
			unsealWithShares(t)
		}

		resp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		t.Logf("Final state: sealed=%v, initialized=%v, threshold=%d, total=%d",
			resp.Sealed, resp.Initialized, resp.Threshold, resp.TotalShares)
	})
}

// TestSealErrorCases tests error handling for seal operations.
// This test doesn't depend on specific seal states.
func TestSealErrorCases(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	t.Run("init_invalid_threshold_greater_than_shares", func(t *testing.T) {
		resp, err := client.SealInit(ctx, &pb.SealInitRequest{
			Threshold:   5,
			TotalShares: 3,
		})
		// Server returns errors in response body, not as gRPC errors
		if err != nil {
			// gRPC error - test passes
			return
		}
		assert.NotEmpty(t, resp.Error, "Init with threshold > shares should fail with error in response")
	})

	t.Run("init_zero_shares", func(t *testing.T) {
		resp, err := client.SealInit(ctx, &pb.SealInitRequest{
			Threshold:   0,
			TotalShares: 0,
		})
		// Server returns errors in response body, not as gRPC errors
		if err != nil {
			// gRPC error - test passes
			return
		}
		assert.NotEmpty(t, resp.Error, "Init with 0 shares should fail with error in response")
	})

	t.Run("unseal_empty_share", func(t *testing.T) {
		// Check current status first to avoid polluting partial unseal state
		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		if statusResp.Sealed {
			// If sealed, sending empty share could potentially increment shares_provided
			// (even though it should be rejected). Skip to avoid state pollution.
			t.Log("System is sealed - skipping empty share test to avoid partial unseal state pollution")
			return
		}

		// System is unsealed - test empty share handling
		// When unsealed, empty share should either return an error or be a no-op
		resp, err := client.Unseal(ctx, &pb.UnsealRequest{Share: nil})
		if err != nil {
			// gRPC error - valid error handling
			t.Logf("Server returned gRPC error for empty share: %v", err)
			return
		}
		if resp.Error != "" {
			// Error in response body - valid error handling
			t.Logf("Server returned error in response for empty share: %s", resp.Error)
			return
		}
		// No error - server treated it as no-op on unsealed system (also valid)
		assert.False(t, resp.Sealed, "System should remain unsealed after empty share no-op")
		t.Log("Server accepted empty share as no-op on unsealed system")
	})

	t.Run("rekey_invalid_threshold", func(t *testing.T) {
		// Check current status - need unsealed state
		statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)

		// If sealed, try to unseal using state manager
		if statusResp.Sealed {
			mgr := getStateManager()
			require.NotNil(t, mgr, "State manager must be available to unseal for rekey error test")

			err := mgr.EnsureUnsealed(ctx, client)
			require.NoError(t, err, "Must unseal for rekey error test")
			t.Log("Unsealed system using state manager for rekey error test")
		}

		resp, err := client.SealRekey(ctx, &pb.SealRekeyRequest{
			NewThreshold: 5,
			NewTotal:     3,
		})
		// Server returns errors in response body, not as gRPC errors
		if err != nil {
			// gRPC error - test passes
			return
		}
		assert.NotEmpty(t, resp.Error, "Rekey with threshold > shares should fail with error in response")
	})
}

// sealTestHelper provides utilities for seal integration tests.
type sealTestHelper struct {
	t      *testing.T
	ctx    context.Context
	conn   *grpc.ClientConn
	client pb.QuicBaaSClient
}

// newSealTestHelper creates a new seal test helper.
func newSealTestHelper(t *testing.T, ctx context.Context) *sealTestHelper {
	t.Helper()
	conn := setupGRPCConnection(t, ctx)

	return &sealTestHelper{
		t:      t,
		ctx:    ctx,
		conn:   conn,
		client: pb.NewQuicBaaSClient(conn),
	}
}

// Close closes the gRPC connection.
func (h *sealTestHelper) Close() {
	if h.conn != nil {
		h.conn.Close()
	}
}

// GetStatus returns the current seal status.
func (h *sealTestHelper) GetStatus() (*pb.SealStatusResponse, error) {
	return h.client.SealStatus(h.ctx, &pb.SealStatusRequest{})
}

// IsSealed returns true if the system is sealed.
func (h *sealTestHelper) IsSealed() bool {
	resp, err := h.GetStatus()
	if err != nil {
		return false
	}
	return resp.Sealed
}

// IsInitialized returns true if the seal is initialized.
func (h *sealTestHelper) IsInitialized() bool {
	resp, err := h.GetStatus()
	if err != nil {
		return false
	}
	return resp.Initialized
}
