package service

import (
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// These tests generate keys through the service against the real go-xkms
// software backend, then ask go-xkms itself about the key it stored. The
// quicbaas metadata cannot stand in for that answer: it is what the service
// wrote, not what the backend enforces (#1768).

// generateSymmetricKey generates an AES-256-GCM key through the service with
// no KeyType, the way a JSON request that omits it arrives.
func generateSymmetricKey(t *testing.T, svc *kmsServiceImpl, name string, exportable bool) {
	t.Helper()

	_, err := svc.GenerateKey(systemTenantCtx(), &GenerateKeyRequest{
		Name: name,
		KeyAttributes: &xkmssdk.KeyAttributes{
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
		Usage:      []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		Exportable: exportable,
	})
	require.NoError(t, err)
}

func TestGenerateKey_ThroughServiceToRealKMS_ExportableSymmetricKeyExports(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	const keyName = "exportable-aes"
	generateSymmetricKey(t, svc, keyName, true)

	wrapped, err := xkmssdk.ExportKey(systemTenantCtx(), keyName, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	require.NoError(t, err, "go-xkms must hold the key as exportable, as the request asked")
	require.NotEmpty(t, wrapped.WrappedKey)
}

func TestGenerateKey_ThroughServiceToRealKMS_NonExportableSymmetricKeyIsRefused(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	const keyName = "sealed-aes"
	generateSymmetricKey(t, svc, keyName, false)

	_, err := xkmssdk.ExportKey(systemTenantCtx(), keyName, xkmssdk.WrappingAlgorithmRSAES_OAEP_SHA_256)
	require.ErrorIs(t, err, xkmssdk.ErrBackendKeyNotExportable,
		"go-xkms must find the key and refuse it as not exportable, not miss it")
}

func TestGenerateKey_ThroughServiceToRealKMS_SymmetricDefaultTypeRoundTrips(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	ctx := systemTenantCtx()
	const keyName = "default-type-aes"
	generateSymmetricKey(t, svc, keyName, false)

	// go-xkms's symmetric provider keeps only "secret" keys as symmetric.
	_, err := dbxkms.GetSymmetricKey(ctx, "software:secret:aes256-gcm:"+keyName)
	require.NoError(t, err, "a symmetric key with no KeyType must be stored as KeyTypeSecret")

	plaintext := []byte("data the default-typed key protects")
	aad := []byte("aad")

	ciphertext, err := manager.Encrypt(ctx, keyName, plaintext, aad)
	require.NoError(t, err)

	decrypted, err := manager.Decrypt(ctx, keyName, ciphertext, aad)
	require.NoError(t, err)
	require.Equal(t, plaintext, decrypted)
}
