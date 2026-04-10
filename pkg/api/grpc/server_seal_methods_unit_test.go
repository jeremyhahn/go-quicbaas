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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestSetGlobalSealManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	// Create a mock seal manager (nil is acceptable for this test)
	SetGlobalSealManager(nil)
	assert.Nil(t, GetGlobalSealManager())
}

func TestGetGlobalSealManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	mgr := GetGlobalSealManager()
	assert.Nil(t, mgr)
}

func TestInitializeDefaultSealManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	err = InitializeDefaultSealManager(log)
	assert.NoError(t, err)
	assert.NotNil(t, GetGlobalSealManager())

	// Test calling again should be a no-op
	err = InitializeDefaultSealManager(log)
	assert.NoError(t, err)

	// Cleanup
	globalSealManager = nil
}

func TestServerSealStatus_Uninitialized(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	resp, err := srv.SealStatus(context.Background(), &pb.SealStatusRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.False(t, resp.Initialized)
}

func TestServerSealStatus_WithManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize the manager
	err = InitializeDefaultSealManager(log)
	require.NoError(t, err)
	defer func() { globalSealManager = nil }()

	srv := &Server{
		logger: log,
	}

	resp, err := srv.SealStatus(context.Background(), &pb.SealStatusRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestServerSealInit_Success(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	req := &pb.SealInitRequest{
		Threshold:   2,
		TotalShares: 3,
	}

	resp, err := srv.SealInit(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// Cleanup
	globalSealManager = nil
}

func TestServerUnseal_NoManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	req := &pb.UnsealRequest{
		Share: []byte("test-share"),
	}

	resp, err := srv.Unseal(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.Contains(t, resp.Error, "not initialized")
}

func TestServerUnseal_WithManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize and bootstrap the manager
	err = InitializeDefaultSealManager(log)
	require.NoError(t, err)
	defer func() { globalSealManager = nil }()

	// Initialize to set up shares
	_, err = globalSealManager.Initialize(context.Background(), 3, 2)
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	req := &pb.UnsealRequest{
		Share: []byte("invalid-share"), // Will fail but covers the code path
	}

	resp, err := srv.Unseal(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestServerSeal_NoManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	req := &pb.SealRequest{
		Force: false,
	}

	resp, err := srv.Seal(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.Contains(t, resp.Error, "not initialized")
}

func TestServerSeal_WithManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize the manager
	err = InitializeDefaultSealManager(log)
	require.NoError(t, err)
	defer func() { globalSealManager = nil }()

	srv := &Server{
		logger: log,
	}

	req := &pb.SealRequest{
		Force: false,
	}

	resp, err := srv.Seal(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestServerSealRekey_NoManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	req := &pb.SealRekeyRequest{
		NewThreshold: 3,
		NewTotal:     5,
	}

	resp, err := srv.SealRekey(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Contains(t, resp.Error, "not initialized")
}

func TestServerSealRekey_WithManager(t *testing.T) {
	// Reset global state
	globalSealManager = nil

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	// Initialize the manager
	err = InitializeDefaultSealManager(log)
	require.NoError(t, err)
	defer func() { globalSealManager = nil }()

	// Initialize first (replaces Bootstrap)
	_, err = globalSealManager.Initialize(context.Background(), 3, 2)
	require.NoError(t, err)

	srv := &Server{
		logger: log,
	}

	req := &pb.SealRekeyRequest{
		NewThreshold: 3,
		NewTotal:     5,
	}

	resp, err := srv.SealRekey(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestConvertSealMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected pb.SealMode
	}{
		{
			name:     "manual mode",
			mode:     "manual",
			expected: pb.SealMode_SEAL_MODE_MANUAL,
		},
		{
			name:     "auto mode",
			mode:     "auto",
			expected: pb.SealMode_SEAL_MODE_AUTO,
		},
		{
			name:     "crypto mode",
			mode:     "crypto",
			expected: pb.SealMode_SEAL_MODE_CRYPTO,
		},
		{
			name:     "unspecified mode",
			mode:     "unknown",
			expected: pb.SealMode_SEAL_MODE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSealMode(tt.mode)
			assert.Equal(t, tt.expected, result)
		})
	}
}
