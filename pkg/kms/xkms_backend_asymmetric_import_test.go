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

package kms

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// Asymmetric import, proven end to end against a REAL KMS backend
// =============================================================================
//
// These tests exist because IMPORT SUCCESS IS WHAT LIED. The original defect
// was not a crash: `kms key import` stored PEM text as an AES key, returned
// success, and listed the key normally. Anything that asserts on the import
// call's error value would have passed against that bug.
//
// So the assertion here is a SIGNATURE VERIFIED AGAINST THE PUBLIC HALF of the
// key that went in. That is the only evidence separating a stored key from a
// stored blob that was called one, and it is not available at the cmd layer:
// cmd/quicbaas's tests swap kmsClientFactory for a mock, which can only confirm
// the arguments the test itself chose to build.
//
// The backend under test is the real go-xkms software backend, stood up
// in-process by initXKMSForTest. Every OTHER Sign test in this package runs
// against NewMockBackend, which is precisely why neither defect these tests
// cover was caught earlier.

// newRealXKMSBackendForTest returns an XKMSBackend over the real go-xkms
// software backend. The caller must have called initXKMSForTest first.
func newRealXKMSBackendForTest(t *testing.T) *XKMSBackend {
	t.Helper()

	backend, err := NewXKMSBackend(&XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	})
	require.NoError(t, err, "the real software backend must stand up")

	return backend
}

// TestXKMSBackend_ImportKey_Ed25519_SignsWithTheKeyThatWasImported is
// condition 3 of the KMS-import fix, and the regression for the whole class.
//
// Ed25519 is the case that matters most: it is the DEFAULT signing algorithm
// for agent commit signing, so a break here is a break in the common path.
func TestXKMSBackend_ImportKey_Ed25519_SignsWithTheKeyThatWasImported(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	// The key an operator would be importing: an existing agent signing key.
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	const keyID = "agent-commit-signing"

	_, err = backend.ImportKey(ctx, keyID, pkcs8, AlgEd25519, nil)
	require.NoError(t, err,
		"asymmetric import must succeed: go-xkms's software backend parses PKCS#8 "+
			"in importAsymmetricKey, so refusing here withholds a capability the "+
			"layer below already provides")

	// Ed25519 signs the MESSAGE, not a digest of it.
	message := []byte("the commit this key exists to sign")

	signature, err := backend.Sign(ctx, keyID, message, "")
	require.NoError(t, err,
		"signing must not pre-hash for Ed25519: crypto/ed25519 rejects any "+
			"SignerOpts whose HashFunc is not crypto.Hash(0)")

	require.True(t, ed25519.Verify(public, message, signature),
		"THE KMS SIGNED WITH A DIFFERENT KEY THAN THE ONE IMPORTED — this is the "+
			"assertion that import-returned-nil cannot make")
}

// TestXKMSBackend_ImportKey_ECDSAP256_SignsWithTheKeyThatWasImported covers the
// FIPS selection. Ed25519 is not FIPS-approved, so P-256 is what a regulated
// deployment imports, and it must be proven by the same standard.
//
// It is a distinct case rather than a table row because the two algorithms
// differ in the property under test: ECDSA signs a digest and Ed25519 signs a
// message, so a shared body would have to branch on exactly the thing that
// broke.
func TestXKMSBackend_ImportKey_ECDSAP256_SignsWithTheKeyThatWasImported(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	const keyID = "agent-commit-signing-fips"

	_, err = backend.ImportKey(ctx, keyID, pkcs8, AlgECDSAP256, nil)
	require.NoError(t, err, "asymmetric import must succeed for the FIPS selection too")

	digest := sha256.Sum256([]byte("the commit this key exists to sign"))

	signature, err := backend.Sign(ctx, keyID, digest[:], "SHA-256")
	require.NoError(t, err)

	require.True(t, ecdsa.VerifyASN1(&private.PublicKey, digest[:], signature),
		"the KMS signed with a different key than the one imported")
}

// TestXKMSBackend_ImportKey_RejectsMaterialThatIsNotTheDeclaredAlgorithm proves
// the backend does not simply believe its caller.
//
// The CLI cross-checks --algorithm against the key it parsed, but that check
// lives at the edge and only guards the one caller that performs it. This
// asserts the ENFORCEMENT boundary: material that contradicts the declared
// attributes is refused by the store itself, so a direct SDK or service caller
// cannot store a key under a false description either.
func TestXKMSBackend_ImportKey_RejectsMaterialThatIsNotTheDeclaredAlgorithm(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	// An Ed25519 key offered as though it were ECDSA P-256.
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	_, err = backend.ImportKey(ctx, "mislabelled", pkcs8, AlgECDSAP256, nil)
	require.ErrorIs(t, err, dbxkms.ErrKeyMaterialAlgorithmMismatch,
		"importing an Ed25519 key as ECDSA must be refused FOR THAT REASON: an "+
			"assertion that merely something failed would pass on an unrelated error")
}

// TestXKMSBackend_ImportKey_SymmetricStillImports is the guard on the other
// side of the change.
//
// Adding the asymmetric route must not cost the symmetric one. Raw AES bytes
// are exactly what the earlier armour-detection guard exists to keep working,
// and a routing change is the most plausible way to break them.
func TestXKMSBackend_ImportKey_SymmetricStillImports(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	material := make([]byte, 32)
	_, err := rand.Read(material)
	require.NoError(t, err)

	_, err = backend.ImportKey(ctx, "tenant-dek", material, AlgAES256GCM, nil)
	require.NoError(t, err, "raw symmetric material must still import unchanged")
}

// TestXKMSBackend_ImportKey_RejectsAMisdescribedCurve is the regression for a
// finding a boundary review caught: comparing only the algorithm FAMILY.
//
// A P-384 key declared as P-256 passed, and it is the likely shape of the
// mistake rather than an exotic one — a caller that names no curve at all is
// defaulted to P-256 downstream, so "ECDSA" plus a P-384 key silently became
// "this is a P-256 key". The curve is part of the key's identity, not a detail
// of it.
func TestXKMSBackend_ImportKey_RejectsAMisdescribedCurve(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	// A P-384 key, offered as P-256.
	private, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	_, err = backend.ImportKey(ctx, "wrong-curve", pkcs8, AlgECDSAP256, nil)
	require.ErrorIs(t, err, dbxkms.ErrKeyMaterialAlgorithmMismatch,
		"a P-384 key stored as P-256 misdescribes itself in exactly the dimension "+
			"the caller was being specific about")
}

// TestXKMSBackend_ImportKey_AcceptsTheMatchingCurve keeps the curve check from
// passing by refusing everything.
func TestXKMSBackend_ImportKey_AcceptsTheMatchingCurve(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	backend := newRealXKMSBackendForTest(t)
	ctx := context.Background()

	private, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)

	_, err = backend.ImportKey(ctx, "right-curve", pkcs8, AlgECDSAP384, nil)
	require.NoError(t, err, "a P-384 key declared P-384 must import")
}
