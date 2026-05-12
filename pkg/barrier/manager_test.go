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

	qrdbseal "github.com/jeremyhahn/go-qrdb/pkg/seal"

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

// newSoftwareBarrier constructs a real qrdb StorageBarrier wrapping an
// in-memory backend with a pre-bound software passphrase. The passphrase
// "test-passphrase" is used to drive the deferred-software code path
// uniformly in tests.
func newSoftwareBarrier(t *testing.T) *qrdbseal.StorageBarrier {
	t.Helper()
	backend := qrdbseal.NewMemoryBackend()
	soft, err := qrdbseal.NewSoftwareStrategy([]byte("test-passphrase"))
	require.NoError(t, err)
	b, err := qrdbseal.NewStorageBarrier(slog.Default(), backend,
		qrdbseal.BarrierConfig{RootKeyPath: "test/root-key"}, soft)
	require.NoError(t, err)
	return b
}

// newShamirBarrier constructs a barrier suitable for shamir
// initialize/unseal flows. The barrier is left sealed; callers drive
// initialization themselves.
func newShamirBarrier(t *testing.T) *qrdbseal.StorageBarrier {
	t.Helper()
	backend := qrdbseal.NewMemoryBackend()
	soft, err := qrdbseal.NewSoftwareStrategy([]byte("test-passphrase"))
	require.NoError(t, err)
	shamirStrat, err := qrdbseal.NewShamirStrategy(backend, 2, 3)
	require.NoError(t, err)
	cfg := qrdbseal.BarrierConfig{
		RootKeyPath: "test/root-key",
		Shamir:      &qrdbseal.ShamirConfig{Threshold: 2, TotalShares: 3},
	}
	b, err := qrdbseal.NewStorageBarrier(slog.Default(), backend, cfg, shamirStrat, soft)
	require.NoError(t, err)
	return b
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
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	resp, err := mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)
	initResp, ok := resp.(*service.SealInitResponse)
	require.True(t, ok)
	assert.Len(t, initResp.Shares, 3)
	assert.Equal(t, uint64(1), initResp.Version)

	// After init the barrier is left sealed.
	assert.True(t, mgr.IsSealed())
}

func TestManager_Initialize_DefaultsFromConfig(t *testing.T) {
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, &Config{Threshold: 2, TotalShares: 3}, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	resp, err := mgr.Initialize(ctx, 0, 0)
	require.NoError(t, err)
	initResp, ok := resp.(*service.SealInitResponse)
	require.True(t, ok)
	assert.Len(t, initResp.Shares, 3)
}

func TestManager_Unseal_PartialAndComplete(t *testing.T) {
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	initResp, err := mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)
	shares := initResp.(*service.SealInitResponse).Shares
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
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, mgr.Seal(ctx))
	require.NoError(t, mgr.Seal(ctx))
}

func TestManager_Rekey_AfterUnseal(t *testing.T) {
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	initResp, err := mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)
	shares := initResp.(*service.SealInitResponse).Shares

	// Unseal so rekey can run.
	_, err = mgr.Unseal(ctx, shares[0])
	require.NoError(t, err)
	_, err = mgr.Unseal(ctx, shares[1])
	require.NoError(t, err)

	rekeyResp, err := mgr.Rekey(ctx, 5, 3)
	require.NoError(t, err)
	r := rekeyResp.(*service.RekeyResponse)
	assert.Len(t, r.Shares, 5)
	assert.Equal(t, uint64(2), r.Version)
}

func TestManager_Rekey_WhileSealed_ReturnsError(t *testing.T) {
	b := newShamirBarrier(t)
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
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	// Sealed barrier rejects recovery key generation.
	_, err = mgr.GenerateRecoveryKeys(context.Background(), 2, 3)
	assert.Error(t, err)
}

func TestManager_DeleteRecoveryKeys_NoOpOK(t *testing.T) {
	b := newShamirBarrier(t)
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
	b := newShamirBarrier(t)
	mgr, err := NewManager(b, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.GenerateRootToken(context.Background(), "bogus", nil)
	require.Error(t, err)
	var ie *InvalidStrategyError
	assert.True(t, errors.As(err, &ie))
}

func TestManager_GenerateRootToken_ValidStrategyForwarded(t *testing.T) {
	b := newShamirBarrier(t)
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
	chain, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategySoftware})
	require.NoError(t, err)
	require.Len(t, chain, 1)
	assert.Equal(t, qrdbseal.StrategySoftware, chain[0].ID())
}

func TestBuildStrategyChain_Shamir(t *testing.T) {
	chain, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:            StrategyShamir,
		Backend:         qrdbseal.NewMemoryBackend(),
		ShamirThreshold: 2,
		ShamirTotal:     3,
	})
	require.NoError(t, err)
	require.Len(t, chain, 2)
	assert.Equal(t, qrdbseal.StrategyShamir, chain[0].ID())
	assert.Equal(t, qrdbseal.StrategySoftware, chain[1].ID())
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
		Backend:         qrdbseal.NewMemoryBackend(),
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
	soft, err := qrdbseal.NewSoftwareStrategy([]byte("test-tpm-substitute"))
	require.NoError(t, err)

	chain, err := BuildStrategyChain(&StrategyChainConfig{
		Kind:         StrategyTPM2,
		TPM2Strategy: soft,
	})
	require.NoError(t, err)
	require.Len(t, chain, 2)
	assert.Same(t, soft, chain[0])
	assert.Equal(t, qrdbseal.StrategySoftware, chain[1].ID())
}

func TestBuildStrategyChain_PKCS11_RequiresConfig(t *testing.T) {
	_, err := BuildStrategyChain(&StrategyChainConfig{Kind: StrategyPKCS11})
	require.Error(t, err)
	var sce *StrategyConstructionError
	assert.True(t, errors.As(err, &sce))
}

func TestBuildStrategyChain_PKCS11_StubBuildReportsUnsupported(t *testing.T) {
	// Without the pkcs11 build tag the stub strategy reports
	// Available()=false; the builder surfaces this as
	// StrategyUnsupportedError. With the tag enabled and a real
	// module present, this test exercises the failure-mode path.
	chain, err := BuildStrategyChain(&StrategyChainConfig{
		Kind: StrategyPKCS11,
		PKCS11Config: &qrdbseal.PKCS11Config{
			ModuleLibPath: "/nonexistent/lib.so",
			SlotID:        0,
			UserPinEnvVar: "QUICBAAS_TEST_PIN",
			KEKLabel:      "kek",
		},
	})
	if err == nil {
		// On a build with the pkcs11 tag and a working softhsm, the
		// strategy may construct successfully; in that case the chain
		// must contain pkcs11 followed by software.
		require.Len(t, chain, 2)
		return
	}
	// Otherwise, the builder must surface a typed error.
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
