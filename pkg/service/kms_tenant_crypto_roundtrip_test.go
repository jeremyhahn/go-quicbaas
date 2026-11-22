package service

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/crypto"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestCryptoVerbs_NonSystemTenant_RealSoftwareBackend drives the crypto verbs
// for a non-system tenant's own keys over the real software backend: the
// tenant-scoped key IDs must reach go-xkms under a name it accepts.
func TestCryptoVerbs_NonSystemTenant_RealSoftwareBackend(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	kmsSvc := newKMSService(manager, newMockLogger())
	cryptoSvc := newCryptoService(crypto.NewMemoryCryptoManager(manager), manager, newMockLogger(), nil)

	ctx := tenantctx.WithTenant(context.Background(), "acme")

	_, err := kmsSvc.GenerateKey(ctx, &GenerateKeyRequest{
		Name: "k1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
	})
	require.NoError(t, err, "tenant acme generates a symmetric key")

	_, err = kmsSvc.GenerateKey(ctx, &GenerateKeyRequest{
		Name: "s1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			KeyType:       xkmssdk.KeyTypeSigning,
			KeyAlgorithm:  x509.ECDSA,
			ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
		},
		Usage: []KeyUsage{KeyUsageSign, KeyUsageVerify},
	})
	require.NoError(t, err, "tenant acme generates a signing key")

	// Encrypt / Decrypt.
	plaintext := []byte("tenant acme secret")
	enc, err := cryptoSvc.Encrypt(ctx, &EncryptRequest{KeyID: "k1", Plaintext: plaintext})
	require.NoError(t, err)
	dec, err := cryptoSvc.Decrypt(ctx, &DecryptRequest{KeyID: "k1", Ciphertext: enc.Ciphertext})
	require.NoError(t, err)
	assert.Equal(t, plaintext, dec.Plaintext)

	// Sign / Verify.
	message := []byte("tenant acme message")
	sig, err := cryptoSvc.Sign(ctx, &SignRequest{KeyID: "s1", Message: message, HashAlgorithm: HashAlgSHA256})
	require.NoError(t, err)
	ver, err := cryptoSvc.Verify(ctx, &VerifyRequest{
		KeyID:         "s1",
		Message:       message,
		Signature:     sig.Signature,
		HashAlgorithm: HashAlgSHA256,
	})
	require.NoError(t, err)
	assert.True(t, ver.Valid)

	// GenerateDataKey: the wrapped key decrypts back to the plaintext key.
	dk, err := cryptoSvc.GenerateDataKey(ctx, &GenerateDataKeyRequest{
		KeyID: "k1",
		KeyAttributes: &xkmssdk.KeyAttributes{
			KeyType:            xkmssdk.KeyTypeEncryption,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, dk.Plaintext)
	unwrapped, err := cryptoSvc.Decrypt(ctx, &DecryptRequest{KeyID: "k1", Ciphertext: dk.CiphertextBlob})
	require.NoError(t, err)
	assert.Equal(t, dk.Plaintext, unwrapped.Plaintext)

	// Isolation: another tenant cannot use acme's key by its name.
	otherCtx := tenantctx.WithTenant(context.Background(), "other")
	_, err = cryptoSvc.Encrypt(otherCtx, &EncryptRequest{KeyID: "k1", Plaintext: plaintext})
	assert.Error(t, err, "tenant other must not encrypt with acme's k1")
}
