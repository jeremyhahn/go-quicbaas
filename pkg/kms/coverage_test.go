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
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// pkixBackend embeds MockBackend but returns real PKIX-DER public keys from
// ExportPublicKey so the full KMSService.Signer success path (which parses the
// DER via x509.ParsePKIXPublicKey) can be exercised. Each registered key gets a
// deterministic in-memory keypair.
type pkixBackend struct {
	*MockBackend
	pubDER map[string][]byte
}

func newPKIXBackend() *pkixBackend {
	return &pkixBackend{
		MockBackend: NewMockBackend(),
		pubDER:      make(map[string][]byte),
	}
}

// addRSAKey creates an RSA key in the backend and records its PKIX DER.
func (b *pkixBackend) addRSAKey(t *testing.T, keyID string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	b.register(t, keyID, AlgRSA2048, &priv.PublicKey)
}

// addECDSAKey creates an ECDSA key in the backend and records its PKIX DER.
func (b *pkixBackend) addECDSAKey(t *testing.T, keyID string, curve elliptic.Curve, alg string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(curve, rand.Reader)
	require.NoError(t, err)
	b.register(t, keyID, alg, &priv.PublicKey)
}

// addEd25519Key creates an Ed25519 key in the backend and records its PKIX DER.
func (b *pkixBackend) addEd25519Key(t *testing.T, keyID string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	b.register(t, keyID, AlgEd25519, pub)
}

func (b *pkixBackend) register(t *testing.T, keyID, alg string, pub crypto.PublicKey) {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	b.pubDER[keyID] = der
	_, err = b.MockBackend.CreateKey(context.Background(), testKeyAttrs(keyID, alg))
	require.NoError(t, err)
}

func (b *pkixBackend) ExportPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if der, ok := b.pubDER[keyID]; ok {
		return der, nil
	}
	return b.MockBackend.ExportPublicKey(ctx, keyID)
}

// =============================================================================
// signAlgorithmForKey — pure dispatch over all supported key types
// =============================================================================

func TestSignAlgorithmForKey_AllSupportedTypes(t *testing.T) {
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	p521, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	require.NoError(t, err)

	edPub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	tests := []struct {
		name string
		pub  crypto.PublicKey
		want string
	}{
		{"rsa", &rsaPriv.PublicKey, "SHA256withRSA"},
		{"ecdsa-p256", &p256.PublicKey, "ECDSA-P256-SHA256"},
		{"ecdsa-p384", &p384.PublicKey, "ECDSA-P384-SHA384"},
		{"ecdsa-p521", &p521.PublicKey, "ECDSA-P521-SHA512"},
		{"ed25519", edPub, "Ed25519"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := signAlgorithmForKey(tt.pub)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSignAlgorithmForKey_UnsupportedCurve(t *testing.T) {
	// P-224 is a valid curve but not one the signer layer supports.
	priv, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	require.NoError(t, err)

	_, err = signAlgorithmForKey(&priv.PublicKey)
	require.Error(t, err)
	var curveErr *UnsupportedCurveError
	require.True(t, errors.As(err, &curveErr))
	assert.Equal(t, 224, curveErr.BitSize)
	assert.Contains(t, curveErr.Error(), "P-224")
}

func TestSignAlgorithmForKey_UnsupportedType(t *testing.T) {
	// A plain string is not a recognised public key type.
	_, err := signAlgorithmForKey("not-a-key")
	require.Error(t, err)
	var typeErr *UnsupportedKeyTypeError
	require.True(t, errors.As(err, &typeErr))
	assert.Contains(t, typeErr.Error(), "unsupported public key type")
}

// =============================================================================
// KMSService.Signer — success paths and guards
// =============================================================================

func TestKMSService_Signer_RSA_SuccessAndSign(t *testing.T) {
	backend := newPKIXBackend()
	backend.addRSAKey(t, "rsa-signer")

	svc, err := NewKMSService(&KMSServiceOptions{Registry: registryWithBackend(backend)})
	require.NoError(t, err)
	defer svc.Close()

	signer, err := svc.Signer(context.Background(), "rsa-signer")
	require.NoError(t, err)
	require.NotNil(t, signer)

	// Public() must return the cached RSA public key.
	_, ok := signer.Public().(*rsa.PublicKey)
	assert.True(t, ok, "expected an *rsa.PublicKey from Public()")

	// Sign() forwards to the backend; the mock returns a non-empty signature.
	digest := make([]byte, 32)
	sig, err := signer.Sign(rand.Reader, digest, crypto.SHA256)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)
}

func TestKMSService_Signer_ECDSAAndEd25519(t *testing.T) {
	backend := newPKIXBackend()
	backend.addECDSAKey(t, "ec-signer", elliptic.P256(), AlgECDSAP256)
	backend.addEd25519Key(t, "ed-signer")

	svc, err := NewKMSService(&KMSServiceOptions{Registry: registryWithBackend(backend)})
	require.NoError(t, err)
	defer svc.Close()

	ecSigner, err := svc.Signer(context.Background(), "ec-signer")
	require.NoError(t, err)
	_, ok := ecSigner.Public().(*ecdsa.PublicKey)
	assert.True(t, ok)

	edSigner, err := svc.Signer(context.Background(), "ed-signer")
	require.NoError(t, err)
	_, ok = edSigner.Public().(ed25519.PublicKey)
	assert.True(t, ok)
}

func TestKMSService_Signer_EmptyKeyID(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{Registry: registryWithBackend(backend)})
	require.NoError(t, err)
	defer svc.Close()

	_, err = svc.Signer(context.Background(), "")
	require.Error(t, err)
	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.True(t, errors.Is(err, ErrKeyIDEmpty))
}

func TestKMSService_Signer_NilService(t *testing.T) {
	var svc *KMSService
	_, err := svc.Signer(context.Background(), "any")
	require.Error(t, err)
	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
	assert.True(t, errors.Is(err, ErrServiceNotInitialized))
}

func TestKMSService_Signer_GetPublicKeyError(t *testing.T) {
	backend := NewMockBackend()
	svc, err := NewKMSService(&KMSServiceOptions{Registry: registryWithBackend(backend)})
	require.NoError(t, err)
	defer svc.Close()

	// Key does not exist -> GetPublicKey fails -> Signer propagates the error.
	_, err = svc.Signer(context.Background(), "missing-key")
	require.Error(t, err)
}

func TestKMSService_Signer_InvalidPKIXDER(t *testing.T) {
	// The default MockBackend returns "mock-public-key-..." which is not valid
	// PKIX DER, so ParsePKIXPublicKey fails inside Signer.
	backend := NewMockBackend()
	_, err := backend.CreateKey(context.Background(), testKeyAttrs("rsa-bad", AlgRSA2048))
	require.NoError(t, err)

	svc, err := NewKMSService(&KMSServiceOptions{Registry: registryWithBackend(backend)})
	require.NoError(t, err)
	defer svc.Close()

	_, err = svc.Signer(context.Background(), "rsa-bad")
	require.Error(t, err)
	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestKMSService_Signer_UnsupportedKeyType(t *testing.T) {
	// An ECDSA P-224 key parses fine as PKIX but is rejected by
	// signAlgorithmForKey, exercising the algorithm-derivation error branch.
	priv, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	backend := newPKIXBackend()
	backend.pubDER["weird"] = der
	_, err = backend.MockBackend.CreateKey(context.Background(), testKeyAttrs("weird", AlgECDSAP256))
	require.NoError(t, err)

	svc, err := NewKMSService(&KMSServiceOptions{Registry: registryWithBackend(backend)})
	require.NoError(t, err)
	defer svc.Close()

	_, err = svc.Signer(context.Background(), "weird")
	require.Error(t, err)
	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

// kmsSigner.Sign defensive nil guard.
func TestKMSSigner_Sign_NilGuard(t *testing.T) {
	var s *kmsSigner
	_, err := s.Sign(rand.Reader, make([]byte, 32), crypto.SHA256)
	require.Error(t, err)
	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.True(t, errors.Is(err, ErrSignerNotInitialized))

	// Also the zero-value struct (svc nil) path.
	s2 := &kmsSigner{}
	_, err = s2.Sign(rand.Reader, make([]byte, 32), crypto.SHA256)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSignerNotInitialized))
}

// =============================================================================
// signer_errors.go — Error() methods
// =============================================================================

func TestSignerErrors_ErrorStrings(t *testing.T) {
	keyTypeErr := &UnsupportedKeyTypeError{}
	assert.Equal(t, "unsupported public key type for kms signer", keyTypeErr.Error())

	curveErr := &UnsupportedCurveError{BitSize: 192}
	assert.Equal(t, "unsupported ecdsa curve: P-192", curveErr.Error())
}

// =============================================================================
// router.go — FailoverExhaustedError.Unwrap
// =============================================================================

func TestFailoverExhaustedError_Unwrap(t *testing.T) {
	cause := errors.New("backend down")
	err := &FailoverExhaustedError{Attempts: 3, LastErr: cause}

	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
	assert.Contains(t, err.Error(), "3 tries")
	assert.Contains(t, err.Error(), "backend down")

	// Nil cause: Error must not append a cause suffix and Unwrap returns nil.
	noCause := &FailoverExhaustedError{Attempts: 1}
	assert.Nil(t, noCause.Unwrap())
	assert.NotContains(t, noCause.Error(), ": ")
}

// =============================================================================
// key_cache.go — typed error Error() methods
// =============================================================================

func TestKeyCacheTypedErrors_ErrorStrings(t *testing.T) {
	tooLarge := &KeyTooLargeError{Size: 5000, Limit: 4096}
	assert.Contains(t, tooLarge.Error(), "5000 bytes")
	assert.Contains(t, tooLarge.Error(), "4096")
	assert.True(t, errors.Is(tooLarge, ErrKeyTooLarge))

	ttlErr := &TTLTooLargeError{TTL: 2 * time.Hour, MaxTTL: time.Hour}
	assert.Contains(t, ttlErr.Error(), "2h0m0s")
	assert.Contains(t, ttlErr.Error(), "1h0m0s")
	assert.True(t, errors.Is(ttlErr, ErrTTLTooLarge))
}

// =============================================================================
// storage_adapter.go — PutIfAbsent and Scan
// =============================================================================

func TestKVStoreStorageAdapter_PutIfAbsent(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)
	concrete := adapter.(*KVStoreStorageAdapter)

	// First write succeeds (key absent).
	stored, err := concrete.PutIfAbsent(ctx, "k1", []byte("v1"))
	require.NoError(t, err)
	assert.True(t, stored)

	// Second write is a no-op (key present).
	stored, err = concrete.PutIfAbsent(ctx, "k1", []byte("v2"))
	require.NoError(t, err)
	assert.False(t, stored)

	// Value must be the original.
	got, err := concrete.Get(ctx, "k1")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), got)
}

func TestKVStoreStorageAdapter_PutIfAbsent_ExistsError(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)
	concrete := adapter.(*KVStoreStorageAdapter)

	storeErr := errors.New("exists failed")
	mockStore.SetError("Exists", storeErr)

	stored, err := concrete.PutIfAbsent(ctx, "k", []byte("v"))
	assert.ErrorIs(t, err, storeErr)
	assert.False(t, stored)
}

func TestKVStoreStorageAdapter_PutIfAbsent_PutError(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)
	concrete := adapter.(*KVStoreStorageAdapter)

	storeErr := errors.New("put failed")
	mockStore.SetError("Put", storeErr)

	stored, err := concrete.PutIfAbsent(ctx, "k", []byte("v"))
	assert.ErrorIs(t, err, storeErr)
	assert.False(t, stored)
}

func TestKVStoreStorageAdapter_Scan(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)
	concrete := adapter.(*KVStoreStorageAdapter)

	require.NoError(t, concrete.Put(ctx, "alpha", []byte("1")))
	require.NoError(t, concrete.Put(ctx, "beta", []byte("2")))

	seen := make(map[string]string)
	err = concrete.Scan(ctx, "", func(key string, value []byte) error {
		seen[key] = string(value)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"alpha": "1", "beta": "2"}, seen)
}

func TestKVStoreStorageAdapter_Scan_CallbackError(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)
	concrete := adapter.(*KVStoreStorageAdapter)

	require.NoError(t, concrete.Put(ctx, "only", []byte("x")))

	cbErr := errors.New("stop")
	err = concrete.Scan(ctx, "", func(_ string, _ []byte) error {
		return cbErr
	})
	assert.ErrorIs(t, err, cbErr)
}

func TestKVStoreStorageAdapter_Scan_StoreError(t *testing.T) {
	ctx := context.Background()
	mockStore := NewMockKVStore()
	adapter, err := NewKVStoreStorageAdapter(mockStore)
	require.NoError(t, err)
	concrete := adapter.(*KVStoreStorageAdapter)

	mockStore.SetScanError(errors.New("scan failed"))
	err = concrete.Scan(ctx, "", func(_ string, _ []byte) error { return nil })
	require.Error(t, err)
}
