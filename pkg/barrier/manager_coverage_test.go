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

package barrier

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Test doubles
// =============================================================================

// deleteFaultBackend wraps a real StorageBackend and forces Delete to return
// an injected (non-ErrSealed) error so the Manager's DeleteRecoveryKeys
// non-sealed error branch can be exercised. Every other method delegates to
// the wrapped backend so the real barrier crypto/state machine is preserved.
type deleteFaultBackend struct {
	inner     qrdbsdk.StorageBackend
	deleteErr error
}

// errDeleteFault is the injected I/O failure used by deleteFaultBackend.
var errDeleteFault = errors.New("barrier_test: injected delete failure")

func (b *deleteFaultBackend) Get(ctx context.Context, key string) ([]byte, error) {
	return b.inner.Get(ctx, key)
}

func (b *deleteFaultBackend) Put(ctx context.Context, key string, data []byte) error {
	return b.inner.Put(ctx, key, data)
}

func (b *deleteFaultBackend) PutIfAbsent(ctx context.Context, key string, data []byte) (bool, error) {
	return b.inner.PutIfAbsent(ctx, key, data)
}

func (b *deleteFaultBackend) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return b.inner.CompareAndDelete(ctx, key, expected)
}

func (b *deleteFaultBackend) Delete(ctx context.Context, key string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	return b.inner.Delete(ctx, key)
}

func (b *deleteFaultBackend) List(ctx context.Context, prefix string) ([]string, error) {
	return b.inner.List(ctx, prefix)
}

func (b *deleteFaultBackend) Exists(ctx context.Context, key string) (bool, error) {
	return b.inner.Exists(ctx, key)
}

func (b *deleteFaultBackend) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return b.inner.Scan(ctx, prefix, fn)
}

func (b *deleteFaultBackend) Close() error { return b.inner.Close() }

// fakeStrategy is a minimal injectable SealingStrategy used to drive the
// PKCS#11 branch of BuildStrategyChain. The chain builder only inspects
// ID()/Available() and threads the strategy into the returned slice; it never
// invokes the seal/unseal crypto methods, so a no-op implementation faithfully
// represents an externally-injected (go-xkms) strategy for this code path.
type fakeStrategy struct {
	id        qrdbsdk.StrategyID
	available bool
}

func (s *fakeStrategy) ID() qrdbsdk.StrategyID { return s.id }
func (s *fakeStrategy) Available() bool        { return s.available }
func (s *fakeStrategy) HardwareBacked() bool   { return true }

func (s *fakeStrategy) SealRootKey(_ context.Context, _ []byte, _ qrdbsdk.Credentials) (*qrdbsdk.SealedRootKey, error) {
	return nil, errors.New("fakeStrategy: SealRootKey not implemented")
}

func (s *fakeStrategy) UnsealRootKey(_ context.Context, _ *qrdbsdk.SealedRootKey, _ qrdbsdk.Credentials) ([]byte, error) {
	return nil, errors.New("fakeStrategy: UnsealRootKey not implemented")
}

func (s *fakeStrategy) BarrierEncryptor(_ context.Context, _ []byte) (qrdbsdk.SymmetricEncrypter, error) {
	return nil, nil
}

func (s *fakeStrategy) Close() error { return nil }

// newShamirBarrierWithBackend builds a shamir barrier wired to the supplied
// backend so tests can inject faults. It mirrors newShamirBarrier but lets the
// caller own the backend instance. Capture distributors are wired for both
// the ceremony (init/rekey) and recovery share-distribution paths and
// returned alongside the barrier so tests can pull the sealed shares.
func newShamirBarrierWithBackend(t *testing.T, backend qrdbsdk.StorageBackend) (*qrdbsdk.StorageBarrier, *captureDistributor, *captureDistributor) {
	t.Helper()
	soft, err := qrdbsdk.NewSoftwareStrategy([]byte("test-passphrase"))
	require.NoError(t, err)
	shamirStrat, err := qrdbsdk.NewShamirStrategy(2, 3)
	require.NoError(t, err)
	cfg := qrdbsdk.BarrierConfig{
		RootKeyPath:     "test/root-key",
		Shamir:          &qrdbsdk.ShamirConfig{Threshold: 2, TotalShares: 3},
		PreferenceOrder: []qrdbsdk.StrategyID{qrdbsdk.StrategyShamir},
	}
	b, err := qrdbsdk.NewStorageBarrier(slog.Default(), backend, cfg, shamirStrat, soft)
	require.NoError(t, err)
	dist := newCaptureDistributor()
	b.SetCeremonyDistributor(dist)
	recovery := newCaptureDistributor()
	b.SetRecoveryDistributor(recovery)
	return b, dist, recovery
}

// unsealShamir fully unseals a freshly initialized shamir barrier by
// submitting threshold shares. It returns the init shares for reuse.
func unsealShamir(t *testing.T, mgr *Manager, shares [][]byte, threshold int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < threshold; i++ {
		_, err := mgr.Unseal(ctx, shares[i])
		require.NoError(t, err)
	}
	require.False(t, mgr.IsSealed(), "barrier should be unsealed after threshold shares")
}

// initAndCollectShares initializes a shamir barrier and returns the shares
// sealed to the ceremony distributor. Per the share-free contract the init
// response itself must carry no share material; this helper asserts that
// invariant on every initialization.
func initAndCollectShares(t *testing.T, mgr *Manager, dist *captureDistributor, total, threshold int) [][]byte {
	t.Helper()
	resp, err := mgr.Initialize(context.Background(), total, threshold)
	require.NoError(t, err)
	// Share-free contract: SealInitResponse has no share fields by type, so
	// the init response cannot carry share material.
	require.IsType(t, (*service.SealInitResponse)(nil), resp)
	shares := dist.ShareBytes()
	require.Len(t, shares, total, "shares must be sealed to the ceremony distributor")
	return shares
}

// =============================================================================
// Initialize error path
// =============================================================================

func TestManager_Initialize_InitializeShamirError(t *testing.T) {
	// A software-only barrier has no Shamir configuration, so InitializeShamir
	// must surface an error which Manager.Initialize propagates verbatim.
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.Initialize(context.Background(), 3, 2)
	require.Error(t, err)
}

// =============================================================================
// Unseal error path
// =============================================================================

func TestManager_Unseal_AlreadyUnsealedError(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)
	unsealShamir(t, mgr, shares, 2)

	// Submitting another share once the barrier is already unsealed must
	// surface the underlying barrier error through Manager.Unseal.
	_, err = mgr.Unseal(context.Background(), shares[2])
	require.Error(t, err)
}

// =============================================================================
// Rekey default-parameter branches
// =============================================================================

func TestManager_Rekey_DefaultsFromConfig(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, &Config{Threshold: 2, TotalShares: 3}, &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)
	unsealShamir(t, mgr, shares, 2)

	// Pass 0/0 so Rekey falls back to the manager's configured values.
	resp, err := mgr.Rekey(context.Background(), 0, 0)
	require.NoError(t, err)
	// Share-free contract: RekeyResponse has no share fields by type.
	r := resp.(*service.RekeyResponse)
	assert.Len(t, dist.ShareBytes(), 3, "config defaults must drive the distributed share count")
	assert.Equal(t, uint64(2), r.Version)
	assert.Equal(t, 2, mgr.config.Threshold)
	assert.Equal(t, 3, mgr.config.TotalShares)
}

// =============================================================================
// GenerateRecoveryKeys success path
// =============================================================================

func TestManager_GenerateRecoveryKeys_Success(t *testing.T) {
	b, dist, recovery := newShamirBarrier(t)
	logger := &testLogger{}
	mgr, err := NewManager(b, DefaultConfig(), logger)
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)
	unsealShamir(t, mgr, shares, 2)

	keys, err := mgr.GenerateRecoveryKeys(context.Background(), 2, 4)
	require.NoError(t, err)
	assert.Empty(t, keys, "share-free contract: recovery key material must never be returned")
	assert.Len(t, recovery.ShareBytes(), 4, "recovery shares must be sealed to the recovery distributor")
	assert.NotEmpty(t, logger.infoFmt, "expected an audit log line on success")
}

// =============================================================================
// DeleteRecoveryKeys branches
// =============================================================================

func TestManager_DeleteRecoveryKeys_Success(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)
	unsealShamir(t, mgr, shares, 2)

	// Generate recovery metadata, then delete it on an unsealed barrier so the
	// real Delete call succeeds (not the sealed no-op shortcut).
	_, err = mgr.GenerateRecoveryKeys(context.Background(), 2, 4)
	require.NoError(t, err)

	err = mgr.DeleteRecoveryKeys(context.Background())
	require.NoError(t, err)
}

func TestManager_DeleteRecoveryKeys_BackendError(t *testing.T) {
	backend := &deleteFaultBackend{inner: qrdbsdk.NewMemoryBackend(), deleteErr: errDeleteFault}
	b, dist, _ := newShamirBarrierWithBackend(t, backend)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)
	unsealShamir(t, mgr, shares, 2)

	// Unsealed + injected Delete failure: the error must propagate (it is not
	// ErrSealed, so the idempotent no-op shortcut does not apply).
	err = mgr.DeleteRecoveryKeys(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, errDeleteFault)
}

// =============================================================================
// GenerateRootToken success path
// =============================================================================

func TestManager_GenerateRootToken_Success(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)

	// GenerateRootToken proves knowledge of the master key from threshold
	// shares; it works against the sealed root key envelope.
	token, err := mgr.GenerateRootToken(context.Background(), StrategyShamirShares, shares[:2])
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestManager_GenerateRootToken_RecoveryKeysStrategyAccepted(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)

	token, err := mgr.GenerateRootToken(context.Background(), StrategyRecoveryKeys, shares[:2])
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestManager_GenerateRootToken_BarrierError(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	shares := initAndCollectShares(t, mgr, dist, 3, 2)

	// Fewer than threshold shares: strategy validation passes but the barrier
	// rejects the quorum, exercising the error-return branch.
	_, err = mgr.GenerateRootToken(context.Background(), StrategyShamirShares, shares[:1])
	require.Error(t, err)
	var ie *InvalidStrategyError
	assert.False(t, errors.As(err, &ie), "error must come from the barrier, not strategy validation")
}

// =============================================================================
// BuildStrategyChain PKCS#11 Available()-true / -false branches
// =============================================================================

func TestBuildStrategyChain_PKCS11_AvailableStrategy(t *testing.T) {
	pkcs := &fakeStrategy{id: xkmssdk.StrategyPKCS11, available: true}
	chain, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:           StrategyPKCS11,
		PKCS11Strategy: pkcs,
	})
	require.NoError(t, err)
	require.Len(t, chain, 2)
	assert.Same(t, pkcs, chain[0])
	assert.Equal(t, qrdbsdk.StrategyPassphrase, chain[1].ID())
}

func TestBuildStrategyChain_PKCS11_UnavailableStrategy(t *testing.T) {
	pkcs := &fakeStrategy{id: xkmssdk.StrategyPKCS11, available: false}
	_, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:           StrategyPKCS11,
		PKCS11Strategy: pkcs,
	})
	require.Error(t, err)
	var sue *StrategyUnsupportedError
	assert.True(t, errors.As(err, &sue), "expected StrategyUnsupportedError, got %T", err)
}
