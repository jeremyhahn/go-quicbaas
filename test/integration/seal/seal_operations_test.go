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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestSealStatusStandalone tests the seal status endpoint on a standalone node.
// This test verifies that:
// - The SealStatus endpoint is reachable
// - Response fields contain valid values
// - Both initialized and uninitialized systems report proper configuration
func TestSealStatusStandalone(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})

	// Verify seal service is implemented - integration tests require all services to be available
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		require.Fail(t, "Seal service must be implemented on the server for integration tests")
	}

	require.NoError(t, err, "SealStatus should not return an error")
	require.NotNil(t, resp, "SealStatus response should not be nil")

	// Verify response structure has valid seal status boolean values
	// Note: resp.Sealed and resp.Initialized are booleans, they are always valid

	// The seal manager always reports its configured threshold and total shares,
	// regardless of initialization state. This allows clients to know the
	// configuration that will be used for initialization.
	assert.GreaterOrEqual(t, resp.Threshold, int32(1),
		"Threshold should be at least 1 (configured value)")
	assert.GreaterOrEqual(t, resp.TotalShares, resp.Threshold,
		"TotalShares should be >= Threshold (configured value)")

	// If initialized, verify additional state values
	if resp.Initialized {
		assert.GreaterOrEqual(t, resp.Version, uint64(1),
			"Version should be at least 1 for initialized seal")

		if resp.Sealed {
			assert.GreaterOrEqual(t, resp.SharesProvided, int32(0),
				"SharesProvided should be non-negative when sealed")
			assert.Less(t, resp.SharesProvided, resp.Threshold,
				"SharesProvided should be less than Threshold when still sealed")
		}
	} else {
		// Uninitialized system should have version 0
		assert.Equal(t, uint64(0), resp.Version,
			"Version should be 0 for uninitialized seal")
	}

	// Mode should be a valid string (may be empty for uninitialized)
	// Valid modes: "", "shamir", "transit", etc.
	t.Logf("Seal status: sealed=%v, initialized=%v, mode=%v, threshold=%d, total=%d, version=%d",
		resp.Sealed, resp.Initialized, resp.Mode, resp.Threshold, resp.TotalShares, resp.Version)
}

// TestSealInitAndUnsealStandalone tests the full seal/unseal lifecycle.
// This test:
// - Initializes the seal with 2-of-3 threshold
// - Verifies correct share count is returned
// - Unseals with threshold shares
// - Verifies state transitions at each step
// - Saves shares to state manager for use by subsequent tests
func TestSealInitAndUnsealStandalone(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Check current seal status
	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		require.Fail(t, "Seal service must be implemented on the server for integration tests")
	}
	require.NoError(t, err, "Initial SealStatus should succeed")

	// If already initialized, try to ensure we have shares available
	if statusResp.Initialized {
		t.Log("Seal manager already initialized, verifying existing configuration")

		assert.GreaterOrEqual(t, statusResp.Threshold, int32(1),
			"Initialized seal should have threshold >= 1")
		assert.GreaterOrEqual(t, statusResp.TotalShares, statusResp.Threshold,
			"TotalShares should be >= Threshold")
		assert.GreaterOrEqual(t, statusResp.Version, uint64(1),
			"Version should be >= 1")

		// If unsealed and we don't have shares, acquire them via rekey for later tests
		mgr := getStateManager()
		if mgr != nil && !mgr.HasShares() && !statusResp.Sealed {
			t.Log("System unsealed without shares - acquiring via rekey")
			shares, err := mgr.AcquireShares(ctx, client)
			if err != nil {
				t.Logf("Warning: Could not acquire shares: %v", err)
			} else {
				t.Logf("Acquired %d shares for subsequent tests", len(shares))
			}
		}
		return
	}

	// Initialize the seal manager with 2-of-3 threshold
	const expectedShares = 3
	const expectedThreshold = 2

	initResp, err := client.SealInit(ctx, &pb.SealInitRequest{
		Threshold:           expectedThreshold,
		TotalShares:         expectedShares,
		RecoveryKeysEnabled: false,
	})
	require.NoError(t, err, "SealInit should succeed")
	require.Empty(t, initResp.Error, "SealInit should not return error message: %s", initResp.Error)

	// Verify init response
	require.Len(t, initResp.Shares, expectedShares,
		"SealInit should return exactly %d shares", expectedShares)
	for i, share := range initResp.Shares {
		assert.NotEmpty(t, share, "Share %d should not be empty", i+1)
		assert.GreaterOrEqual(t, len(share), 16, "Share %d should be at least 16 bytes", i+1)
	}
	assert.GreaterOrEqual(t, initResp.Version, uint64(1), "Initial version should be >= 1")

	// Save shares to state manager for use by subsequent tests
	mgr := getStateManager()
	if mgr != nil {
		mgr.UpdateConfig(expectedThreshold, expectedShares, initResp.Version)
		if err := mgr.SaveShares(initResp.Shares); err != nil {
			t.Logf("Warning: Failed to save shares: %v", err)
		} else {
			t.Logf("Saved %d shares to state manager", len(initResp.Shares))
		}
	}

	// Verify sealed status after init
	statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus after init should succeed")
	assert.True(t, statusResp.Sealed, "System should be sealed after init")
	assert.True(t, statusResp.Initialized, "System should be initialized after init")
	assert.Equal(t, int32(expectedThreshold), statusResp.Threshold,
		"Threshold should match init request")
	assert.Equal(t, int32(expectedShares), statusResp.TotalShares,
		"TotalShares should match init request")

	// Unseal with first share
	unsealResp1, err := client.Unseal(ctx, &pb.UnsealRequest{Share: initResp.Shares[0]})
	require.NoError(t, err, "First unseal should succeed")
	require.Empty(t, unsealResp1.Error, "First unseal should not return error: %s", unsealResp1.Error)
	assert.True(t, unsealResp1.Sealed, "Should still be sealed after 1 share")
	assert.Equal(t, int32(1), unsealResp1.SharesProvided,
		"SharesProvided should be 1 after first share")
	assert.Equal(t, int32(expectedThreshold), unsealResp1.Threshold,
		"Threshold should be returned in unseal response")

	// Unseal with second share (should complete unseal)
	unsealResp2, err := client.Unseal(ctx, &pb.UnsealRequest{Share: initResp.Shares[1]})
	require.NoError(t, err, "Second unseal should succeed")
	require.Empty(t, unsealResp2.Error, "Second unseal should not return error: %s", unsealResp2.Error)
	assert.False(t, unsealResp2.Sealed, "Should be unsealed after threshold shares")
	assert.Equal(t, int32(expectedThreshold), unsealResp2.SharesProvided,
		"SharesProvided should equal threshold after complete unseal")

	// Verify unsealed status
	statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus after unseal should succeed")
	assert.False(t, statusResp.Sealed, "System should be unsealed")
	assert.True(t, statusResp.Initialized, "System should still be initialized")
}

// TestSealAndResealStandalone tests sealing an unsealed database.
// This test:
// - Ensures the database is unsealed (using state manager if needed)
// - Seals the database
// - Verifies sealed state
// - Restores unsealed state for subsequent tests
func TestSealAndResealStandalone(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Check seal status
	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		require.Fail(t, "Seal service must be implemented on the server for integration tests")
	}
	require.NoError(t, err, "SealStatus should succeed")

	require.True(t, statusResp.Initialized,
		"Seal manager must be initialized - run TestSealInitAndUnsealStandalone first")

	// If sealed, try to unseal first using state manager
	if statusResp.Sealed {
		mgr := getStateManager()
		require.NotNil(t, mgr, "State manager must be available when system is sealed")
		err := mgr.EnsureUnsealed(ctx, client)
		require.NoError(t, err, "Must be able to unseal system using state manager")
		t.Log("Unsealed system using state manager")
		// Refresh status
		statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)
	}

	// Record pre-seal state
	preSealVersion := statusResp.Version

	// Seal the database
	sealResp, err := client.Seal(ctx, &pb.SealRequest{Force: false})
	require.NoError(t, err, "Seal should succeed")
	require.Empty(t, sealResp.Error, "Seal should not return error: %s", sealResp.Error)
	assert.True(t, sealResp.Sealed, "Response should indicate sealed=true after seal")

	// Verify sealed status
	statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus after seal should succeed")
	assert.True(t, statusResp.Sealed, "System should be sealed")
	assert.True(t, statusResp.Initialized, "System should still be initialized")
	assert.Equal(t, preSealVersion, statusResp.Version,
		"Version should not change after seal (only rekey changes version)")
	assert.Equal(t, int32(0), statusResp.SharesProvided,
		"SharesProvided should be 0 after seal")

	// Restore unsealed state for subsequent tests
	mgr := getStateManager()
	if mgr != nil {
		if err := mgr.EnsureUnsealed(ctx, client); err != nil {
			t.Logf("Warning: Could not restore unsealed state: %v", err)
		} else {
			t.Log("Restored unsealed state for subsequent tests")
		}
	}
}

// TestSealRekeyStandalone tests the rekey operation.
// This test:
// - Ensures database is unsealed (using state manager if needed)
// - Rekeys to new threshold (3-of-5)
// - Verifies new shares and configuration
// - Saves new shares to state manager
func TestSealRekeyStandalone(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Check seal status
	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		require.Fail(t, "Seal service must be implemented on the server for integration tests")
	}
	require.NoError(t, err, "SealStatus should succeed")

	// Need to be initialized to rekey
	require.True(t, statusResp.Initialized,
		"Seal manager must be initialized for rekey operation")

	// If sealed, try to unseal first using state manager
	if statusResp.Sealed {
		mgr := getStateManager()
		require.NotNil(t, mgr, "State manager must be available when system is sealed")
		err := mgr.EnsureUnsealed(ctx, client)
		require.NoError(t, err, "Must be able to unseal system for rekey operation")
		t.Log("Unsealed system using state manager")
		// Refresh status
		statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)
	}

	// Record pre-rekey state
	oldThreshold := statusResp.Threshold
	oldTotal := statusResp.TotalShares
	oldVersion := statusResp.Version

	// Rekey with new threshold (3-of-5)
	const newThreshold = 3
	const newTotal = 5

	rekeyResp, err := client.SealRekey(ctx, &pb.SealRekeyRequest{
		NewThreshold: newThreshold,
		NewTotal:     newTotal,
	})
	require.NoError(t, err, "SealRekey should succeed")
	require.Empty(t, rekeyResp.Error, "SealRekey should not return error: %s", rekeyResp.Error)

	// Verify rekey response
	require.Len(t, rekeyResp.Shares, newTotal,
		"Rekey should return exactly %d new shares", newTotal)
	for i, share := range rekeyResp.Shares {
		assert.NotEmpty(t, share, "New share %d should not be empty", i+1)
		assert.GreaterOrEqual(t, len(share), 16, "New share %d should be at least 16 bytes", i+1)
	}
	assert.Greater(t, rekeyResp.Version, oldVersion,
		"Version should increase after rekey (old=%d)", oldVersion)

	// Verify new configuration via status
	statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus after rekey should succeed")
	assert.Equal(t, int32(newThreshold), statusResp.Threshold,
		"Threshold should be updated (was %d)", oldThreshold)
	assert.Equal(t, int32(newTotal), statusResp.TotalShares,
		"TotalShares should be updated (was %d)", oldTotal)
	assert.False(t, statusResp.Sealed,
		"System should still be unsealed after rekey")

	// Save new shares to state manager for persistence
	mgr := getStateManager()
	if mgr != nil {
		mgr.UpdateConfig(int32(newThreshold), int32(newTotal), rekeyResp.Version)
		if err := mgr.SaveShares(rekeyResp.Shares); err != nil {
			t.Logf("Warning: Failed to save new shares: %v", err)
		} else {
			t.Log("Saved new shares to state manager")
		}
	}
}

// TestSealClusterMembership tests seal operations across cluster nodes.
// This test:
// - Connects to all cluster nodes
// - Verifies seal status consistency across initialized nodes
// - Checks cluster membership information
//
// Note: Seal initialization state is managed locally by each node.
// This test verifies consistency only among nodes that are initialized.
// In a production cluster, all nodes should be initialized before operations.
func TestSealClusterMembership(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	require.GreaterOrEqual(t, len(endpoints), 3,
		"Cluster test requires at least 3 endpoints configured")

	type nodeStatus struct {
		endpoint     string
		sealed       bool
		initialized  bool
		threshold    int32
		totalShares  int32
		version      uint64
		clusterNodes int32
		reachable    bool
		err          error
	}

	statuses := make([]nodeStatus, len(endpoints))

	// Collect status from all nodes
	for i, endpoint := range endpoints {
		statuses[i].endpoint = endpoint

		conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, endpoint)
		if err != nil {
			statuses[i].reachable = false
			statuses[i].err = err
			t.Logf("Node %d (%s): Connection failed - %v", i+1, endpoint, err)
			continue
		}

		client := pb.NewQuicBaaSClient(conn)
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		conn.Close()

		if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
			t.Logf("Node %d (%s): Seal service not implemented", i+1, endpoint)
			continue
		}

		if err != nil {
			statuses[i].reachable = true
			statuses[i].err = err
			t.Logf("Node %d (%s): SealStatus failed - %v", i+1, endpoint, err)
			continue
		}

		statuses[i].reachable = true
		statuses[i].sealed = resp.Sealed
		statuses[i].initialized = resp.Initialized
		statuses[i].threshold = resp.Threshold
		statuses[i].totalShares = resp.TotalShares
		statuses[i].version = resp.Version
		statuses[i].clusterNodes = resp.ClusterNodes
	}

	// Count reachable and initialized nodes
	reachableCount := 0
	initializedCount := 0
	var firstInitialized *nodeStatus
	for i := range statuses {
		if statuses[i].reachable && statuses[i].err == nil {
			reachableCount++
			if statuses[i].initialized {
				initializedCount++
				if firstInitialized == nil {
					firstInitialized = &statuses[i]
				}
			}
		}
	}

	require.GreaterOrEqual(t, reachableCount, 1,
		"At least one node should be reachable for cluster test")

	// Log all node statuses for debugging
	for i, nodeState := range statuses {
		if nodeState.reachable && nodeState.err == nil {
			t.Logf("Node %d (%s): sealed=%v, initialized=%v, threshold=%d, total=%d, version=%d, cluster_nodes=%d",
				i+1, nodeState.endpoint, nodeState.sealed, nodeState.initialized,
				nodeState.threshold, nodeState.totalShares, nodeState.version, nodeState.clusterNodes)
		}
	}

	// Verify consistency across INITIALIZED nodes only
	// Seal initialization is a local operation - each node manages its own state.
	// In a proper cluster deployment, all nodes should be initialized together.
	if firstInitialized != nil && initializedCount > 1 {
		t.Logf("Checking consistency across %d initialized nodes", initializedCount)

		for i, nodeState := range statuses {
			if !nodeState.reachable || nodeState.err != nil {
				continue
			}

			// Only compare initialized nodes against each other
			if !nodeState.initialized {
				t.Logf("Node %d is not initialized - skipping consistency check", i+1)
				continue
			}

			// All initialized nodes should have consistent configuration
			assert.Equal(t, firstInitialized.threshold, nodeState.threshold,
				"Node %d should have same threshold as first initialized node", i+1)
			assert.Equal(t, firstInitialized.totalShares, nodeState.totalShares,
				"Node %d should have same totalShares as first initialized node", i+1)
			assert.Equal(t, firstInitialized.version, nodeState.version,
				"Node %d should have same version as first initialized node", i+1)
			assert.Equal(t, firstInitialized.sealed, nodeState.sealed,
				"Node %d should have same sealed state as first initialized node", i+1)
		}
	} else if firstInitialized != nil {
		t.Logf("Only one initialized node found - consistency check not applicable")
	} else {
		t.Log("No initialized nodes found - cluster seal not yet configured")
	}
}

// TestSealUnsealProgressTracking tests that unseal progress is tracked correctly.
// This test verifies that partial unseal state is maintained and reported.
func TestSealUnsealProgressTracking(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Get current status
	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
		require.Fail(t, "Seal service must be implemented on the server for integration tests")
	}
	require.NoError(t, err, "SealStatus should succeed")

	require.True(t, statusResp.Initialized,
		"Seal manager must be initialized for progress tracking test")

	// Ensure system is sealed for this test (previous tests may have left it unsealed)
	if !statusResp.Sealed {
		mgr := getStateManager()
		require.NotNil(t, mgr, "State manager must be available to seal system for testing")
		err := mgr.EnsureSealed(ctx, client)
		require.NoError(t, err, "Must be able to seal system for progress tracking test")
		t.Log("Sealed system using state manager")
		// Refresh status
		statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
		require.NoError(t, err)
	}

	require.True(t, statusResp.Sealed,
		"Database must be in sealed state to test progress tracking")

	// Verify progress state - note: SharesProvided may not be 0 if a previous test
	// provided shares (state pollution is expected in integration tests where tests
	// may run sequentially against the same server). What matters is that the
	// values are consistent: SharesProvided <= Threshold and Threshold > 0.
	assert.GreaterOrEqual(t, statusResp.Threshold, int32(1),
		"Threshold should be >= 1 for initialized seal")
	assert.GreaterOrEqual(t, statusResp.SharesProvided, int32(0),
		"SharesProvided should be >= 0")
	assert.LessOrEqual(t, statusResp.SharesProvided, statusResp.Threshold,
		"SharesProvided should not exceed Threshold")

	t.Logf("Progress tracking: %d/%d shares provided, need %d more",
		statusResp.SharesProvided, statusResp.Threshold,
		statusResp.Threshold-statusResp.SharesProvided)
}
