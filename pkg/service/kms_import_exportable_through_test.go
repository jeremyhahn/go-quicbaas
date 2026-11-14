package service

import (
	"crypto/rand"
	"fmt"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
)

// These tests import and unwrap keys through the service against the real
// go-xkms software backend, then ask go-xkms itself whether it will export the
// key. As with GenerateKey (#1768), the quicbaas metadata cannot answer that:
// go-xkms enforces exportability from the record it wrote at import (#1779).

// randomAES256Key returns fresh AES-256 key material.
func randomAES256Key(t *testing.T) []byte {
	t.Helper()

	material := make([]byte, 32)
	_, err := rand.Read(material)
	require.NoError(t, err)
	return material
}

// importSymmetricKey imports an AES-256-GCM key through the service.
func importSymmetricKey(t *testing.T, svc *kmsServiceImpl, name string, exportable bool) {
	t.Helper()

	_, err := svc.ImportKey(systemTenantCtx(), &ImportKeyRequest{
		Name:        name,
		KeyMaterial: randomAES256Key(t),
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			Exportable:         exportable,
		},
		Usage: []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
	})
	require.NoError(t, err)
}

// unwrapSymmetricKey wraps fresh AES-256 material under wrappingKey the way
// ExportKey does, then unwraps it through the service as name.
func unwrapSymmetricKey(t *testing.T, svc *kmsServiceImpl, wrappingKey, name string, exportable bool) {
	t.Helper()

	const sourceKeyID = "unwrap-source"
	const sourceVersion = 1
	ctx := systemTenantCtx()
	aad := []byte(fmt.Sprintf("export:%s:v%d", sourceKeyID, sourceVersion))
	wrapped, err := svc.kmsManager.Encrypt(ctx, wrappingKey, randomAES256Key(t), aad)
	require.NoError(t, err)

	_, err = svc.UnwrapKey(ctx, &UnwrapKeyRequest{
		KeyID:             name,
		Name:              name,
		WrappedKey:        wrapped,
		WrappingKeyID:     wrappingKey,
		WrappingAlgorithm: "AES-GCM",
		Algorithm:         string(KeyAlgAES256GCM),
		SourceKeyID:       sourceKeyID,
		SourceVersion:     sourceVersion,
		KeyAttributes:     &xkmssdk.KeyAttributes{Exportable: exportable},
	})
	require.NoError(t, err)
}

func TestImportKey_ThroughServiceToRealKMS_ExportableKeyExports(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	const keyName = "imported-exportable-aes"
	importSymmetricKey(t, svc, keyName, true)

	wrapped, err := xkmssdk.ExportKey(systemTenantCtx(), keyName, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	require.NoError(t, err, "go-xkms must hold the imported key as exportable, as the request asked")
	require.NotEmpty(t, wrapped.WrappedKey)
}

func TestImportKey_ThroughServiceToRealKMS_NonExportableKeyIsRefused(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	const keyName = "imported-sealed-aes"
	importSymmetricKey(t, svc, keyName, false)

	_, err := xkmssdk.ExportKey(systemTenantCtx(), keyName, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	require.ErrorIs(t, err, xkmssdk.ErrBackendKeyNotExportable,
		"go-xkms must find the imported key and refuse it as not exportable")
}

func TestUnwrapKey_ThroughServiceToRealKMS_ExportableKeyExports(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	const wrappingKey = "unwrap-wrapping-aes"
	const keyName = "unwrapped-exportable-aes"
	generateSymmetricKey(t, svc, wrappingKey, false)
	unwrapSymmetricKey(t, svc, wrappingKey, keyName, true)

	wrapped, err := xkmssdk.ExportKey(systemTenantCtx(), keyName, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	require.NoError(t, err, "go-xkms must hold the unwrapped key as exportable, as the request asked")
	require.NotEmpty(t, wrapped.WrappedKey)
}

func TestUnwrapKey_ThroughServiceToRealKMS_NonExportableKeyIsRefused(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	const wrappingKey = "unwrap-wrapping-aes"
	const keyName = "unwrapped-sealed-aes"
	generateSymmetricKey(t, svc, wrappingKey, false)
	unwrapSymmetricKey(t, svc, wrappingKey, keyName, false)

	_, err := xkmssdk.ExportKey(systemTenantCtx(), keyName, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	require.ErrorIs(t, err, xkmssdk.ErrBackendKeyNotExportable,
		"go-xkms must find the unwrapped key and refuse it as not exportable")
}

func TestUnwrapKey_ThroughServiceToRealKMS_UnwrappedKeyEncryptsAndDecrypts(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	ctx := systemTenantCtx()
	const wrappingKey = "unwrap-wrapping-aes"
	const keyName = "unwrapped-working-aes"
	generateSymmetricKey(t, svc, wrappingKey, false)
	unwrapSymmetricKey(t, svc, wrappingKey, keyName, false)

	plaintext := []byte("data the unwrapped key protects")
	aad := []byte("aad")

	ciphertext, err := manager.Encrypt(ctx, keyName, plaintext, aad)
	require.NoError(t, err, "a key brought in by UnwrapKey must encrypt")

	decrypted, err := manager.Decrypt(ctx, keyName, ciphertext, aad)
	require.NoError(t, err, "a key brought in by UnwrapKey must decrypt")
	require.Equal(t, plaintext, decrypted)
}
