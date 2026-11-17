package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestKMSService_RotateKey_ResetsSafetyUsage checks that rotating a key
// clears the AEAD usage the Crypto service counted under it, so a key whose
// operation budget is exhausted encrypts again after RotateKey.
func TestKMSService_RotateKey_ResetsSafetyUsage(t *testing.T) {
	const keyID = "rotate-safety-key"
	ctx := systemTenantCtx()

	kmsMgr := newMockKMSManager()
	kmsMgr.keys[keyID] = &mockKeyData{
		keyID:     keyID,
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}
	logger := newMockLogger()
	kmsSvc := newKMSService(kmsMgr, logger)
	tracker := kmsSvc.GetSafetyTracker()
	require.NotNil(t, tracker)
	cryptoSvc := newCryptoService(newMockCryptoManager(), kmsMgr, logger, tracker)

	scoped, err := scopeKeyID(ctx, keyID)
	require.NoError(t, err)
	// The tracker refuses the operation that reaches MaxOperations, so with
	// MaxOperations=2 the first Encrypt passes and the second exhausts the key.
	tracker.SetKeyConfig(scoped, AEADSafetyConfig{MaxOperations: 2})

	encrypt := func() error {
		_, err := cryptoSvc.Encrypt(ctx, &EncryptRequest{KeyID: keyID, Plaintext: []byte("pt")})
		return err
	}
	require.NoError(t, encrypt())
	require.Error(t, encrypt(), "the key's operation budget is exhausted")

	_, err = kmsSvc.RotateKey(ctx, &RotateKeyRequest{KeyID: keyID})
	require.NoError(t, err)

	ops, _, _ := tracker.GetKeyUsage(scoped)
	require.Zero(t, ops)
	require.NoError(t, encrypt(), "the rotated key starts a fresh budget")
}
