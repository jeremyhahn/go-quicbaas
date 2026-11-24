package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBatchSafetyCryptoService builds a crypto service whose tracker enforces
// maxOps operations per key (bytes are not limited).
func newBatchSafetyCryptoService(maxOps uint64) (*cryptoService, *AEADSafetyTracker) {
	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{MaxOperations: maxOps}),
		WithEnforceDefaults(true),
	)
	return newCryptoService(newMockCryptoManager(), newMockKMSManager(), newMockLogger(), tracker), tracker
}

func batchItems(n int) []BatchEncryptItem {
	items := make([]BatchEncryptItem, n)
	for i := range items {
		items[i] = BatchEncryptItem{Plaintext: []byte("pt")}
	}
	return items
}

// TestCryptoService_EncryptBatch_SafetyCountsEveryItem checks that a batch
// records one operation per item, so a batch cannot pass a per-key operation
// limit that the same items sent one by one would hit.
func TestCryptoService_EncryptBatch_SafetyCountsEveryItem(t *testing.T) {
	ctx := systemTenantCtx()

	t.Run("batch of three refused at MaxOperations=2", func(t *testing.T) {
		svc, tracker := newBatchSafetyCryptoService(2)
		resp, err := svc.EncryptBatch(ctx, &EncryptBatchRequest{KeyID: "batch-key", Items: batchItems(3)})
		require.Error(t, err)
		assert.Nil(t, resp)

		scoped, err := scopeKeyID(ctx, "batch-key")
		require.NoError(t, err)
		ops, _, _ := tracker.GetKeyUsage(scoped)
		assert.Equal(t, uint64(3), ops)
	})

	t.Run("batch of one after one Encrypt accepted under the limit", func(t *testing.T) {
		// The tracker refuses the operation that reaches MaxOperations, so
		// with MaxOperations=3 an Encrypt and a one-item batch (2 operations)
		// pass, and a further two-item batch (4) is refused.
		svc, _ := newBatchSafetyCryptoService(3)
		_, err := svc.Encrypt(ctx, &EncryptRequest{KeyID: "batch-key-2", Plaintext: []byte("pt")})
		require.NoError(t, err)

		resp, err := svc.EncryptBatch(ctx, &EncryptBatchRequest{KeyID: "batch-key-2", Items: batchItems(1)})
		require.NoError(t, err)
		require.Len(t, resp.Results, 1)

		_, err = svc.EncryptBatch(ctx, &EncryptBatchRequest{KeyID: "batch-key-2", Items: batchItems(2)})
		require.Error(t, err)
	})
}

// TestCryptoService_DecryptBatch_SafetyCountsEveryItem checks the decrypt
// twin: a batch of three is refused at MaxOperations=2.
func TestCryptoService_DecryptBatch_SafetyCountsEveryItem(t *testing.T) {
	ctx := systemTenantCtx()
	svc, _ := newBatchSafetyCryptoService(2)

	items := make([]BatchDecryptItem, 3)
	for i := range items {
		items[i] = BatchDecryptItem{Ciphertext: []byte("crypto:encrypted:pt")}
	}
	resp, err := svc.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: "batch-dec-key", Items: items})
	require.Error(t, err)
	assert.Nil(t, resp)

	resp, err = svc.DecryptBatch(ctx, &DecryptBatchRequest{KeyID: "batch-dec-key-2", Items: items[:1]})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
}
