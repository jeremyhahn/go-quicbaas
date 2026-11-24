package kms

import (
	"context"
	"crypto"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
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

// TestXKMSBackend_SignPSS_RefusesUnverifiedBackends proves backends that drop
// the PSS padding or the salt length are refused before any signing.
func TestXKMSBackend_SignPSS_RefusesUnverifiedBackends(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	unverified := []config.BackendType{
		config.BackendTypeTPM2,
		config.BackendTypeAWSKMS,
		config.BackendTypeGCPKMS,
		config.BackendTypeAzureKV,
		config.BackendTypeVault,
	}

	for _, bt := range unverified {
		t.Run(string(bt), func(t *testing.T) {
			backend, err := NewXKMSBackend(&XKMSBackendConfig{BackendType: bt, Logger: &mockLogger{}})
			require.NoError(t, err)
			_, err = backend.SignPSS(context.Background(), "k", make([]byte, 32), crypto.SHA256, 0)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrPSSNotSupported), "got %v", err)
		})
	}

	t.Run("supported list", func(t *testing.T) {
		assert.True(t, pssBackendSupported(config.BackendTypeSoftware))
		assert.True(t, pssBackendSupported(config.BackendTypePKCS11))
		for _, bt := range unverified {
			assert.False(t, pssBackendSupported(bt), string(bt))
		}
	})
}

// TestXKMSBackend_SignPSS_SaltLengths proves the software backend signs with
// the requested hash and the exact salt length.
func TestXKMSBackend_SignPSS_SaltLengths(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err)

	ctx := context.Background()
	rsaID := "pss-salt-rsa-2048"
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

	message := []byte("rsa-pss salt message")
	s256 := sha256.Sum256(message)
	s384 := sha512.Sum384(message)
	s512 := sha512.Sum512(message)

	cases := []struct {
		name   string
		hash   crypto.Hash
		digest []byte
		salt   int
	}{
		{"SHA256 salt 20", crypto.SHA256, s256[:], 20},
		{"SHA384 salt 0", crypto.SHA384, s384[:], 0},
		{"SHA384 salt 20", crypto.SHA384, s384[:], 20},
		{"SHA512 salt 64", crypto.SHA512, s512[:], 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig, err := backend.SignPSS(ctx, rsaID, tc.digest, tc.hash, tc.salt)
			require.NoError(t, err)

			verifySalt := tc.salt
			if verifySalt == 0 {
				verifySalt = rsa.PSSSaltLengthEqualsHash
			}
			require.NoError(t, rsa.VerifyPSS(pub, tc.hash, tc.digest, sig,
				&rsa.PSSOptions{SaltLength: verifySalt, Hash: tc.hash}))

			if tc.salt == 20 {
				assert.Error(t, rsa.VerifyPSS(pub, tc.hash, tc.digest, sig,
					&rsa.PSSOptions{SaltLength: 32, Hash: tc.hash}), "a wrong salt must not verify")
			}
		})
	}
}
