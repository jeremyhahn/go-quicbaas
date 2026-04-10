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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// SetKeySafety Deep Path Validation Tests
// =============================================================================

func TestKMSService_SetKeySafety_NilRequest(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.SetKeySafety(context.Background(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestKMSService_SetKeySafety_InvalidWarningPercentTooHigh(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Add a key
	mockMgr.keys["test-key"] = &mockKeyData{
		keyID:     "test-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	resp, err := svc.SetKeySafety(context.Background(), &SetKeySafetyRequest{
		KeyID:          "test-key",
		WarningPercent: 150, // Invalid: > 100
	})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestKMSService_SetKeySafety_InvalidAutoRotatePercentNegative(t *testing.T) {
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

	resp, err := svc.SetKeySafety(context.Background(), &SetKeySafetyRequest{
		KeyID:             "test-key",
		AutoRotatePercent: -5, // Invalid: < 0
	})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestKMSService_SetKeySafety_WarningGEAutoRotate(t *testing.T) {
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

	resp, err := svc.SetKeySafety(context.Background(), &SetKeySafetyRequest{
		KeyID:             "test-key",
		WarningPercent:    90,
		AutoRotatePercent: 80, // Invalid: warning >= auto-rotate
	})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestKMSService_SetKeySafety_DefaultsApplied(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.keys["default-key"] = &mockKeyData{
		keyID:     "default-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	// All zero values should use defaults
	resp, err := svc.SetKeySafety(context.Background(), &SetKeySafetyRequest{
		KeyID: "default-key",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "default-key", resp.KeyID)
	assert.True(t, resp.Config.MaxOperations > 0)
	assert.True(t, resp.Config.MaxBytes > 0)
}

func TestKMSService_SetKeySafety_NilLogger(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, nil)

	mockMgr.keys["no-logger-key"] = &mockKeyData{
		keyID:     "no-logger-key",
		algorithm: "AES-256-GCM",
		version:   1,
		status:    "enabled",
		createdAt: time.Now(),
	}

	resp, err := svc.SetKeySafety(context.Background(), &SetKeySafetyRequest{
		KeyID:             "no-logger-key",
		MaxOperations:     500000,
		WarningPercent:    70,
		AutoRotatePercent: 90,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "no-logger-key", resp.KeyID)
}

// =============================================================================
// SetDEKSafety Deep Path Validation Tests
// =============================================================================

func TestKMSService_SetDEKSafety_InvalidWarningPercentTooHigh(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	// Create a DEK directly in the store - svc is already *kmsServiceImpl
	svc.dekStore.set("test/dek", &dekEntry{
		metadata: &DEKMetadata{
			Path:      "test/dek",
			Algorithm: "AES-256-GCM",
			Status:    string(DEKStatusActive),
			Version:   1,
			CreatedAt: time.Now(),
		},
	})

	resp, err := svc.SetDEKSafety(context.Background(), &SetDEKSafetyRequest{
		Path:           "test/dek",
		WarningPercent: 150, // Invalid
	})
	assert.Nil(t, resp)
	require.Error(t, err)
}

func TestKMSService_SetDEKSafety_DEKNotFound(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.SetDEKSafety(context.Background(), &SetDEKSafetyRequest{
		Path: "nonexistent/dek",
	})
	assert.Nil(t, resp)
	require.Error(t, err)
}
