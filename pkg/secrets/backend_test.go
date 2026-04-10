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

// Package secrets provides secret storage and retrieval functionality for QuicBaaS.
package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
)

// MockBackend is a mock implementation of the Backend interface for testing.
type MockBackend struct {
	mock.Mock
}

// Store saves a secret at the specified path.
func (m *MockBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	args := m.Called(ctx, path, data, metadata)
	return args.Error(0)
}

// Get retrieves a secret from the specified path.
func (m *MockBackend) Get(ctx context.Context, path string) (data map[string][]byte, metadata map[string]string, err error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(map[string][]byte), args.Get(1).(map[string]string), args.Error(2)
}

// GetVersion retrieves a specific version of a secret.
func (m *MockBackend) GetVersion(ctx context.Context, path string, version int) (data map[string][]byte, metadata map[string]string, err error) {
	args := m.Called(ctx, path, version)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(map[string][]byte), args.Get(1).(map[string]string), args.Error(2)
}

// List returns all secret paths matching the prefix.
func (m *MockBackend) List(ctx context.Context, prefix string) ([]string, error) {
	args := m.Called(ctx, prefix)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// Delete removes a secret (soft delete, keeps versions).
func (m *MockBackend) Delete(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

// Destroy permanently removes all versions of a secret.
func (m *MockBackend) Destroy(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

// ListVersions returns metadata for all versions of a secret.
func (m *MockBackend) ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*VersionMetadata), args.Error(1)
}

// Rollback restores a previous version as the current version.
func (m *MockBackend) Rollback(ctx context.Context, path string, version int) error {
	args := m.Called(ctx, path, version)
	return args.Error(0)
}

// Type returns the backend type identifier.
func (m *MockBackend) Type() string {
	args := m.Called()
	return args.String(0)
}

// Close releases any resources held by the backend.
func (m *MockBackend) Close() error {
	args := m.Called()
	return args.Error(0)
}

// NewMockBackend creates a new mock backend instance.
func NewMockBackend() *MockBackend {
	m := &MockBackend{}
	m.On("Type").Return("mock")
	m.On("Close").Return(nil)
	return m
}

// testAny is a test helper that matches any argument in mock calls.
var testAny = mock.Anything

// TestSecretsCauseError_Unwrap tests the Unwrap method of SecretsCauseError.
func TestSecretsCauseError_Unwrap(t *testing.T) {
	cause := errors.New("underlying cause")
	err := &SecretsCauseError{
		Message: "operation failed",
		Cause:   cause,
	}

	// Test Unwrap returns the cause
	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Test errors.Is works through Unwrap
	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the cause through Unwrap")
	}
}

// TestSecretsCauseError_Error tests the Error method of SecretsCauseError.
func TestSecretsCauseError_Error(t *testing.T) {
	cause := errors.New("disk full")
	err := &SecretsCauseError{
		Message: "failed to store secret",
		Cause:   cause,
	}

	expected := "failed to store secret: disk full"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// TestSecretsSentinelCauseError_Unwrap tests the Unwrap method of SecretsSentinelCauseError.
func TestSecretsSentinelCauseError_Unwrap(t *testing.T) {
	sentinel := ErrEncryptionFailed
	cause := errors.New("key unavailable")
	err := &SecretsSentinelCauseError{
		Sentinel: sentinel,
		Cause:    cause,
	}

	// Test Unwrap returns both errors
	unwrapped := err.Unwrap()
	if len(unwrapped) != 2 {
		t.Fatalf("Unwrap() returned %d errors, want 2", len(unwrapped))
	}
	if unwrapped[0] != sentinel {
		t.Errorf("Unwrap()[0] = %v, want %v", unwrapped[0], sentinel)
	}
	if unwrapped[1] != cause {
		t.Errorf("Unwrap()[1] = %v, want %v", unwrapped[1], cause)
	}

	// Test errors.Is works for both sentinel and cause
	if !errors.Is(err, sentinel) {
		t.Error("errors.Is should find sentinel through Unwrap")
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is should find cause through Unwrap")
	}
}

// TestSecretsSentinelCauseError_Error tests the Error method of SecretsSentinelCauseError.
func TestSecretsSentinelCauseError_Error(t *testing.T) {
	sentinel := ErrEncryptionFailed
	cause := errors.New("cipher error")
	err := &SecretsSentinelCauseError{
		Sentinel: sentinel,
		Cause:    cause,
	}

	expected := sentinel.Error() + ": cipher error"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
