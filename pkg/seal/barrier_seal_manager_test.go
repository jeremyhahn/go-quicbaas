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

package seal

import (
	"context"
	"errors"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Compile-time check that testLogger satisfies interfaces.Logger.
var _ interfaces.Logger = (*testLogger)(nil)

// testLogger implements interfaces.Logger as a no-op for unit tests.
type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...any)                                           {}
func (l *testLogger) Debugf(format string, args ...interface{})                  {}
func (l *testLogger) Info(msg string, args ...any)                                            {}
func (l *testLogger) Infof(format string, args ...interface{})                   {}
func (l *testLogger) Warn(msg string, args ...any)                                            {}
func (l *testLogger) Warnf(format string, args ...interface{})                   {}
func (l *testLogger) Error(msg string, args ...any)                                           {}
func (l *testLogger) Errorf(format string, args ...interface{})                  {}
func (l *testLogger) Fatal(msg string)                                           {}
func (l *testLogger) Fatalf(format string, args ...interface{})                  {}
func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *testLogger) WithError(err error) interfaces.Logger                      { return l }

// Compile-time check that mockBarrierClient satisfies BarrierClient.
var _ qrdbtransport.BarrierClient = (*mockBarrierClient)(nil)

// mockBarrierClient implements all methods of transport.BarrierClient with
// configurable behavior for unit testing.
type mockBarrierClient struct {
	mu sync.Mutex

	sealed      bool
	initialized bool
	initShares  []string
	threshold   int
	totalShares int
	unsealCount int

	// Error injection
	statusErr    error
	initErr      error
	unsealErr    error
	sealErr      error
	rekeyErr     error
	recoveryErr  error
	deleteRecErr error
	rootTokenErr error
}

func newMockBarrierClient() *mockBarrierClient {
	return &mockBarrierClient{
		sealed:      true,
		initialized: false,
		threshold:   2,
		totalShares: 3,
	}
}

func (m *mockBarrierClient) BarrierInitialize(_ context.Context, _ *qrdbtransport.BarrierInitializeRequest) error {
	return nil
}

func (m *mockBarrierClient) BarrierUnseal(_ context.Context, _ *qrdbtransport.BarrierUnsealRequest) error {
	return nil
}

func (m *mockBarrierClient) BarrierSeal(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealErr != nil {
		return m.sealErr
	}
	m.sealed = true
	return nil
}

func (m *mockBarrierClient) BarrierStatus(_ context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	initializedAt := ""
	if m.initialized {
		initializedAt = "2025-01-01T00:00:00Z"
	}
	return &qrdbtransport.BarrierStatusResponse{
		Sealed:        m.sealed,
		InitializedAt: initializedAt,
	}, nil
}

func (m *mockBarrierClient) BarrierInitializeShamir(_ context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return nil, m.initErr
	}
	shares := make([]string, req.TotalShares)
	for i := range shares {
		shares[i] = "share-" + string(rune('a'+i))
	}
	m.initShares = shares
	m.initialized = true
	m.sealed = false // InitializeShamir leaves unsealed
	m.threshold = req.Threshold
	m.totalShares = req.TotalShares
	return &qrdbtransport.BarrierInitializeShamirResponse{
		Shares:      shares,
		Threshold:   req.Threshold,
		TotalShares: req.TotalShares,
	}, nil
}

func (m *mockBarrierClient) BarrierUnsealWithShare(_ context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unsealErr != nil {
		return nil, m.unsealErr
	}
	m.unsealCount++
	complete := m.unsealCount >= m.threshold
	if complete {
		m.sealed = false
	}
	return &qrdbtransport.BarrierUnsealShareResponse{
		Required:  m.threshold,
		Submitted: m.unsealCount,
		Complete:  complete,
	}, nil
}

func (m *mockBarrierClient) BarrierUnsealWithShares(_ context.Context, _ *qrdbtransport.BarrierUnsealSharesRequest) error {
	return nil
}

func (m *mockBarrierClient) BarrierShamirListShares(_ context.Context) (*qrdbtransport.BarrierShamirSharesResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) BarrierShamirDeleteShare(_ context.Context, _ *qrdbtransport.BarrierShamirDeleteShareRequest) error {
	return nil
}

func (m *mockBarrierClient) BarrierShamirDeleteAllShares(_ context.Context) error {
	return nil
}

func (m *mockBarrierClient) BarrierShamirVerify(_ context.Context) error {
	return nil
}

func (m *mockBarrierClient) BarrierRekey(_ context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rekeyErr != nil {
		return nil, m.rekeyErr
	}
	shares := make([]string, req.Total)
	for i := range shares {
		shares[i] = "new-share-" + string(rune('a'+i))
	}
	return &qrdbtransport.BarrierRekeyResponse{
		Shares:      shares,
		Threshold:   req.Threshold,
		TotalShares: req.Total,
	}, nil
}

func (m *mockBarrierClient) BarrierGenerateRecoveryKeys(_ context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recoveryErr != nil {
		return nil, m.recoveryErr
	}
	keys := make([]string, req.Total)
	for i := range keys {
		keys[i] = "recovery-key-" + string(rune('a'+i))
	}
	return &qrdbtransport.BarrierRecoveryKeysResponse{
		Keys:      keys,
		Threshold: req.Threshold,
		Total:     req.Total,
	}, nil
}

func (m *mockBarrierClient) BarrierRecoverWithKeys(_ context.Context, _ *qrdbtransport.BarrierRecoverWithKeysRequest) error {
	return nil
}

func (m *mockBarrierClient) BarrierDeleteRecoveryKeys(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleteRecErr
}

func (m *mockBarrierClient) BarrierHasRecoveryKeys(_ context.Context) (*qrdbtransport.BarrierHasRecoveryKeysResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) BarrierGenerateRootToken(_ context.Context, _ *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rootTokenErr != nil {
		return nil, m.rootTokenErr
	}
	return &qrdbtransport.BarrierRootTokenResponse{
		Token:     "generated-root-token",
		CreatedAt: "2025-01-01T00:00:00Z",
	}, nil
}

// =============================================================================
// Constructor Tests
// =============================================================================

// TestNewBarrierSealManager_NilClient verifies that a nil client produces
// a BarrierNilError.
func TestNewBarrierSealManager_NilClient(t *testing.T) {
	mgr, err := NewBarrierSealManager(nil, DefaultConfig(), &testLogger{})
	require.Nil(t, mgr)
	require.Error(t, err)

	var barrierNilErr *BarrierNilError
	require.ErrorAs(t, err, &barrierNilErr)
}

// TestNewBarrierSealManager_NilConfig verifies that a nil config applies
// defaults and the constructor succeeds.
func TestNewBarrierSealManager_NilConfig(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, nil, &testLogger{})
	require.NoError(t, err)
	require.NotNil(t, mgr)

	require.Equal(t, SealModeAuto, mgr.config.Mode)
	require.Equal(t, 2, mgr.config.Threshold)
	require.Equal(t, 3, mgr.config.TotalShares)
}

// TestNewBarrierSealManager_Success verifies successful construction with
// valid parameters.
func TestNewBarrierSealManager_Success(t *testing.T) {
	client := newMockBarrierClient()
	cfg := &Config{
		Mode:        SealModeManual,
		Threshold:   2,
		TotalShares: 3,
	}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)
	require.NotNil(t, mgr)
}

// =============================================================================
// Status Tests
// =============================================================================

// TestBarrierSealManager_Status_DelegatesToSDK verifies Status delegates to
// the SDK client and maps the response.
func TestBarrierSealManager_Status_DelegatesToSDK(t *testing.T) {
	client := newMockBarrierClient()
	client.initialized = true
	cfg := &Config{Mode: SealModeManual, Threshold: 2, TotalShares: 3}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := mgr.Status(ctx)
	require.NoError(t, err)

	statusResp, ok := result.(*service.SealStatusResponse)
	require.True(t, ok)
	require.True(t, statusResp.Sealed)
	require.True(t, statusResp.Initialized)
	require.Equal(t, "manual", statusResp.Mode)
	require.Equal(t, 2, statusResp.Threshold)
	require.Equal(t, 3, statusResp.TotalShares)
	require.False(t, statusResp.Timestamp.IsZero())
}

// TestBarrierSealManager_Status_Error verifies Status returns errors from
// the SDK client.
func TestBarrierSealManager_Status_Error(t *testing.T) {
	client := newMockBarrierClient()
	client.statusErr = errors.New("connection refused")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.Status(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")
}

// =============================================================================
// IsSealed Tests
// =============================================================================

// TestBarrierSealManager_IsSealed_True verifies IsSealed returns true when
// the barrier is sealed.
func TestBarrierSealManager_IsSealed_True(t *testing.T) {
	client := newMockBarrierClient()
	client.sealed = true
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.True(t, mgr.IsSealed())
}

// TestBarrierSealManager_IsSealed_False verifies IsSealed returns false when
// the barrier is unsealed.
func TestBarrierSealManager_IsSealed_False(t *testing.T) {
	client := newMockBarrierClient()
	client.sealed = false
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.False(t, mgr.IsSealed())
}

// TestBarrierSealManager_IsSealed_ErrorReturnsTrueDefault verifies IsSealed
// assumes sealed on error.
func TestBarrierSealManager_IsSealed_ErrorReturnsTrueDefault(t *testing.T) {
	client := newMockBarrierClient()
	client.statusErr = errors.New("connection refused")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.True(t, mgr.IsSealed())
}

// =============================================================================
// IsInitialized Tests
// =============================================================================

// TestBarrierSealManager_IsInitialized_True verifies IsInitialized returns
// true when InitializedAt is set.
func TestBarrierSealManager_IsInitialized_True(t *testing.T) {
	client := newMockBarrierClient()
	client.initialized = true
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.True(t, mgr.IsInitialized())
}

// TestBarrierSealManager_IsInitialized_False verifies IsInitialized returns
// false when InitializedAt is empty.
func TestBarrierSealManager_IsInitialized_False(t *testing.T) {
	client := newMockBarrierClient()
	client.initialized = false
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.False(t, mgr.IsInitialized())
}

// TestBarrierSealManager_IsInitialized_ErrorReturnsFalse verifies
// IsInitialized returns false on error.
func TestBarrierSealManager_IsInitialized_ErrorReturnsFalse(t *testing.T) {
	client := newMockBarrierClient()
	client.statusErr = errors.New("connection refused")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.False(t, mgr.IsInitialized())
}

// =============================================================================
// Initialize Tests
// =============================================================================

// TestBarrierSealManager_Initialize_Success verifies Initialize creates shares,
// seals after init, and returns a SealInitResponse.
func TestBarrierSealManager_Initialize_Success(t *testing.T) {
	client := newMockBarrierClient()
	cfg := &Config{Mode: SealModeManual, Threshold: 2, TotalShares: 3}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := mgr.Initialize(ctx, 3, 2)
	require.NoError(t, err)

	initResp, ok := result.(*service.SealInitResponse)
	require.True(t, ok)
	require.Len(t, initResp.Shares, 3)
	require.Equal(t, uint64(1), initResp.Version)
	require.False(t, initResp.Timestamp.IsZero())

	// After initialization, the barrier should be sealed.
	require.True(t, client.sealed)
	require.True(t, client.initialized)
}

// TestBarrierSealManager_Initialize_DefaultParams verifies Initialize uses
// config defaults when shares/threshold are zero.
func TestBarrierSealManager_Initialize_DefaultParams(t *testing.T) {
	client := newMockBarrierClient()
	cfg := &Config{Mode: SealModeManual, Threshold: 3, TotalShares: 5}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := mgr.Initialize(ctx, 0, 0)
	require.NoError(t, err)

	initResp, ok := result.(*service.SealInitResponse)
	require.True(t, ok)
	require.Len(t, initResp.Shares, 5)
}

// TestBarrierSealManager_Initialize_SDKError verifies Initialize propagates
// SDK errors.
func TestBarrierSealManager_Initialize_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.initErr = errors.New("threshold must be >= 2")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.Initialize(context.Background(), 3, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "threshold must be >= 2")
}

// TestBarrierSealManager_Initialize_SealAfterInitError verifies Initialize
// returns error when post-init seal fails.
func TestBarrierSealManager_Initialize_SealAfterInitError(t *testing.T) {
	client := newMockBarrierClient()
	client.sealErr = errors.New("seal failed")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.Initialize(context.Background(), 3, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "seal failed")
}

// =============================================================================
// Unseal Tests
// =============================================================================

// TestBarrierSealManager_Unseal_Success verifies Unseal delegates to SDK and
// maps the response correctly for partial progress.
func TestBarrierSealManager_Unseal_Success(t *testing.T) {
	client := newMockBarrierClient()
	client.initialized = true
	cfg := &Config{Mode: SealModeManual, Threshold: 2, TotalShares: 3}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := mgr.Unseal(ctx, []byte("share-a"))
	require.NoError(t, err)

	unsealResp, ok := result.(*service.UnsealResponse)
	require.True(t, ok)
	require.True(t, unsealResp.Sealed, "should still be sealed after first share")
	require.Equal(t, 1, unsealResp.SharesProvided)
	require.Equal(t, 2, unsealResp.Threshold)
}

// TestBarrierSealManager_Unseal_Complete verifies Unseal correctly reports
// unsealed state when threshold is met.
func TestBarrierSealManager_Unseal_Complete(t *testing.T) {
	client := newMockBarrierClient()
	client.initialized = true
	cfg := &Config{Mode: SealModeManual, Threshold: 2, TotalShares: 3}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()

	// First share
	_, err = mgr.Unseal(ctx, []byte("share-a"))
	require.NoError(t, err)

	// Second share meets threshold
	result, err := mgr.Unseal(ctx, []byte("share-b"))
	require.NoError(t, err)

	unsealResp, ok := result.(*service.UnsealResponse)
	require.True(t, ok)
	require.False(t, unsealResp.Sealed, "should be unsealed after threshold met")
	require.Equal(t, 2, unsealResp.SharesProvided)
}

// TestBarrierSealManager_Unseal_SDKError verifies Unseal propagates SDK errors.
func TestBarrierSealManager_Unseal_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.unsealErr = errors.New("invalid share")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.Unseal(context.Background(), []byte("bad-share"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid share")
}

// =============================================================================
// Seal Tests
// =============================================================================

// TestBarrierSealManager_Seal_Success verifies Seal delegates to SDK.
func TestBarrierSealManager_Seal_Success(t *testing.T) {
	client := newMockBarrierClient()
	client.sealed = false
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	err = mgr.Seal(context.Background())
	require.NoError(t, err)
	require.True(t, client.sealed)
}

// TestBarrierSealManager_Seal_SDKError verifies Seal propagates SDK errors.
func TestBarrierSealManager_Seal_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.sealErr = errors.New("seal failed")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	err = mgr.Seal(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "seal failed")
}

// =============================================================================
// Rekey Tests
// =============================================================================

// TestBarrierSealManager_Rekey_Success verifies Rekey delegates to SDK, maps
// shares, and increments version.
func TestBarrierSealManager_Rekey_Success(t *testing.T) {
	client := newMockBarrierClient()
	cfg := &Config{Mode: SealModeManual, Threshold: 2, TotalShares: 3}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := mgr.Rekey(ctx, 5, 3)
	require.NoError(t, err)

	rekeyResp, ok := result.(*service.RekeyResponse)
	require.True(t, ok)
	require.Len(t, rekeyResp.Shares, 5)
	require.Equal(t, uint64(1), rekeyResp.Version)
	require.False(t, rekeyResp.Timestamp.IsZero())

	// Config should be updated.
	require.Equal(t, 3, mgr.config.Threshold)
	require.Equal(t, 5, mgr.config.TotalShares)
}

// TestBarrierSealManager_Rekey_DefaultParams verifies Rekey uses config
// defaults when shares/threshold are zero.
func TestBarrierSealManager_Rekey_DefaultParams(t *testing.T) {
	client := newMockBarrierClient()
	cfg := &Config{Mode: SealModeManual, Threshold: 3, TotalShares: 5}
	mgr, err := NewBarrierSealManager(client, cfg, &testLogger{})
	require.NoError(t, err)

	result, err := mgr.Rekey(context.Background(), 0, 0)
	require.NoError(t, err)

	rekeyResp, ok := result.(*service.RekeyResponse)
	require.True(t, ok)
	require.Len(t, rekeyResp.Shares, 5)
}

// TestBarrierSealManager_Rekey_SDKError verifies Rekey propagates SDK errors.
func TestBarrierSealManager_Rekey_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.rekeyErr = errors.New("rekey failed")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.Rekey(context.Background(), 5, 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rekey failed")
}

// =============================================================================
// GenerateRecoveryKeys Tests
// =============================================================================

// TestBarrierSealManager_GenerateRecoveryKeys_Success verifies recovery key
// generation delegates to SDK and maps response.
func TestBarrierSealManager_GenerateRecoveryKeys_Success(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	keys, err := mgr.GenerateRecoveryKeys(context.Background(), 2, 3)
	require.NoError(t, err)
	require.Len(t, keys, 3)
	for _, key := range keys {
		require.NotEmpty(t, key)
	}
}

// TestBarrierSealManager_GenerateRecoveryKeys_SDKError verifies error
// propagation from SDK.
func TestBarrierSealManager_GenerateRecoveryKeys_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.recoveryErr = errors.New("generation failed")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.GenerateRecoveryKeys(context.Background(), 2, 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generation failed")
}

// =============================================================================
// DeleteRecoveryKeys Tests
// =============================================================================

// TestBarrierSealManager_DeleteRecoveryKeys_Success verifies delete delegates
// to SDK.
func TestBarrierSealManager_DeleteRecoveryKeys_Success(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	err = mgr.DeleteRecoveryKeys(context.Background())
	require.NoError(t, err)
}

// TestBarrierSealManager_DeleteRecoveryKeys_SDKError verifies error
// propagation from SDK.
func TestBarrierSealManager_DeleteRecoveryKeys_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.deleteRecErr = errors.New("delete failed")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	err = mgr.DeleteRecoveryKeys(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete failed")
}

// =============================================================================
// GenerateRootToken Tests
// =============================================================================

// TestBarrierSealManager_GenerateRootToken_Success verifies root token
// generation delegates to SDK.
func TestBarrierSealManager_GenerateRootToken_Success(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	token, err := mgr.GenerateRootToken(context.Background(), StrategyShamirShares, [][]byte{[]byte("share-a"), []byte("share-b")})
	require.NoError(t, err)
	require.Equal(t, "generated-root-token", token)
}

// TestBarrierSealManager_GenerateRootToken_RecoveryKeysStrategy verifies
// recovery_keys strategy is accepted.
func TestBarrierSealManager_GenerateRootToken_RecoveryKeysStrategy(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	token, err := mgr.GenerateRootToken(context.Background(), StrategyRecoveryKeys, [][]byte{[]byte("key-a")})
	require.NoError(t, err)
	require.Equal(t, "generated-root-token", token)
}

// TestBarrierSealManager_GenerateRootToken_InvalidStrategy verifies that an
// unknown strategy produces an InvalidStrategyError.
func TestBarrierSealManager_GenerateRootToken_InvalidStrategy(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.GenerateRootToken(context.Background(), "invalid_strategy", [][]byte{[]byte("share")})
	require.Error(t, err)

	var invalidErr *InvalidStrategyError
	require.ErrorAs(t, err, &invalidErr)
	require.Equal(t, "invalid_strategy", invalidErr.Strategy)
}

// TestBarrierSealManager_GenerateRootToken_SDKError verifies error propagation.
func TestBarrierSealManager_GenerateRootToken_SDKError(t *testing.T) {
	client := newMockBarrierClient()
	client.rootTokenErr = errors.New("token generation failed")
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	_, err = mgr.GenerateRootToken(context.Background(), StrategyShamirShares, [][]byte{[]byte("share")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "token generation failed")
}

// =============================================================================
// Close Tests
// =============================================================================

// TestBarrierSealManager_Close_ReturnsNil verifies Close is a no-op that
// returns nil.
func TestBarrierSealManager_Close_ReturnsNil(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.NoError(t, mgr.Close())
}

// =============================================================================
// Error Type Tests
// =============================================================================

// TestErrorTypes_Messages verifies all error types produce the expected messages.
func TestErrorTypes_Messages(t *testing.T) {
	assert.Equal(t, "barrier_seal_manager: duplicate share submitted", (&DuplicateShareError{}).Error())
	assert.Equal(t, "barrier_seal_manager: barrier is already unsealed", (&AlreadyUnsealedError{}).Error())
	assert.Equal(t, "barrier_seal_manager: barrier must not be nil", (&BarrierNilError{}).Error())
	assert.Equal(t, "barrier_seal_manager: config must not be nil", (&ConfigNilError{}).Error())
	assert.Equal(t, "barrier_seal_manager: barrier has not been initialized", (&NotInitializedError{}).Error())
	assert.Equal(t, "barrier_seal_manager: barrier is sealed: test", (&BarrierSealedError{Op: "test"}).Error())
	assert.Equal(t, "barrier_seal_manager: shamir strategy not available for rekey", (&RekeyStrategyError{}).Error())
	assert.Equal(t, "barrier_seal_manager: invalid strategy: foo", (&InvalidStrategyError{Strategy: "foo"}).Error())
}

// =============================================================================
// Version Tracking Tests
// =============================================================================

// TestBarrierSealManager_VersionTracking verifies version increments across
// init and rekey operations.
func TestBarrierSealManager_VersionTracking(t *testing.T) {
	client := newMockBarrierClient()
	mgr, err := NewBarrierSealManager(client, DefaultConfig(), &testLogger{})
	require.NoError(t, err)

	require.Equal(t, uint64(0), mgr.version.Load())

	// Initialize sets version to 1
	_, err = mgr.Initialize(context.Background(), 3, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(1), mgr.version.Load())

	// First rekey increments to 2
	_, err = mgr.Rekey(context.Background(), 5, 3)
	require.NoError(t, err)
	require.Equal(t, uint64(2), mgr.version.Load())

	// Second rekey increments to 3
	_, err = mgr.Rekey(context.Background(), 7, 4)
	require.NoError(t, err)
	require.Equal(t, uint64(3), mgr.version.Load())
}
