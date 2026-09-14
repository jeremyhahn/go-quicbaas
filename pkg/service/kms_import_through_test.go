// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package service

import (
	"context"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// Key import through the service layer to a REAL KMS
// =============================================================================
//
// This closes the one link in the import path that was verified by READING
// rather than by a test.
//
// The CLI builds an ImportKeyRequest carrying KeyAttributes. This layer does
// NOT pass those attributes down: it collapses them to an algorithm STRING via
// attrsToAlgorithm, and the backend expands that string back into attributes.
// A string round-trip through two independent mappings is the kind of seam
// where a case difference or a renamed constant loses the algorithm silently —
// and the failure would look exactly like the original defect, an import that
// succeeds and yields a key that cannot sign.
//
// So the assertion is again a signature verified against the public half. The
// CLI's half (that it sends PKCS#8 and the right attributes) is covered in
// cmd/quicbaas; the backend's half in pkg/kms. This is the middle.

// realKMSManagerForTest builds a KMS manager over the real go-xkms software
// backend and returns it with a cleanup function.
func realKMSManagerForTest(t *testing.T) (*kms.KMSService, func()) {
	t.Helper()

	dbxkms.Reset()

	keyStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	certStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)

	softwareBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: keyStorage,
	})
	require.NoError(t, err)

	serviceBackend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     softwareBackend,
		CertStorage: certStorage,
	})
	require.NoError(t, err)

	require.NoError(t, dbxkms.Initialize(&dbxkms.Config{
		DefaultBackend: "software",
		Backends:       map[string]xkmssdk.ServiceBackend{"software": serviceBackend},
		Logger:         newMockLogger(),
	}))

	backend, err := kms.NewXKMSBackend(&kms.XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      newMockLogger(),
	})
	require.NoError(t, err)

	registry := kms.NewBackendRegistry(nil)
	require.NoError(t, registry.Register("software", backend, 100))

	manager, err := kms.NewKMSService(&kms.KMSServiceOptions{Registry: registry})
	require.NoError(t, err)

	return manager, func() { dbxkms.Reset() }
}

// TestImportKey_ThroughServiceToRealKMS_SignsWithTheImportedKey proves the
// attribute-to-string-to-attribute round trip preserves the key's algorithm.
func TestImportKey_ThroughServiceToRealKMS_SignsWithTheImportedKey(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	ctx := context.Background()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	// Exactly the request `quicbaas kms key import` builds for an OpenSSH
	// Ed25519 key: attributes describing the key, PKCS#8 material, sign usage.
	const keyName = "agent-commit-signing"
	resp, err := svc.ImportKey(ctx, &ImportKeyRequest{
		KeyAttributes: &xkmssdk.KeyAttributes{
			CN:           keyName,
			KeyAlgorithm: x509.Ed25519,
		},
		Name:        keyName,
		KeyMaterial: pkcs8,
		Usage:       []KeyUsage{KeyUsageSign},
	})
	require.NoError(t, err, "the CLI-shaped import request must survive this layer")
	require.NotNil(t, resp)

	require.Equal(t, KeyAlgorithm("ED25519"), resp.Key.Algorithm,
		"the algorithm must survive the attributes-to-string collapse")

	// The proof. Ed25519 signs the message itself.
	message := []byte("the commit this key exists to sign")

	signature, err := manager.Sign(ctx, keyName, message, "")
	require.NoError(t, err)

	require.True(t, ed25519.Verify(public, message, signature),
		"the key stored through the service is not the key that was imported")
}

// TestImportKey_ThroughServiceToRealKMS_RefusesContradictoryAttributes proves
// the refusal survives the same collapse.
//
// A guard that holds when called directly but is bypassed by the path callers
// actually take is not a guard, so it is asserted where the CLI's request
// enters rather than only at the backend.
func TestImportKey_ThroughServiceToRealKMS_RefusesContradictoryAttributes(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	svc := newKMSService(manager, newMockLogger())
	ctx := context.Background()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	// An Ed25519 key described as ECDSA P-256.
	_, err = svc.ImportKey(ctx, &ImportKeyRequest{
		KeyAttributes: &xkmssdk.KeyAttributes{
			CN:            "mislabelled",
			KeyAlgorithm:  x509.ECDSA,
			ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
		},
		Name:        "mislabelled",
		KeyMaterial: pkcs8,
		Usage:       []KeyUsage{KeyUsageSign},
	})
	require.ErrorIs(t, err, dbxkms.ErrKeyMaterialAlgorithmMismatch,
		"the refusal must survive this path FOR THE SAME REASON: asserting only "+
			"that something failed would pass on an unrelated error and hide a "+
			"guard that had stopped working")
}
