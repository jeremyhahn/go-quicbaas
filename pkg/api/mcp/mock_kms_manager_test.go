//go:build mcp

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

package mcp

import (
	"context"
	"crypto"
	"sync"
	"time"

	"github.com/stretchr/testify/mock"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// MockKMSManager is a testify mock implementation of interfaces.KMSManager
// for testing MCP KMS tools.
type MockKMSManager struct {
	mock.Mock
	mu sync.RWMutex
}

// NewMockKMSManager creates a new MockKMSManager.
func NewMockKMSManager() *MockKMSManager {
	return &MockKMSManager{}
}

func (m *MockKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	args := m.Called(ctx, attrs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, _ bool) (interface{}, error) {
	args := m.Called(ctx, keyID, material, algorithm)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	args := m.Called(ctx, keyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	args := m.Called(metadata)
	return args.Error(0)
}

func (m *MockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	args := m.Called(ctx, prefix)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	args := m.Called(ctx, keyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	args := m.Called(ctx, keyID, plaintext, aad)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	args := m.Called(ctx, keyID, ciphertext, aad)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	args := m.Called(ctx, keyID, digest, algorithm)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	args := m.Called(ctx, keyID, digest, signature, algorithm)
	return args.Bool(0), args.Error(1)
}

func (m *MockKMSManager) GenerateDataKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
	args := m.Called(ctx, attrs, keyBytes)
	var pt, wp []byte
	if args.Get(0) != nil {
		pt = args.Get(0).([]byte)
	}
	if args.Get(1) != nil {
		wp = args.Get(1).([]byte)
	}
	return pt, wp, args.Error(2)
}

func (m *MockKMSManager) GetBackendType() config.BackendType {
	args := m.Called()
	return args.Get(0).(config.BackendType)
}

func (m *MockKMSManager) IsAvailable(ctx context.Context) bool {
	args := m.Called(ctx)
	return args.Bool(0)
}

func (m *MockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	args := m.Called(ctx, keyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Close() error {
	args := m.Called()
	return args.Error(0)
}

// createTestKeyMetadata creates a test KeyMetadata instance for testing.
func createTestKeyMetadata(keyID, name, algorithm string) *kms.KeyMetadata {
	now := time.Now()
	return &kms.KeyMetadata{
		KeyID:              keyID,
		Name:               name,
		Description:        "Test key",
		Algorithm:          algorithm,
		State:              kms.KeyStateEnabled,
		Version:            1,
		Exportable:         false,
		DeletionProtection: true,
		Created:            now,
		Updated:            now,
		Usage:              []kms.KeyUsage{kms.KeyUsageEncrypt, kms.KeyUsageDecrypt},
		Tags:               map[string]string{"env": "test"},
		Owner:              "test-user",
	}
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *MockKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}
