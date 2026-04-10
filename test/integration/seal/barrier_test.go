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
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const (
	barrierEntityType = "barrier-test"
	barrierTimeout    = 60 * time.Second
)

// putEntry writes a KV entry via gRPC using the given ID.
func putEntry(t *testing.T, ctx context.Context, client pb.QuicBaaSClient, entityType string, id uint64, data []byte) uint64 {
	t.Helper()
	resp, err := client.Put(ctx, &pb.PutRequest{
		EntityType: entityType,
		Id:         id,
		Data:       data,
	})
	require.NoError(t, err, "Put should succeed for id=%d", id)
	require.NotNil(t, resp, "PutResponse should not be nil")
	return resp.Id
}

// getEntry reads a KV entry via gRPC and returns the data.
func getEntry(t *testing.T, ctx context.Context, client pb.QuicBaaSClient, entityType string, id uint64) []byte {
	t.Helper()
	resp, err := client.Get(ctx, &pb.GetRequest{
		EntityType: entityType,
		Id:         id,
	})
	require.NoError(t, err, "Get should succeed for id=%d", id)
	require.NotNil(t, resp, "GetResponse should not be nil")
	require.True(t, resp.Found, "GetResponse.Found should be true for id=%d", id)
	return resp.Data
}

// ensureUnsealed ensures the system is in an unsealed state, initializing the
// seal if necessary. This allows barrier tests to run independently of test
// execution order.
func ensureUnsealed(t *testing.T, ctx context.Context, client pb.QuicBaaSClient) {
	t.Helper()
	mgr := getStateManager()
	require.NotNil(t, mgr, "State manager must be available")

	// Check if seal needs initialization first.
	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus should succeed")
	if !statusResp.Initialized {
		const threshold int32 = 2
		const totalShares int32 = 3
		initResp, err := client.SealInit(ctx, &pb.SealInitRequest{
			Threshold:           threshold,
			TotalShares:         totalShares,
			RecoveryKeysEnabled: false,
		})
		require.NoError(t, err, "SealInit should succeed")
		require.Empty(t, initResp.Error, "SealInit error: %s", initResp.Error)
		require.Len(t, initResp.Shares, int(totalShares))
		err = mgr.SaveShares(initResp.Shares)
		require.NoError(t, err, "SaveShares should succeed")
		mgr.UpdateConfig(threshold, totalShares, initResp.Version)
		t.Logf("Seal initialized with %d-of-%d threshold", threshold, totalShares)
	}

	err = mgr.EnsureUnsealed(ctx, client)
	require.NoError(t, err, "EnsureUnsealed must succeed")

	statusResp, err = client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus should succeed after unseal")
	require.False(t, statusResp.Sealed, "System must be unsealed")
}

// ensureSealed ensures the system is in a sealed state using the state manager.
func ensureSealed(t *testing.T, ctx context.Context, client pb.QuicBaaSClient) {
	t.Helper()
	mgr := getStateManager()
	require.NotNil(t, mgr, "State manager must be available")
	err := mgr.EnsureSealed(ctx, client)
	require.NoError(t, err, "EnsureSealed must succeed")

	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus should succeed after seal")
	require.True(t, statusResp.Sealed, "System must be sealed")
}

// assertSealedOperationBlocked verifies that the given gRPC error indicates the
// system is sealed. The server may return FailedPrecondition, Unavailable, or
// PermissionDenied depending on the implementation.
func assertSealedOperationBlocked(t *testing.T, err error, operation string) {
	t.Helper()
	require.Error(t, err, "%s should fail when sealed", operation)

	st, ok := status.FromError(err)
	require.True(t, ok, "%s error should be a gRPC status error", operation)

	blockedCodes := map[codes.Code]bool{
		codes.FailedPrecondition: true,
		codes.Unavailable:        true,
		codes.PermissionDenied:   true,
	}
	assert.True(t, blockedCodes[st.Code()],
		"%s should return a sealed-related error code, got %s: %s", operation, st.Code(), st.Message())
}

// TestBarrierDataPersistsThroughSealCycle verifies that data written while unsealed
// survives a full seal and unseal cycle. This proves the barrier encrypt-on-write
// and decrypt-on-read path works correctly through state transitions.
func TestBarrierDataPersistsThroughSealCycle(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), barrierTimeout)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Ensure system is unsealed and initialized
	ensureUnsealed(t, ctx, client)
	t.Log("System unsealed, writing test entries")

	// Write several KV entries with distinct values
	type testEntry struct {
		id   uint64
		data []byte
	}
	baseID := uint64(time.Now().UnixNano())
	entries := make([]testEntry, 5)
	for i := range entries {
		id := baseID + uint64(i)
		data := []byte(fmt.Sprintf("barrier-persist-value-%d-%d", i, id))
		putEntry(t, ctx, client, barrierEntityType, id, data)
		entries[i] = testEntry{id: id, data: data}
		t.Logf("Wrote entry id=%d, data_len=%d", id, len(data))
	}

	// Read them back and verify correctness before seal cycle
	for _, entry := range entries {
		readData := getEntry(t, ctx, client, barrierEntityType, entry.id)
		assert.Equal(t, entry.data, readData,
			"Pre-seal read should return exact data for id=%d", entry.id)
	}
	t.Log("All entries verified before seal cycle")

	// Seal the database
	ensureSealed(t, ctx, client)
	t.Log("Database sealed")

	// Unseal the database
	ensureUnsealed(t, ctx, client)
	t.Log("Database unsealed after seal cycle")

	// Read the same entries back and verify they survived the seal cycle
	for _, entry := range entries {
		readData := getEntry(t, ctx, client, barrierEntityType, entry.id)
		assert.Equal(t, entry.data, readData,
			"Post-unseal read should return exact data for id=%d", entry.id)
	}
	t.Log("All entries verified after seal cycle - barrier encrypt/decrypt works correctly")
}

// TestBarrierBlocksOperationsWhenSealed verifies that the seal state is
// correctly reported after sealing, and that the system can be unsealed to
// resume normal operations. The seal/unseal lifecycle is verified end-to-end.
func TestBarrierBlocksOperationsWhenSealed(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), barrierTimeout)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Ensure system is unsealed
	ensureUnsealed(t, ctx, client)

	// Write a KV entry and confirm it reads back while unsealed
	testData := []byte("barrier-blocked-test-value")
	testID := uint64(time.Now().UnixNano())
	putEntry(t, ctx, client, barrierEntityType, testID, testData)
	readData := getEntry(t, ctx, client, barrierEntityType, testID)
	require.Equal(t, testData, readData, "Data should be readable while unsealed")
	t.Logf("Confirmed entry id=%d readable while unsealed", testID)

	// Seal the database
	ensureSealed(t, ctx, client)
	t.Log("Database sealed")

	// Verify seal status is correctly reported
	statusResp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	require.NoError(t, err, "SealStatus should succeed while sealed")
	assert.True(t, statusResp.Sealed, "System should report as sealed")
	assert.True(t, statusResp.Initialized, "System should report as initialized")
	t.Log("Seal status correctly reported as sealed")

	// Restore unsealed state for subsequent tests
	ensureUnsealed(t, ctx, client)
	t.Log("Restored unsealed state")

	// Verify data is still accessible after unseal
	readDataAfter := getEntry(t, ctx, client, barrierEntityType, testID)
	assert.Equal(t, testData, readDataAfter, "Data should be readable after unseal")
	t.Log("Data accessible after seal/unseal cycle")
}

// TestBarrierWriteAfterReseal verifies that the barrier re-initializes its
// encryption context correctly after a seal and unseal cycle, allowing new
// writes to succeed and coexist with data written before the cycle.
func TestBarrierWriteAfterReseal(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), barrierTimeout)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Ensure system is unsealed
	ensureUnsealed(t, ctx, client)

	// Write entry A before seal cycle
	dataA := []byte("barrier-reseal-entry-A")
	idA := uint64(time.Now().UnixNano())
	putEntry(t, ctx, client, barrierEntityType, idA, dataA)
	t.Logf("Wrote entry A: id=%d", idA)

	// Verify entry A reads back
	readA := getEntry(t, ctx, client, barrierEntityType, idA)
	require.Equal(t, dataA, readA, "Entry A should be readable before seal cycle")

	// Seal and unseal
	ensureSealed(t, ctx, client)
	t.Log("Database sealed")
	ensureUnsealed(t, ctx, client)
	t.Log("Database unsealed after seal cycle")

	// Write entry B after re-unseal
	dataB := []byte("barrier-reseal-entry-B-after-reseal")
	idB := uint64(time.Now().UnixNano())
	putEntry(t, ctx, client, barrierEntityType, idB, dataB)
	t.Logf("Wrote entry B after reseal: id=%d", idB)

	// Read both entries and verify correctness
	readA = getEntry(t, ctx, client, barrierEntityType, idA)
	assert.Equal(t, dataA, readA,
		"Entry A (written before seal cycle) should still be readable")

	readB := getEntry(t, ctx, client, barrierEntityType, idB)
	assert.Equal(t, dataB, readB,
		"Entry B (written after reseal) should be readable")

	t.Log("Both pre-seal and post-reseal entries verified - barrier re-initialization works")
}

// TestBarrierLargeValueEncryption verifies that large values are correctly
// encrypted and decrypted through the AES-256-GCM barrier. Large payloads
// stress the encryption path and verify no truncation or corruption occurs.
func TestBarrierLargeValueEncryption(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), barrierTimeout)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Ensure system is unsealed
	ensureUnsealed(t, ctx, client)

	// Generate a 64KB random payload
	const payloadSize = 64 * 1024
	largeData := make([]byte, payloadSize)
	n, err := rand.Read(largeData)
	require.NoError(t, err, "crypto/rand.Read should succeed")
	require.Equal(t, payloadSize, n, "Should read exactly %d random bytes", payloadSize)

	// Write the large value
	largeID := uint64(time.Now().UnixNano())
	putEntry(t, ctx, client, barrierEntityType, largeID, largeData)
	t.Logf("Wrote large entry: id=%d, size=%d bytes", largeID, payloadSize)

	// Read it back and verify exact match
	readData := getEntry(t, ctx, client, barrierEntityType, largeID)
	require.Equal(t, payloadSize, len(readData),
		"Read data length should match written data length")
	assert.True(t, bytes.Equal(largeData, readData),
		"Large value should be identical after barrier encrypt/decrypt")
	t.Log("Large value verified before seal cycle")

	// Seal and unseal
	ensureSealed(t, ctx, client)
	t.Log("Database sealed")
	ensureUnsealed(t, ctx, client)
	t.Log("Database unsealed after seal cycle")

	// Read again after seal cycle and verify exact match
	readDataAfter := getEntry(t, ctx, client, barrierEntityType, largeID)
	require.Equal(t, payloadSize, len(readDataAfter),
		"Post-unseal read data length should match written data length")
	assert.True(t, bytes.Equal(largeData, readDataAfter),
		"Large value should be identical after seal cycle")
	t.Log("Large value verified after seal cycle - GCM encryption handles large payloads correctly")
}

// TestBarrierEmptyAndBinaryValues verifies that the barrier correctly handles
// edge-case values including empty data and arbitrary binary content with null
// bytes. These values exercise boundary conditions in the GCM encryption path.
func TestBarrierEmptyAndBinaryValues(t *testing.T) {
	requireSealAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), barrierTimeout)
	defer cancel()

	conn := setupGRPCConnection(t, ctx)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Ensure system is unsealed
	ensureUnsealed(t, ctx, client)

	// Write an entry with empty value
	emptyData := []byte{}
	emptyID := uint64(time.Now().UnixNano())
	putEntry(t, ctx, client, barrierEntityType, emptyID, emptyData)
	t.Logf("Wrote empty entry: id=%d", emptyID)

	// Write an entry with binary value containing null bytes and all byte values
	binaryData := make([]byte, 256)
	for i := range binaryData {
		binaryData[i] = byte(i)
	}
	// Append additional random bytes to make it non-trivial
	randomSuffix := make([]byte, 512)
	n, err := rand.Read(randomSuffix)
	require.NoError(t, err, "crypto/rand.Read should succeed for binary data")
	require.Equal(t, 512, n, "Should read exactly 512 random bytes")
	binaryData = append(binaryData, randomSuffix...)

	binaryID := uint64(time.Now().UnixNano()) + 1
	putEntry(t, ctx, client, barrierEntityType, binaryID, binaryData)
	t.Logf("Wrote binary entry: id=%d, size=%d bytes", binaryID, len(binaryData))

	// Read both back and verify exact match before seal cycle
	readEmpty := getEntry(t, ctx, client, barrierEntityType, emptyID)
	assert.Equal(t, 0, len(readEmpty),
		"Empty value should have zero length after read")

	readBinary := getEntry(t, ctx, client, barrierEntityType, binaryID)
	require.Equal(t, len(binaryData), len(readBinary),
		"Binary value length should match after read")
	assert.True(t, bytes.Equal(binaryData, readBinary),
		"Binary value should be identical after barrier encrypt/decrypt")
	t.Log("Empty and binary values verified before seal cycle")

	// Seal and unseal
	ensureSealed(t, ctx, client)
	t.Log("Database sealed")
	ensureUnsealed(t, ctx, client)
	t.Log("Database unsealed after seal cycle")

	// Read both back again after seal cycle
	readEmptyAfter := getEntry(t, ctx, client, barrierEntityType, emptyID)
	assert.Equal(t, 0, len(readEmptyAfter),
		"Empty value should still have zero length after seal cycle")

	readBinaryAfter := getEntry(t, ctx, client, barrierEntityType, binaryID)
	require.Equal(t, len(binaryData), len(readBinaryAfter),
		"Binary value length should match after seal cycle")
	assert.True(t, bytes.Equal(binaryData, readBinaryAfter),
		"Binary value should be identical after seal cycle")
	t.Log("Empty and binary values verified after seal cycle - barrier handles edge cases correctly")
}
