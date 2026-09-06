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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Test fixtures
// =============================================================================

// Compile-time check that testLogger satisfies interfaces.Logger.
var _ interfaces.Logger = (*testLogger)(nil)

// testLogger is a no-op logger used in unit tests so that we exercise the
// Manager's audit-emitting paths without polluting test output.
type testLogger struct {
	infos    []string
	warns    []string
	errs     []string
	debugs   []string
	infoFmt  []string
	warnFmt  []string
	errorFmt []string
	debugFmt []string
}

func (l *testLogger) Debug(msg string, _ ...any)             { l.debugs = append(l.debugs, msg) }
func (l *testLogger) Debugf(format string, _ ...interface{}) { l.debugFmt = append(l.debugFmt, format) }
func (l *testLogger) Info(msg string, _ ...any)              { l.infos = append(l.infos, msg) }
func (l *testLogger) Infof(format string, _ ...interface{})  { l.infoFmt = append(l.infoFmt, format) }
func (l *testLogger) Warn(msg string, _ ...any)              { l.warns = append(l.warns, msg) }
func (l *testLogger) Warnf(format string, _ ...interface{})  { l.warnFmt = append(l.warnFmt, format) }
func (l *testLogger) Error(msg string, _ ...any)             { l.errs = append(l.errs, msg) }
func (l *testLogger) Errorf(format string, _ ...interface{}) { l.errorFmt = append(l.errorFmt, format) }
func (l *testLogger) Fatal(_ string)                         {}
func (l *testLogger) Fatalf(_ string, _ ...interface{})      {}
func (l *testLogger) WithField(_ string, _ interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithFields(_ map[string]interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithError(_ error) interfaces.Logger {
	return l
}

// Compile-time check that captureDistributor satisfies the qrdb share-free
// distribution contract.
var _ qrdbsdk.CeremonyDistributor = (*captureDistributor)(nil)

// captureDistributor implements the qrdb CeremonyDistributor contract for
// unit tests. It splits the secret with the real Shamir splitter, records
// the share strings so tests can drive Unseal/GenerateRootToken, and
// returns the share count — mirroring what the production ceremony
// subsystem does (seal one share per enrolled Security Officer) without
// requiring officer enrollment.
type captureDistributor struct {
	mu       sync.Mutex
	splitter qrdbsdk.ShareSplitter
	shares   [][]byte
}

// newCaptureDistributor returns a captureDistributor backed by the real
// Shamir share splitter.
func newCaptureDistributor() *captureDistributor {
	return &captureDistributor{splitter: qrdbsdk.NewShamirShareSplitter()}
}

// SealAndDistribute splits secret into total shares with the given
// threshold, records each share's value (the base64 payload
// UnsealWithShare accepts), and returns the share count. The protected
// shares are destroyed once copied: each one is an mlock'd allocation, and
// the copy is test material the barrier under test will consume.
func (c *captureDistributor) SealAndDistribute(_ context.Context, secret []byte, threshold, total int) (int, error) {
	shares, err := c.splitter.SplitProtected(secret, threshold, total)
	if err != nil {
		return 0, err
	}
	values := make([][]byte, len(shares))
	for i, share := range shares {
		values[i] = append([]byte(nil), share.Value()...)
		share.Destroy()
	}
	c.mu.Lock()
	c.shares = values
	c.mu.Unlock()
	return len(values), nil
}

// RekeyAndDistribute destroys the prior round's shares and records fresh
// shares split from newSecret.
func (c *captureDistributor) RekeyAndDistribute(ctx context.Context, newSecret []byte, newThreshold, newTotal int) (int, error) {
	c.mu.Lock()
	c.shares = nil
	c.mu.Unlock()
	return c.SealAndDistribute(ctx, newSecret, newThreshold, newTotal)
}

// ShareBytes returns the captured shares from the last distribution round
// as byte slices, ready for Manager.Unseal / GenerateRootToken.
func (c *captureDistributor) ShareBytes() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.shares))
	for i, s := range c.shares {
		out[i] = append([]byte(nil), s...)
	}
	return out
}

// newSoftwareBarrier constructs a real qrdb StorageBarrier wrapping an
// in-memory backend with a pre-bound software passphrase. The passphrase
// "test-passphrase" is used to drive the deferred-software code path
// uniformly in tests.
func newSoftwareBarrier(t *testing.T) *qrdbsdk.StorageBarrier {
	t.Helper()
	backend := qrdbsdk.NewMemoryBackend()
	soft, err := qrdbsdk.NewSoftwareStrategy([]byte("test-passphrase"))
	require.NoError(t, err)
	b, err := qrdbsdk.NewStorageBarrier(slog.Default(), backend,
		qrdbsdk.BarrierConfig{RootKeyPath: "test/root-key"}, soft)
	require.NoError(t, err)
	return b
}

// newShamirBarrier constructs a barrier suitable for shamir
// initialize/unseal flows, with capture distributors wired for both the
// ceremony (init/rekey) and recovery share-distribution paths so the
// share-free contract can be exercised end to end. The barrier is left
// sealed; callers drive initialization themselves. It returns the barrier
// plus the ceremony and recovery distributors so tests can pull the
// sealed shares.
func newShamirBarrier(t *testing.T) (*qrdbsdk.StorageBarrier, *captureDistributor, *captureDistributor) {
	t.Helper()
	return newShamirBarrierWithBackend(t, qrdbsdk.NewMemoryBackend())
}

// =============================================================================
// Constructor tests
// =============================================================================

func TestNewManager_Success(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)
	require.NotNil(t, mgr)
	assert.Same(t, b, mgr.SystemBarrier())
}

func TestNewManager_NilBarrier(t *testing.T) {
	mgr, err := NewManager(nil, DefaultConfig(), &testLogger{})
	require.Error(t, err)
	assert.Nil(t, mgr)
	var be *BarrierNilError
	assert.True(t, errors.As(err, &be), "expected BarrierNilError, got %T", err)
}

func TestNewManager_NilLogger(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), nil)
	require.Error(t, err)
	assert.Nil(t, mgr)
	var le *LoggerNilError
	assert.True(t, errors.As(err, &le), "expected LoggerNilError, got %T", err)
}

func TestNewManager_NilConfigUsesDefault(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, nil, &testLogger{})
	require.NoError(t, err)
	require.NotNil(t, mgr)
	assert.Equal(t, DefaultConfig().Threshold, mgr.config.Threshold)
	assert.Equal(t, DefaultConfig().TotalShares, mgr.config.TotalShares)
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	assert.Equal(t, 2, c.Threshold)
	assert.Equal(t, 3, c.TotalShares)
	assert.True(t, c.RecoveryKeysEnabled)
	assert.Equal(t, 3, c.RecoveryThreshold)
	assert.Equal(t, 5, c.RecoveryTotalKeys)
}

// =============================================================================
// Status / IsSealed / IsInitialized
// =============================================================================

func TestManager_Status_SealedAndUninitialized(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	resp, err := mgr.Status(context.Background())
	require.NoError(t, err)
	status, ok := resp.(*service.SealStatusResponse)
	require.True(t, ok)
	assert.True(t, status.Sealed)
	assert.False(t, status.Initialized)
}

func TestManager_IsSealed_Initial(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)
	assert.True(t, mgr.IsSealed())
}

func TestManager_IsInitialized_Initial(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)
	assert.False(t, mgr.IsInitialized())
}

// =============================================================================
// Shamir Initialize / Unseal / Seal / Rekey
// =============================================================================

func TestManager_Initialize_Shamir_Success(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	resp, err := mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)
	// Share-free contract: SealInitResponse has no share fields by type.
	initResp, ok := resp.(*service.SealInitResponse)
	require.True(t, ok)
	assert.Len(t, dist.ShareBytes(), 3, "shares must be sealed to the ceremony distributor")
	assert.Equal(t, uint64(1), initResp.Version)

	// After init the barrier is left sealed.
	assert.True(t, mgr.IsSealed())
}

func TestManager_Initialize_DefaultsFromConfig(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, &Config{Threshold: 2, TotalShares: 3}, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	resp, err := mgr.Initialize(ctx, 0, 0)
	require.NoError(t, err)
	// Share-free contract: SealInitResponse has no share fields by type.
	_, ok := resp.(*service.SealInitResponse)
	require.True(t, ok)
	assert.Len(t, dist.ShareBytes(), 3, "config defaults must drive the distributed share count")
}

func TestManager_Unseal_PartialAndComplete(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)
	shares := dist.ShareBytes()
	require.Len(t, shares, 3)

	// First share: progress, still sealed.
	progress, err := mgr.Unseal(ctx, shares[0])
	require.NoError(t, err)
	p1 := progress.(*service.UnsealResponse)
	assert.True(t, p1.Sealed)
	assert.Equal(t, 1, p1.SharesProvided)
	assert.Equal(t, 2, p1.Threshold)

	// Second share: barrier unseals.
	progress2, err := mgr.Unseal(ctx, shares[1])
	require.NoError(t, err)
	p2 := progress2.(*service.UnsealResponse)
	assert.False(t, p2.Sealed)
	assert.Equal(t, 2, p2.SharesProvided)
}

func TestManager_Seal_Idempotent(t *testing.T) {
	b, _, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, mgr.Seal(ctx))
	require.NoError(t, mgr.Seal(ctx))
}

func TestManager_Rekey_AfterUnseal(t *testing.T) {
	b, dist, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)
	shares := dist.ShareBytes()
	require.Len(t, shares, 3)

	// Unseal so rekey can run.
	_, err = mgr.Unseal(ctx, shares[0])
	require.NoError(t, err)
	_, err = mgr.Unseal(ctx, shares[1])
	require.NoError(t, err)

	rekeyResp, err := mgr.Rekey(ctx, 5, 3)
	require.NoError(t, err)
	// Share-free contract: RekeyResponse has no share fields by type.
	r := rekeyResp.(*service.RekeyResponse)
	assert.Len(t, dist.ShareBytes(), 5, "new shares must be sealed to the ceremony distributor")
	assert.Equal(t, uint64(2), r.Version)
}

func TestManager_Rekey_WhileSealed_ReturnsError(t *testing.T) {
	b, _, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)

	// Without unsealing first, rekey must fail.
	_, err = mgr.Rekey(ctx, 5, 3)
	assert.Error(t, err)
}

// =============================================================================
// Recovery keys
// =============================================================================

func TestManager_GenerateRecoveryKeys_RequiresUnseal(t *testing.T) {
	b, _, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	// Sealed barrier rejects recovery key generation.
	_, err = mgr.GenerateRecoveryKeys(context.Background(), 2, 3)
	assert.Error(t, err)
}

func TestManager_DeleteRecoveryKeys_NoOpOK(t *testing.T) {
	b, _, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	// DeleteRecoveryKeys on a fresh barrier with no recovery metadata
	// should not error; the underlying barrier treats absence as success.
	err = mgr.DeleteRecoveryKeys(context.Background())
	assert.NoError(t, err)
}

// =============================================================================
// Root token
// =============================================================================

func TestManager_GenerateRootToken_InvalidStrategy(t *testing.T) {
	b, _, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.GenerateRootToken(context.Background(), "bogus", nil)
	require.Error(t, err)
	var ie *InvalidStrategyError
	assert.True(t, errors.As(err, &ie))
}

func TestManager_GenerateRootToken_ValidStrategyForwarded(t *testing.T) {
	b, _, _ := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	// Without initialization the barrier rejects root token generation;
	// we only need to confirm the strategy validation accepts a
	// recognized value before calling the barrier.
	_, err = mgr.GenerateRootToken(context.Background(), StrategyShamirShares, [][]byte{})
	assert.Error(t, err)
	var ie *InvalidStrategyError
	assert.False(t, errors.As(err, &ie), "should not fail on strategy validation for shamir_shares")
}

// =============================================================================
// RotateRootKey is unsupported by this barrier surface
// =============================================================================

func TestManager_RotateRootKey_Unsupported(t *testing.T) {
	b := newSoftwareBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	err = mgr.RotateRootKey(context.Background())
	assert.ErrorIs(t, err, ErrRotateRootKeyUnsupported)
}

// =============================================================================
// Close
// =============================================================================

func TestManager_Close_NoError(t *testing.T) {
	b := newSoftwareBarrier(t)
	logger := &testLogger{}
	mgr, err := NewManager(b, DefaultConfig(), logger)
	require.NoError(t, err)

	require.NoError(t, mgr.Close())
	// Close emits an audit log line.
	assert.NotEmpty(t, logger.infos)
}

// =============================================================================
// Strategy chain construction
// =============================================================================

func TestBuildStrategyChain_Disabled(t *testing.T) {
	got, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyDisabled})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestBuildStrategyChain_Software(t *testing.T) {
	chain, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyPassphrase})
	require.NoError(t, err)
	require.Len(t, chain, 1)
	assert.Equal(t, qrdbsdk.StrategyPassphrase, chain[0].ID())
}

func TestBuildStrategyChain_Shamir(t *testing.T) {
	chain, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:            StrategyShamir,
		Backend:         qrdbsdk.NewMemoryBackend(),
		ShamirThreshold: 2,
		ShamirTotal:     3,
	})
	require.NoError(t, err)
	require.Len(t, chain, 2)
	assert.Equal(t, qrdbsdk.StrategyShamir, chain[0].ID())
	assert.Equal(t, qrdbsdk.StrategyPassphrase, chain[1].ID())
}

func TestBuildStrategyChain_Shamir_NilBackend(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:            StrategyShamir,
		ShamirThreshold: 2,
		ShamirTotal:     3,
	})
	require.Error(t, err)
	var sce *StrategyConstructionError
	assert.True(t, errors.As(err, &sce))
}

func TestBuildStrategyChain_Shamir_BadParams(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:            StrategyShamir,
		Backend:         qrdbsdk.NewMemoryBackend(),
		ShamirThreshold: 1,
		ShamirTotal:     0,
	})
	require.Error(t, err)
	var sce *StrategyConstructionError
	assert.True(t, errors.As(err, &sce))
}

func TestBuildStrategyChain_TPM2_RequiresInjectedStrategy(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyTPM2})
	require.Error(t, err)
	var sce *StrategyConstructionError
	assert.True(t, errors.As(err, &sce))
}

func TestBuildStrategyChain_TPM2_WithInjectedStrategy(t *testing.T) {
	// Use a software strategy as a stand-in for the injected TPM
	// strategy: the chain builder only needs a non-nil SealingStrategy
	// to thread it into the resulting slice.
	soft, err := qrdbsdk.NewSoftwareStrategy([]byte("test-tpm-substitute"))
	require.NoError(t, err)

	chain, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:         StrategyTPM2,
		TPM2Strategy: soft,
	})
	require.NoError(t, err)
	require.Len(t, chain, 2)
	assert.Same(t, soft, chain[0])
	assert.Equal(t, qrdbsdk.StrategyPassphrase, chain[1].ID())
}

func TestBuildStrategyChain_PKCS11_RequiresConfig(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyPKCS11})
	require.Error(t, err)
	var sce *StrategyConstructionError
	assert.True(t, errors.As(err, &sce))
}

func TestBuildStrategyChain_PKCS11_RequiresInjectedStrategy(t *testing.T) {
	// PKCS#11 strategy construction lives exclusively in go-xkms. The
	// builder requires the caller to inject a pre-constructed
	// SealingStrategy via PKCS11Strategy. With nil PKCS11Strategy the
	// builder must return StrategyConstructionError; with a non-nil
	// strategy that reports Available()=false it must return
	// StrategyUnsupportedError.
	_, err := BuildStrategyChain(&StrategyChainConfig{
		Kind: StrategyPKCS11,
	})
	if err == nil {
		t.Fatal("PKCS11 with nil strategy: expected error, got nil")
	}
	var sue *StrategyUnsupportedError
	var sce *StrategyConstructionError
	assert.True(t, errors.As(err, &sue) || errors.As(err, &sce),
		"expected StrategyUnsupportedError or StrategyConstructionError, got %T", err)
}

func TestBuildStrategyChain_AWSKMS_Unsupported(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyAWSKMS})
	require.Error(t, err)
	var sue *StrategyUnsupportedError
	assert.True(t, errors.As(err, &sue))
}

func TestBuildStrategyChain_GCPKMS_Unsupported(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyGCPKMS})
	require.Error(t, err)
	var sue *StrategyUnsupportedError
	assert.True(t, errors.As(err, &sue))
}

func TestBuildStrategyChain_AzureKV_Unsupported(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyAzureKV})
	require.Error(t, err)
	var sue *StrategyUnsupportedError
	assert.True(t, errors.As(err, &sue))
}

func TestBuildStrategyChain_Vault_Unsupported(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyVault})
	require.Error(t, err)
	var sue *StrategyUnsupportedError
	assert.True(t, errors.As(err, &sue))
}

func TestBuildStrategyChain_UnknownKind(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: "totally-bogus"})
	require.Error(t, err)
	var ue *UnknownStrategyError
	assert.True(t, errors.As(err, &ue))
}

func TestBuildStrategyChain_NilConfig(t *testing.T) {
	_, err := BuildStrategyChain(nil)
	require.Error(t, err)
	var ue *UnknownStrategyError
	assert.True(t, errors.As(err, &ue))
}

func TestIsValidStrategy(t *testing.T) {
	for kind := range validStrategies {
		assert.True(t, IsValidStrategy(kind), "expected %s to be valid", kind)
	}
	assert.False(t, IsValidStrategy("nonsense"))
}

// =============================================================================
// Error type messages
// =============================================================================

func TestErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{(&BarrierNilError{}), "barrier.Manager: storage barrier must not be nil"},
		{(&LoggerNilError{}), "barrier.Manager: logger must not be nil"},
		{(&InvalidStrategyError{Strategy: "x"}), "barrier.Manager: invalid strategy: x"},
		{(&UnknownStrategyError{Kind: "x"}), "barrier: unknown sealing strategy: x"},
		{(&StrategyUnsupportedError{Kind: "x"}), "barrier: sealing strategy not available in this build: x"},
		{(&StrategyConstructionError{Kind: "x", Err: errBackendRequired}), "barrier: failed to construct x strategy: barrier: storage backend is required"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.err.Error())
	}
}

func TestStrategyConstructionError_Unwrap(t *testing.T) {
	inner := errBackendRequired
	wrapped := &StrategyConstructionError{Kind: "shamir", Err: inner}
	assert.True(t, errors.Is(wrapped, inner))
}
