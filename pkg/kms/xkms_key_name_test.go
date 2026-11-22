package kms

import (
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestXKMSKeyName_RoundTrip(t *testing.T) {
	for _, id := range []string{"k1", "my.key-1", "tenant/acme/k1", "tenant/acme-corp/k1_v2", "tenant/a_b/x.y"} {
		assert.Equal(t, id, serviceKeyID(xkmsKeyName(id)), "round trip of %q", id)
	}

	assert.Equal(t, "k1", xkmsKeyName("k1"))
	assert.Equal(t, "my.key-1", xkmsKeyName("my.key-1"))
	assert.Equal(t, "tenant.acme.k1", xkmsKeyName("tenant/acme/k1"))
	assert.Equal(t, "tenant.acme-corp.k1_v2", xkmsKeyName("tenant/acme-corp/k1_v2"))

	for _, malformed := range []string{"tenant/", "tenant//k1", "tenant/acme/"} {
		assert.Equal(t, malformed, xkmsKeyName(malformed), "malformed %q is unchanged", malformed)
	}
}

func TestCheckUntenantedKeyName(t *testing.T) {
	assert.True(t, errors.Is(checkUntenantedKeyName("tenant.acme.k1"), ErrReservedKeyName))
	assert.NoError(t, checkUntenantedKeyName("tenant/acme/k1"))
	assert.NoError(t, checkUntenantedKeyName("k1"))
}

func TestXKMSBackend_TenantScopedKey_SoftwareRoundTrip(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()

	symAttrs := func(cn string) *xkmssdk.KeyAttributes {
		return &xkmssdk.KeyAttributes{
			CN:                 cn,
			StoreType:          xkmssdk.StoreType("software"),
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		}
	}

	// Tenant acme's symmetric key.
	attrs := symAttrs("tenant/acme/k1")
	meta, err := backend.CreateKey(ctx, attrs)
	require.NoError(t, err)
	assert.Equal(t, "tenant/acme/k1", meta.KeyID)
	assert.Equal(t, "tenant/acme/k1", attrs.CN, "the caller's attrs keep the service ID")

	plaintext := []byte("tenant acme plaintext")
	ciphertext, err := backend.Encrypt(ctx, "tenant/acme/k1", plaintext, nil)
	require.NoError(t, err)
	decrypted, err := backend.Decrypt(ctx, "tenant/acme/k1", ciphertext, nil)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	// Tenant acme's signing key.
	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:            "tenant/acme/s1",
		StoreType:     xkmssdk.StoreType("software"),
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
	})
	require.NoError(t, err)

	digest := sha256.Sum256([]byte("tenant acme message"))
	signature, err := backend.Sign(ctx, "tenant/acme/s1", digest[:], "SHA-256")
	require.NoError(t, err)
	valid, err := backend.Verify(ctx, "tenant/acme/s1", digest[:], signature, "SHA-256")
	require.NoError(t, err)
	assert.True(t, valid)

	// A system key named like the tenant's key is a different key.
	_, err = backend.CreateKey(ctx, symAttrs("k1"))
	require.NoError(t, err)
	sysPlain := []byte("system plaintext")
	sysCipher, err := backend.Encrypt(ctx, "k1", sysPlain, nil)
	require.NoError(t, err)
	sysDecrypted, err := backend.Decrypt(ctx, "k1", sysCipher, nil)
	require.NoError(t, err)
	assert.Equal(t, sysPlain, sysDecrypted)
	_, err = backend.Decrypt(ctx, "k1", ciphertext, nil)
	assert.Error(t, err, "the system key must not decrypt the tenant's ciphertext")

	// Listing by the tenant prefix returns the service IDs.
	keys, _, err := backend.ListKeys(ctx, "tenant/acme/", 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.KeyID)
	}
	assert.Contains(t, ids, "tenant/acme/k1")
	assert.Contains(t, ids, "tenant/acme/s1")
	assert.NotContains(t, ids, "k1")

	// An untenanted name in the reserved backend namespace is refused.
	_, err = backend.CreateKey(ctx, symAttrs("tenant.acme.k1"))
	assert.ErrorIs(t, err, ErrReservedKeyName)
}
