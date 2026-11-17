package kms

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestXKMSBackend_TenantScopedKeyID_RealXKMS creates a tenant-scoped key (its
// ID carries "/", which go-xkms refuses in a key name) on a real software
// backend, encrypts and decrypts with it, and lists it back under its
// quicbaas ID, also from a second backend whose metadata cache is empty, so
// the ID is decoded from the name go-xkms stored.
func TestXKMSBackend_TenantScopedKeyID_RealXKMS(t *testing.T) {
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
	backend := newBackend()

	ctx := context.Background()
	const keyID = "tenant/alice/k1"
	attrs := &xkmssdk.KeyAttributes{
		CN:                 keyID,
		StoreType:          xkmssdk.StoreType("software"),
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
	meta, err := backend.CreateKey(ctx, attrs)
	require.NoError(t, err)
	assert.Equal(t, keyID, meta.KeyID)
	assert.Equal(t, keyID, attrs.CN, "CreateKey must not rewrite the caller's attrs")

	plaintext := []byte("tenant secret")
	aad := []byte("tenant/alice")
	ciphertext, err := backend.Encrypt(ctx, keyID, plaintext, aad)
	require.NoError(t, err)
	decrypted, err := backend.Decrypt(ctx, keyID, ciphertext, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	listedIDs := func(b *XKMSBackend, prefix string) []string {
		keys, _, err := b.ListKeys(ctx, prefix, 0)
		require.NoError(t, err)
		ids := make([]string, 0, len(keys))
		for _, k := range keys {
			ids = append(ids, k.KeyID)
		}
		return ids
	}
	assert.Contains(t, listedIDs(backend, "tenant/alice/"), keyID)

	fresh := newBackend()
	ids := listedIDs(fresh, "tenant/alice/")
	assert.Equal(t, []string{keyID}, ids)
	for _, id := range listedIDs(fresh, "") {
		assert.False(t, strings.HasPrefix(id, xkmsEncodedKeyPrefix), "listed an encoded name: %q", id)
	}

	decrypted, err = fresh.Decrypt(ctx, keyID, ciphertext, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestXKMSKeyName maps quicbaas key IDs to go-xkms key names and back.
func TestXKMSKeyName(t *testing.T) {
	tooLong := strings.Repeat("a", 130) + "/k"
	tests := []struct {
		name    string
		keyID   string
		want    string
		wantErr bool
	}{
		{name: "plain ID unchanged", keyID: "my-key", want: "my-key"},
		{name: "plain ID with colon unchanged", keyID: "my-key:v2", want: "my-key:v2"},
		{name: "tenant-scoped ID encoded", keyID: "tenant/alice/k1",
			want: xkmsEncodedKeyPrefix + hex.EncodeToString([]byte("tenant/alice/k1"))},
		{name: "encoding is lowercase hex", keyID: "T/A", want: "qbt.542f41"},
		{name: "reserved prefix refused", keyID: "qbt.6b31", wantErr: true},
		{name: "encoded name too long refused", keyID: tooLong, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := xkmsKeyName(tc.keyID)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrInvalidKeyID)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqual(t, len(got), xkmsMaxKeyNameLen)
			assert.Equal(t, tc.keyID, quicbaasKeyIDFromXKMSName(got))
		})
	}
}
