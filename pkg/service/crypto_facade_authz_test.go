package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// cryptoFacadeFixture is the crypto service as the production constructor
// newQuicBaaS wires it: the facade the Crypto RPCs of every transport
// delegate to.
type cryptoFacadeFixture struct {
	crypto *cryptoService
	kms    *kmsServiceImpl
	kv     *mockKVStore
	calls  int
}

func newCryptoFacadeFixture(t *testing.T) *cryptoFacadeFixture {
	t.Helper()
	f := &cryptoFacadeFixture{kv: newMockKVStore()}

	kmsManager := newMockKMSManager()
	kmsManager.keys["authz-key"] = &mockKeyData{keyID: "authz-key", status: "enabled"}
	kmsManager.keys["open-key"] = &mockKeyData{keyID: "open-key", status: "enabled"}

	cryptoMgr := newMockCryptoManager()
	cryptoMgr.rewrapFn = func(_ context.Context, _ string, ciphertext, _ []byte) ([]byte, error) {
		f.calls++
		return ciphertext, nil
	}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.kmsManager = kmsManager
		b.cryptoManager = cryptoMgr
		b.kvStoreProvider = func(uint64) interface{} { return f.kv }
	})

	crypto, ok := svc.Crypto().(*cryptoService)
	require.True(t, ok, "Crypto() must be the *cryptoService facade")
	f.crypto = crypto
	kmsImpl, ok := svc.(*quicBaaS).kmsService.(*kmsServiceImpl)
	require.True(t, ok, "kmsService must be *kmsServiceImpl")
	f.kms = kmsImpl
	return f
}

func (f *cryptoFacadeFixture) rewrap(ctx context.Context, keyID string) error {
	_, err := f.crypto.Rewrap(ctx, &RewrapRequest{KeyID: keyID, Ciphertext: []byte("ct")})
	return err
}

// TestNewQuicBaaS_CryptoFacadeAuthzEnabled pins the production wiring: the
// facade built by newQuicBaaS enforces persisted key policies on both the
// crypto and the KMS service.
func TestNewQuicBaaS_CryptoFacadeAuthzEnabled(t *testing.T) {
	f := newCryptoFacadeFixture(t)

	assert.True(t, f.crypto.authzEnabled)
	require.NotNil(t, f.crypto.kmsSvc)
	assert.Same(t, f.kms, f.crypto.kmsSvc)
	assert.True(t, f.kms.authzEnabled)
}

// TestNewQuicBaaS_CryptoFacadeEnforcesKeyPolicy is the regression for the
// Crypto RPCs bypassing key policies: a principal the persisted policy does
// not allow is refused before the crypto manager is reached.
func TestNewQuicBaaS_CryptoFacadeEnforcesKeyPolicy(t *testing.T) {
	f := newCryptoFacadeFixture(t)
	data, err := json.Marshal(&KeyPolicy{
		KeyID:             "authz-key",
		AllowedPrincipals: []string{"user-1"},
	})
	require.NoError(t, err)
	require.NoError(t, f.kv.Put(systemTenantCtx(), kmsKeyPolicyKey("authz-key"), data))

	t.Run("denied", func(t *testing.T) {
		err := f.rewrap(subjectCtx(authenticatedSubject("user-2", "bob")), "authz-key")
		var qe *errors.QBError
		require.ErrorAs(t, err, &qe)
		assert.Equal(t, errors.ErrForbidden, qe.Code)
		assert.Zero(t, f.calls)
	})

	t.Run("allowed", func(t *testing.T) {
		require.NoError(t, f.rewrap(subjectCtx(authenticatedSubject("user-1", "alice")), "authz-key"))
		assert.Equal(t, 1, f.calls)
	})
}

// TestNewQuicBaaS_CryptoFacadeNoPolicyAllows covers a key without a stored
// policy: it stays default-allow for an authenticated principal, as the
// per-key authorization contract says.
func TestNewQuicBaaS_CryptoFacadeNoPolicyAllows(t *testing.T) {
	f := newCryptoFacadeFixture(t)

	require.NoError(t, f.rewrap(subjectCtx(authenticatedSubject("user-2", "bob")), "open-key"))
	assert.Equal(t, 1, f.calls)
}
