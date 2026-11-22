package kms

import (
	"context"
	"crypto"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestXKMSBackend_SignPSS_Software(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	rsaID := "pss-rsa-2048"
	_, err = backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
		CN:            rsaID,
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.RSA,
		RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048},
	})
	require.NoError(t, err)

	der, err := backend.ExportPublicKey(ctx, rsaID)
	require.NoError(t, err)
	parsed, err := x509.ParsePKIXPublicKey(der)
	require.NoError(t, err)
	pub, ok := parsed.(*rsa.PublicKey)
	require.True(t, ok, "expected an RSA public key, got %T", parsed)

	sum := sha256.Sum256([]byte("rsa-pss message"))
	digest := sum[:]

	t.Run("salt 0 equals hash length", func(t *testing.T) {
		sig, err := backend.SignPSS(ctx, rsaID, digest, crypto.SHA256, 0)
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPSS(pub, crypto.SHA256, digest, sig,
			&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256}))
	})

	t.Run("explicit salt 32", func(t *testing.T) {
		sig, err := backend.SignPSS(ctx, rsaID, digest, crypto.SHA256, 32)
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPSS(pub, crypto.SHA256, digest, sig,
			&rsa.PSSOptions{SaltLength: 32, Hash: crypto.SHA256}))
	})

	t.Run("Sign stays PKCS1v15", func(t *testing.T) {
		sig, err := backend.Sign(ctx, rsaID, digest, "SHA256")
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest, sig))
	})

	t.Run("ECDSA key refuses PSS", func(t *testing.T) {
		ecID := "pss-ecdsa-p256"
		_, err := backend.CreateKey(ctx, &xkmssdk.KeyAttributes{
			CN:            ecID,
			KeyType:       xkmssdk.KeyTypeSigning,
			KeyAlgorithm:  x509.ECDSA,
			ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
		})
		require.NoError(t, err)

		_, err = backend.SignPSS(ctx, ecID, digest, crypto.SHA256, 0)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrPSSNotSupported), "got %v", err)
	})
}
