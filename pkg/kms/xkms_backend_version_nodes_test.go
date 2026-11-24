package kms

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestXKMSBackend_EncryptFollowsRotationOnOtherBackend_RealXKMS runs two
// backends over one go-xkms store, as two nodes do. A rotation on one must
// move the other forward: it encrypts under the new version and its own
// rotation creates the next version instead of regenerating the same one.
func TestXKMSBackend_EncryptFollowsRotationOnOtherBackend_RealXKMS(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	newBackend := func() *XKMSBackend {
		b, err := NewXKMSBackend(&XKMSBackendConfig{
			BackendType: config.BackendTypeSoftware,
			Logger:      &mockLogger{},
		})
		require.NoError(t, err)
		return b
	}
	a, b := newBackend(), newBackend()

	ctx := context.Background()
	const keyID = "rot-follow-nodes"
	_, err := a.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 keyID,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, err)

	sealVersion := func(be *XKMSBackend, pt string) []byte {
		t.Helper()
		blob, encErr := be.Encrypt(ctx, keyID, []byte(pt), nil)
		require.NoError(t, encErr)
		return blob
	}
	versionOf := func(blob []byte) int {
		t.Helper()
		v, ok := CiphertextKeyVersion(blob)
		require.True(t, ok)
		return v
	}

	require.Equal(t, 1, versionOf(sealVersion(b, "before rotation")))

	_, err = a.RotateKey(ctx, keyID)
	require.NoError(t, err)
	blob := sealVersion(b, "after rotation on a")
	require.Equal(t, 2, versionOf(blob), "b must follow a's rotation")
	for _, be := range []*XKMSBackend{a, b} {
		got, decErr := be.Decrypt(ctx, keyID, blob, nil)
		require.NoError(t, decErr)
		require.Equal(t, []byte("after rotation on a"), got)
	}

	meta, err := b.RotateKey(ctx, keyID)
	require.NoError(t, err)
	require.Equal(t, 3, meta.Version, "b's rotation must create the next version, not a second v2")
	require.Equal(t, 3, versionOf(sealVersion(a, "after rotation on b")))
}
