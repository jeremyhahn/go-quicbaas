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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// ListKeys Deep Path Tests - Uncovered branches
// =============================================================================

func TestKMSService_ListKeys_WithServiceKeyMetadata(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	created := time.Now()
	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{
			&KeyMetadata{
				KeyID:     "svc-key-1",
				Algorithm: KeyAlgAES256GCM,
				Version:   3,
				Status:    KeyStatusEnabled,
				CreatedAt: created,
			},
		}, nil
	}

	req := &ListKeysRequest{Limit: 10}

	resp, err := svc.ListKeys(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.TotalCount)
	assert.Equal(t, "svc-key-1", resp.Keys[0].KeyID)
	assert.Equal(t, uint64(3), resp.Keys[0].Version)
	assert.Equal(t, KeyAlgAES256GCM, resp.Keys[0].Algorithm)
	assert.Equal(t, KeyStatusEnabled, resp.Keys[0].Status)
}

func TestKMSService_ListKeys_WithReflectionFallback(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Return an unknown type that has KeyID and Version fields
	type customKey struct {
		KeyID     string
		Version   int
		Algorithm string
		State     string
	}

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{
			&customKey{
				KeyID:     "custom-key-1",
				Version:   5,
				Algorithm: "ChaCha20",
				State:     "enabled",
			},
		}, nil
	}

	req := &ListKeysRequest{Limit: 10}

	resp, err := svc.ListKeys(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.TotalCount)
	assert.Equal(t, "custom-key-1", resp.Keys[0].KeyID)
	assert.Equal(t, uint64(5), resp.Keys[0].Version)
	assert.Equal(t, KeyAlgorithm("ChaCha20"), resp.Keys[0].Algorithm)
}

func TestKMSService_ListKeys_WithReflectionUintVersion(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	type customKey struct {
		KeyID   string
		Version uint64
	}

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{
			&customKey{KeyID: "uint-key", Version: 10},
		}, nil
	}

	req := &ListKeysRequest{Limit: 10}

	resp, err := svc.ListKeys(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(10), resp.Keys[0].Version)
}

func TestKMSService_ListKeys_LimitTruncation(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		results := make([]interface{}, 5)
		for i := 0; i < 5; i++ {
			results[i] = &kms.KeyMetadata{
				KeyID:     "key-" + string(rune('a'+i)),
				Algorithm: "AES-256-GCM",
				Version:   1,
				State:     kms.KeyStateEnabled,
				Created:   time.Now(),
			}
		}
		return results, nil
	}

	req := &ListKeysRequest{Limit: 3}

	resp, err := svc.ListKeys(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Keys, 3)
	assert.Equal(t, 5, resp.TotalCount)
	assert.True(t, resp.HasMore)
}

func TestKMSService_ListKeys_NilKeyInResults(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{
			nil,
			&kms.KeyMetadata{
				KeyID:     "valid-key",
				Algorithm: "AES-256-GCM",
				Version:   1,
				State:     kms.KeyStateEnabled,
				Created:   time.Now(),
			},
		}, nil
	}

	req := &ListKeysRequest{Limit: 10}

	resp, err := svc.ListKeys(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, len(resp.Keys))
}

func TestKMSService_ListKeys_NilFilter(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	var capturedPrefix string
	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		capturedPrefix = prefix
		return []interface{}{}, nil
	}

	req := &ListKeysRequest{Filter: nil, Limit: 10}

	resp, err := svc.ListKeys(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, capturedPrefix)
}

func TestKMSService_ListKeys_ManagerListError(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return nil, errors.New("storage unavailable")
	}

	req := &ListKeysRequest{Limit: 10}

	resp, err := svc.ListKeys(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list keys")
}

// =============================================================================
// UpdateKeyStatus Deep Path Tests - Uncovered branches
// =============================================================================

func TestKMSService_UpdateKeyStatus_VersionSpecificDisable(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.keys["test-key"] = &mockKeyData{
		keyID:     "test-key",
		algorithm: "AES-256-GCM",
		version:   3,
		status:    "enabled",
		createdAt: time.Now(),
	}

	req := &UpdateKeyStatusRequest{
		KeyID:   "test-key",
		Version: 2,
		Status:  KeyStatusDisabled,
		Reason:  "disabling old version",
	}

	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(2), resp.Version)
	assert.Equal(t, KeyStatusDisabled, resp.NewStatus)
}

func TestKMSService_UpdateKeyStatus_SameStatusNoOp(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.keys["test-key"] = &mockKeyData{
		keyID:     "test-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	req := &UpdateKeyStatusRequest{
		KeyID:  "test-key",
		Status: KeyStatusEnabled,
	}

	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyStatusEnabled, resp.OldStatus)
	assert.Equal(t, KeyStatusEnabled, resp.NewStatus)
}

func TestKMSService_UpdateKeyStatus_WithKMSMetadata(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &kms.KeyMetadata{
			KeyID:     keyID,
			Algorithm: "AES-256-GCM",
			Version:   1,
			State:     kms.KeyStateEnabled,
		}, nil
	}

	mockMgr.keys["test-key"] = &mockKeyData{
		keyID:     "test-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	req := &UpdateKeyStatusRequest{
		KeyID:  "test-key",
		Status: KeyStatusDisabled,
	}

	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyStatusEnabled, resp.OldStatus)
	assert.Equal(t, KeyStatusDisabled, resp.NewStatus)
}

func TestKMSService_UpdateKeyStatus_WithServiceKeyMetadata(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &KeyMetadata{
			KeyID:     keyID,
			Algorithm: KeyAlgAES256GCM,
			Version:   1,
			Status:    KeyStatusDisabled,
		}, nil
	}

	mockMgr.keys["test-key"] = &mockKeyData{
		keyID:     "test-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "disabled",
		createdAt: time.Now(),
	}

	req := &UpdateKeyStatusRequest{
		KeyID:  "test-key",
		Status: KeyStatusEnabled,
	}

	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, KeyStatusDisabled, resp.OldStatus)
	assert.Equal(t, KeyStatusEnabled, resp.NewStatus)
}

func TestKMSService_UpdateKeyStatus_EnableKeyFailure(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &kms.KeyMetadata{
			KeyID: keyID,
			State: kms.KeyStateDisabled,
		}, nil
	}

	req := &UpdateKeyStatusRequest{
		KeyID:  "no-such-key-in-map",
		Status: KeyStatusEnabled,
	}

	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to enable key")
}

func TestKMSService_UpdateKeyStatus_DisableKeyFailure(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.getKeyFn = func(ctx context.Context, keyID string) (interface{}, error) {
		return &kms.KeyMetadata{
			KeyID: keyID,
			State: kms.KeyStateEnabled,
		}, nil
	}

	req := &UpdateKeyStatusRequest{
		KeyID:  "no-such-key-in-map",
		Status: KeyStatusDisabled,
	}

	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to disable key")
}

func TestKMSService_UpdateKeyStatus_VersionSpecificEnable(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.keys["test-key"] = &mockKeyData{
		keyID:     "test-key",
		algorithm: "AES-256-GCM",
		version:   3,
		status:    "enabled",
		createdAt: time.Now(),
	}

	// First disable version 2
	req := &UpdateKeyStatusRequest{
		KeyID:   "test-key",
		Version: 2,
		Status:  KeyStatusDisabled,
	}
	resp, err := svc.UpdateKeyStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Now re-enable version 2
	req2 := &UpdateKeyStatusRequest{
		KeyID:   "test-key",
		Version: 2,
		Status:  KeyStatusEnabled,
	}
	resp2, err := svc.UpdateKeyStatus(context.Background(), req2)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, KeyStatusDisabled, resp2.OldStatus)
	assert.Equal(t, KeyStatusEnabled, resp2.NewStatus)
}

// =============================================================================
// CancelReencryption Tests
// =============================================================================

func TestKMSService_CancelReencryption_NoActiveReencryption(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	err := svc.CancelReencryption(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no re-encryption operation in progress")
}
