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

// Package service provides unit tests for the Seal service implementation.
package service

import (
	"context"
	"errors"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Mock Seal Manager for Unit Testing
// =============================================================================

// mockSealManager implements interfaces.SealManager for testing.
type mockSealManager struct {
	isSealed               bool
	isInitialized          bool
	recoveryKeysEnabled    bool
	statusFn               func(ctx context.Context) (interface{}, error)
	initializeFn           func(ctx context.Context, shares, threshold int) (interface{}, error)
	unsealFn               func(ctx context.Context, share []byte) (interface{}, error)
	sealFn                 func(ctx context.Context) error
	rekeyFn                func(ctx context.Context, shares, threshold int) (interface{}, error)
	generateRecoveryKeysFn func(ctx context.Context, threshold, totalKeys int) ([][]byte, error)
	deleteRecoveryKeysFn   func(ctx context.Context) error
	generateRootTokenFn    func(ctx context.Context, strategy string, shares [][]byte) (string, error)
}

// generateMockShares creates mock share data for testing.
func generateMockShares(count int) [][]byte {
	shares := make([][]byte, count)
	for i := 0; i < count; i++ {
		shares[i] = []byte(fmt.Sprintf("share-%d", i+1))
	}
	return shares
}

func newMockSealManager() *mockSealManager {
	return &mockSealManager{
		isSealed:            true,
		isInitialized:       false,
		recoveryKeysEnabled: true,
	}
}

func (m *mockSealManager) Status(ctx context.Context) (interface{}, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	// Default behavior
	return &SealStatusResponse{
		Sealed:         m.isSealed,
		Initialized:    m.isInitialized,
		Mode:           "manual",
		Threshold:      3,
		TotalShares:    5,
		SharesProvided: 0,
		TPMAvailable:   false,
		ClusterNodes:   1,
		SharesReceived: 0,
		Version:        1,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockSealManager) IsSealed() bool {
	return m.isSealed
}

func (m *mockSealManager) IsInitialized() bool {
	return m.isInitialized
}

func (m *mockSealManager) Initialize(ctx context.Context, shares, threshold int) (interface{}, error) {
	if m.initializeFn != nil {
		return m.initializeFn(ctx, shares, threshold)
	}
	// Default behavior
	m.isInitialized = true
	return &SealInitResponse{
		Shares:       generateMockShares(shares),
		RecoveryKeys: [][]byte{},
		RootToken:    "root-token-12345",
		Version:      1,
		Timestamp:    time.Now(),
	}, nil
}

func (m *mockSealManager) Unseal(ctx context.Context, share []byte) (interface{}, error) {
	if m.unsealFn != nil {
		return m.unsealFn(ctx, share)
	}
	// Default behavior
	return &UnsealResponse{
		Sealed:         true,
		SharesProvided: 1,
		Threshold:      3,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockSealManager) Seal(ctx context.Context) error {
	if m.sealFn != nil {
		return m.sealFn(ctx)
	}
	// Default behavior
	m.isSealed = true
	return nil
}

func (m *mockSealManager) Rekey(ctx context.Context, shares, threshold int) (interface{}, error) {
	if m.rekeyFn != nil {
		return m.rekeyFn(ctx, shares, threshold)
	}
	// Default behavior
	return &RekeyResponse{
		Shares:    generateMockShares(shares),
		Version:   2,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSealManager) GenerateRecoveryKeys(ctx context.Context, threshold, totalKeys int) ([][]byte, error) {
	if m.generateRecoveryKeysFn != nil {
		return m.generateRecoveryKeysFn(ctx, threshold, totalKeys)
	}
	// Default behavior
	if m.isSealed {
		return nil, &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrUnauthorized,
			Op:   "seal.GenerateRecoveryKeys",
			Err:  stderrors.New("database must be unsealed"),
		}
	}
	return generateMockShares(totalKeys), nil
}

func (m *mockSealManager) DeleteRecoveryKeys(ctx context.Context) error {
	if m.deleteRecoveryKeysFn != nil {
		return m.deleteRecoveryKeysFn(ctx)
	}
	// Default behavior
	if m.isSealed {
		return &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrUnauthorized,
			Op:   "seal.DeleteRecoveryKeys",
			Err:  stderrors.New("database must be unsealed"),
		}
	}
	if !m.recoveryKeysEnabled {
		return &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrInvalidInput,
			Op:   "seal.DeleteRecoveryKeys",
			Err:  stderrors.New("recovery keys are not enabled"),
		}
	}
	m.recoveryKeysEnabled = false
	return nil
}

func (m *mockSealManager) GenerateRootToken(ctx context.Context, strategy string, shares [][]byte) (string, error) {
	if m.generateRootTokenFn != nil {
		return m.generateRootTokenFn(ctx, strategy, shares)
	}
	// Default behavior
	return "generated-root-token-12345", nil
}

func (m *mockSealManager) RotateRootKey(_ context.Context) error {
	return nil
}

func (m *mockSealManager) Close() error {
	return nil
}

// =============================================================================
// Status Tests
// =============================================================================

func TestSealService_Status_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isSealed = true
	mockManager.isInitialized = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.Status(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.True(t, resp.Initialized)
	assert.Equal(t, 3, resp.Threshold)
	assert.Equal(t, 5, resp.TotalShares)
}

func TestSealService_Status_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.statusFn = func(ctx context.Context) (interface{}, error) {
		return nil, errors.New("database connection failed")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.Status(context.Background())

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

func TestSealService_Status_InvalidType(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.statusFn = func(ctx context.Context) (interface{}, error) {
		// Return wrong type
		return "invalid-type", nil
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.Status(context.Background())

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unexpected status type")
}

// =============================================================================
// Initialize Tests
// =============================================================================

func TestSealService_Initialize_Success(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:           3,
		TotalShares:         5,
		RecoveryKeysEnabled: false,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Shares, 5)
	assert.NotEmpty(t, resp.RootToken)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestSealService_Initialize_WithRecoveryKeys(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.initializeFn = func(ctx context.Context, shares, threshold int) (interface{}, error) {
		return &SealInitResponse{
			Shares:       make([][]byte, shares),
			RecoveryKeys: make([][]byte, 3),
			RootToken:    "root-token-12345",
			Version:      1,
			Timestamp:    time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:           3,
		TotalShares:         5,
		RecoveryKeysEnabled: true,
		RecoveryThreshold:   2,
		RecoveryTotalKeys:   3,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Shares, 5)
	assert.Len(t, resp.RecoveryKeys, 3)
}

func TestSealService_Initialize_NilRequest(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.Initialize(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSealService_Initialize_ZeroThreshold(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:   0,
		TotalShares: 5,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "threshold must be positive")
}

func TestSealService_Initialize_ZeroTotalShares(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:   3,
		TotalShares: 0,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "total shares must be positive")
}

func TestSealService_Initialize_ThresholdExceedsShares(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:   6,
		TotalShares: 5,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "threshold (6) cannot exceed total shares (5)")
}

func TestSealService_Initialize_RecoveryKeysInvalidThreshold(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:           3,
		TotalShares:         5,
		RecoveryKeysEnabled: true,
		RecoveryThreshold:   0,
		RecoveryTotalKeys:   3,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "recovery threshold must be positive")
}

func TestSealService_Initialize_RecoveryKeysInvalidTotal(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:           3,
		TotalShares:         5,
		RecoveryKeysEnabled: true,
		RecoveryThreshold:   2,
		RecoveryTotalKeys:   0,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "recovery total keys must be positive")
}

func TestSealService_Initialize_RecoveryThresholdExceedsTotal(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:           3,
		TotalShares:         5,
		RecoveryKeysEnabled: true,
		RecoveryThreshold:   4,
		RecoveryTotalKeys:   3,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "recovery threshold (4) cannot exceed recovery total keys (3)")
}

func TestSealService_Initialize_AlreadyInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:   3,
		TotalShares: 5,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrAlreadyExists, dErr.Code)
	}
}

func TestSealService_Initialize_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.initializeFn = func(ctx context.Context, shares, threshold int) (interface{}, error) {
		return nil, errors.New("HSM initialization failed")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:   3,
		TotalShares: 5,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

func TestSealService_Initialize_InvalidResponseType(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.initializeFn = func(ctx context.Context, shares, threshold int) (interface{}, error) {
		return "invalid-type", nil
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &SealInitRequest{
		Threshold:   3,
		TotalShares: 5,
	}

	resp, err := svc.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unexpected response type")
}

// =============================================================================
// Unseal Tests
// =============================================================================

func TestSealService_Unseal_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &UnsealRequest{
		Share: []byte("share-1"),
	}

	resp, err := svc.Unseal(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.Equal(t, 1, resp.SharesProvided)
	assert.Equal(t, 3, resp.Threshold)
}

func TestSealService_Unseal_CompleteUnseal(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.unsealFn = func(ctx context.Context, share []byte) (interface{}, error) {
		// Simulate successful unseal
		mockManager.isSealed = false
		return &UnsealResponse{
			Sealed:         false,
			SharesProvided: 3,
			Threshold:      3,
			Timestamp:      time.Now(),
		}, nil
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &UnsealRequest{
		Share: []byte("final-share"),
	}

	resp, err := svc.Unseal(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Sealed)
	assert.Equal(t, 3, resp.SharesProvided)
}

func TestSealService_Unseal_NilRequest(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.Unseal(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSealService_Unseal_EmptyShare(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &UnsealRequest{
		Share: []byte{},
	}

	resp, err := svc.Unseal(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "share cannot be empty")
}

func TestSealService_Unseal_NotInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &UnsealRequest{
		Share: []byte("share-1"),
	}

	resp, err := svc.Unseal(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSealService_Unseal_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.unsealFn = func(ctx context.Context, share []byte) (interface{}, error) {
		return nil, errors.New("invalid share")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &UnsealRequest{
		Share: []byte("bad-share"),
	}

	resp, err := svc.Unseal(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

func TestSealService_Unseal_InvalidResponseType(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.unsealFn = func(ctx context.Context, share []byte) (interface{}, error) {
		return 123, nil
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &UnsealRequest{
		Share: []byte("share-1"),
	}

	resp, err := svc.Unseal(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unexpected response type")
}

// =============================================================================
// Seal Tests
// =============================================================================

func TestSealService_Seal_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isSealed = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	err := svc.Seal(context.Background())

	require.NoError(t, err)
	assert.True(t, mockManager.isSealed)
}

func TestSealService_Seal_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.sealFn = func(ctx context.Context) error {
		return errors.New("failed to clear master key")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	err := svc.Seal(context.Background())

	require.Error(t, err)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

// =============================================================================
// Rekey Tests
// =============================================================================

func TestSealService_Rekey_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 4,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Shares, 4)
	assert.Equal(t, uint64(2), resp.Version)
}

func TestSealService_Rekey_NilRequest(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.Rekey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSealService_Rekey_ZeroThreshold(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   0,
		NewTotalShares: 4,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "new threshold must be positive")
}

func TestSealService_Rekey_ZeroTotalShares(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 0,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "new total shares must be positive")
}

func TestSealService_Rekey_ThresholdExceedsShares(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   5,
		NewTotalShares: 4,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "new threshold (5) cannot exceed new total shares (4)")
}

func TestSealService_Rekey_NotInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 4,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSealService_Rekey_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.rekeyFn = func(ctx context.Context, shares, threshold int) (interface{}, error) {
		return nil, errors.New("rekey operation failed")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 4,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

func TestSealService_Rekey_InvalidResponseType(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.rekeyFn = func(ctx context.Context, shares, threshold int) (interface{}, error) {
		return map[string]string{"invalid": "type"}, nil
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 4,
	}

	resp, err := svc.Rekey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unexpected response type")
}

// =============================================================================
// GenerateRecoveryKey Tests
// =============================================================================

func TestSealService_GenerateRecoveryKey_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.isSealed = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRecoveryKeyRequest{
		RecoveryThreshold: 2,
		RecoveryTotalKeys: 3,
	}

	resp, err := svc.GenerateRecoveryKey(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.RecoveryKeys, 3)
}

func TestSealService_GenerateRecoveryKey_NilRequest(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.GenerateRecoveryKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSealService_GenerateRecoveryKey_ZeroThreshold(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRecoveryKeyRequest{
		RecoveryThreshold: 0,
		RecoveryTotalKeys: 3,
	}

	resp, err := svc.GenerateRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "recovery threshold must be positive")
}

func TestSealService_GenerateRecoveryKey_ZeroTotalKeys(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRecoveryKeyRequest{
		RecoveryThreshold: 2,
		RecoveryTotalKeys: 0,
	}

	resp, err := svc.GenerateRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "recovery total keys must be positive")
}

func TestSealService_GenerateRecoveryKey_ThresholdExceedsTotal(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRecoveryKeyRequest{
		RecoveryThreshold: 4,
		RecoveryTotalKeys: 3,
	}

	resp, err := svc.GenerateRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "recovery threshold (4) cannot exceed recovery total keys (3)")
}

func TestSealService_GenerateRecoveryKey_NotInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRecoveryKeyRequest{
		RecoveryThreshold: 2,
		RecoveryTotalKeys: 3,
	}

	resp, err := svc.GenerateRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSealService_GenerateRecoveryKey_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.generateRecoveryKeysFn = func(ctx context.Context, threshold, totalKeys int) ([][]byte, error) {
		return nil, errors.New("failed to generate recovery keys")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRecoveryKeyRequest{
		RecoveryThreshold: 2,
		RecoveryTotalKeys: 3,
	}

	resp, err := svc.GenerateRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

// =============================================================================
// DeleteRecoveryKey Tests
// =============================================================================

func TestSealService_DeleteRecoveryKey_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.isSealed = false
	mockManager.recoveryKeysEnabled = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &DeleteRecoveryKeyRequest{
		Confirm: true,
	}

	resp, err := svc.DeleteRecoveryKey(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestSealService_DeleteRecoveryKey_NilRequest(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.DeleteRecoveryKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSealService_DeleteRecoveryKey_NotConfirmed(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &DeleteRecoveryKeyRequest{
		Confirm: false,
	}

	resp, err := svc.DeleteRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "must confirm deletion")
}

func TestSealService_DeleteRecoveryKey_NotInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &DeleteRecoveryKeyRequest{
		Confirm: true,
	}

	resp, err := svc.DeleteRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSealService_DeleteRecoveryKey_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.deleteRecoveryKeysFn = func(ctx context.Context) error {
		return errors.New("failed to delete recovery keys")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &DeleteRecoveryKeyRequest{
		Confirm: true,
	}

	resp, err := svc.DeleteRecoveryKey(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

// =============================================================================
// GenerateRootToken Tests
// =============================================================================

func TestSealService_GenerateRootToken_Success(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "recovery_keys",
		Shares:   [][]byte{[]byte("share-1"), []byte("share-2")},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.RootToken)
}

func TestSealService_GenerateRootToken_ShamirStrategy(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "shamir_shares",
		Shares:   [][]byte{[]byte("share-1"), []byte("share-2"), []byte("share-3")},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.RootToken)
}

func TestSealService_GenerateRootToken_NilRequest(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	resp, err := svc.GenerateRootToken(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestSealService_GenerateRootToken_EmptyStrategy(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "",
		Shares:   [][]byte{[]byte("share-1")},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "strategy cannot be empty")
}

func TestSealService_GenerateRootToken_InvalidStrategy(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "invalid_strategy",
		Shares:   [][]byte{[]byte("share-1")},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid strategy")
}

func TestSealService_GenerateRootToken_EmptyShares(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "recovery_keys",
		Shares:   [][]byte{},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "shares cannot be empty")
}

func TestSealService_GenerateRootToken_NotInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "recovery_keys",
		Shares:   [][]byte{[]byte("share-1"), []byte("share-2")},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrNotFound, dErr.Code)
	}
}

func TestSealService_GenerateRootToken_ManagerError(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.generateRootTokenFn = func(ctx context.Context, strategy string, shares [][]byte) (string, error) {
		return "", errors.New("failed to generate root token")
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	req := &GenerateRootTokenRequest{
		Strategy: "recovery_keys",
		Shares:   [][]byte{[]byte("share-1"), []byte("share-2")},
	}

	resp, err := svc.GenerateRootToken(context.Background(), req)

	require.Error(t, err)
	assert.Nil(t, resp)
	var dErr *quicbaasErrors.QBError
	if errors.As(err, &dErr) {
		assert.Equal(t, quicbaasErrors.ErrInternal, dErr.Code)
	}
}

// =============================================================================
// Service Construction Tests
// =============================================================================

func TestNewSealServiceImpl_NilLogger(t *testing.T) {
	mockManager := newMockSealManager()
	svc := newSealServiceImpl(mockManager, nil)

	// Service should still be usable without logger
	resp, err := svc.Status(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestSealService_MetricsInitialized(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	// Verify metrics are initialized
	assert.NotNil(t, svc.metrics)
	assert.NotNil(t, svc.metrics.sealOperationsTotal)
	assert.NotNil(t, svc.metrics.unsealOperationsTotal)
	assert.NotNil(t, svc.metrics.rekeyOperationsTotal)
	assert.NotNil(t, svc.metrics.recoveryKeyOpsTotal)
	assert.NotNil(t, svc.metrics.rootTokenOpsTotal)
	assert.NotNil(t, svc.metrics.operationDuration)
	assert.NotNil(t, svc.metrics.sealStatus)
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestSealService_Initialize_ContextCanceled(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.initializeFn = func(ctx context.Context, shares, threshold int) (interface{}, error) {
		return nil, ctx.Err()
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &SealInitRequest{
		Threshold:   3,
		TotalShares: 5,
	}

	resp, err := svc.Initialize(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSealService_Unseal_ContextCanceled(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.unsealFn = func(ctx context.Context, share []byte) (interface{}, error) {
		return nil, ctx.Err()
	}
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &UnsealRequest{
		Share: []byte("share-1"),
	}

	resp, err := svc.Unseal(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Integration Scenarios
// =============================================================================

func TestSealService_FullLifecycle(t *testing.T) {
	mockManager := newMockSealManager()
	logger := newMockLogger()

	// Setup unseal tracking
	sharesProvided := 0
	mockManager.unsealFn = func(ctx context.Context, share []byte) (interface{}, error) {
		sharesProvided++
		sealed := sharesProvided < 3
		if !sealed {
			mockManager.isSealed = false
		}
		return &UnsealResponse{
			Sealed:         sealed,
			SharesProvided: sharesProvided,
			Threshold:      3,
			Timestamp:      time.Now(),
		}, nil
	}

	svc := newSealServiceImpl(mockManager, logger)

	// 1. Check initial status
	statusResp, err := svc.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, statusResp.Sealed)
	assert.False(t, statusResp.Initialized)

	// 2. Initialize
	initReq := &SealInitRequest{
		Threshold:   3,
		TotalShares: 5,
	}
	initResp, err := svc.Initialize(context.Background(), initReq)
	require.NoError(t, err)
	assert.Len(t, initResp.Shares, 5)
	assert.NotEmpty(t, initResp.RootToken)

	// 3. Provide unseal shares
	for i := 0; i < 3; i++ {
		unsealReq := &UnsealRequest{
			Share: initResp.Shares[i],
		}
		unsealResp, err := svc.Unseal(context.Background(), unsealReq)
		require.NoError(t, err)
		assert.Equal(t, i+1, unsealResp.SharesProvided)
		if i < 2 {
			assert.True(t, unsealResp.Sealed)
		} else {
			assert.False(t, unsealResp.Sealed)
		}
	}

	// 4. Seal it again
	err = svc.Seal(context.Background())
	require.NoError(t, err)
	assert.True(t, mockManager.isSealed)
}

func TestSealService_RekeyScenario(t *testing.T) {
	mockManager := newMockSealManager()
	mockManager.isInitialized = true
	mockManager.isSealed = false
	logger := newMockLogger()
	svc := newSealServiceImpl(mockManager, logger)

	// Perform rekey
	rekeyReq := &RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 4,
	}
	rekeyResp, err := svc.Rekey(context.Background(), rekeyReq)

	require.NoError(t, err)
	assert.NotNil(t, rekeyResp)
	assert.Len(t, rekeyResp.Shares, 4)
	assert.Equal(t, uint64(2), rekeyResp.Version)
}
