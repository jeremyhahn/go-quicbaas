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

package cache

import (
	"context"
	"errors"
	"testing"
)

// mockInternalSecretStore is a mock implementation of InternalSecretStore for testing.
type mockInternalSecretStore struct {
	secrets   map[string][]byte
	getErr    error
	storeErr  error
	deleteErr error
	existsErr error
}

// newMockInternalSecretStore creates a new mock internal secret store.
func newMockInternalSecretStore() *mockInternalSecretStore {
	return &mockInternalSecretStore{
		secrets: make(map[string][]byte),
	}
}

// Get retrieves a secret from the mock store.
func (m *mockInternalSecretStore) Get(ctx context.Context, key string) ([]byte, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}

	value, exists := m.secrets[key]
	if !exists {
		return nil, errors.New("secret not found")
	}

	return value, nil
}

// Store stores a secret in the mock store.
func (m *mockInternalSecretStore) Store(ctx context.Context, key string, value []byte) error {
	if m.storeErr != nil {
		return m.storeErr
	}

	m.secrets[key] = value
	return nil
}

// Delete removes a secret from the mock store.
func (m *mockInternalSecretStore) Delete(ctx context.Context, key string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}

	_, exists := m.secrets[key]
	if !exists {
		return errors.New("secret not found")
	}

	delete(m.secrets, key)
	return nil
}

// Exists checks if a secret exists in the mock store.
func (m *mockInternalSecretStore) Exists(ctx context.Context, key string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}

	_, exists := m.secrets[key]
	return exists, nil
}

// TestNewInternalSecretsFetcher tests creating a new InternalSecretsFetcher.
func TestNewInternalSecretsFetcher(t *testing.T) {
	tests := []struct {
		name    string
		store   InternalSecretStore
		wantErr bool
	}{
		{
			name:    "valid store",
			store:   newMockInternalSecretStore(),
			wantErr: false,
		},
		{
			name:    "nil store",
			store:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher, err := NewInternalSecretsFetcher(tt.store)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewInternalSecretsFetcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && fetcher == nil {
				t.Error("NewInternalSecretsFetcher() returned nil fetcher")
			}
			if tt.wantErr && fetcher != nil {
				t.Error("NewInternalSecretsFetcher() should return nil fetcher on error")
			}
		})
	}
}

// TestInternalSecretsFetcher_GetSecret tests retrieving secrets.
func TestInternalSecretsFetcher_GetSecret(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setupFunc func(*mockInternalSecretStore)
		path      string
		wantErr   bool
		wantValue string
	}{
		{
			name: "successful retrieval",
			setupFunc: func(m *mockInternalSecretStore) {
				m.secrets["cache/redis/password"] = []byte("secret123")
			},
			path:      "cache/redis/password",
			wantErr:   false,
			wantValue: "secret123",
		},
		{
			name:      "secret not found",
			setupFunc: func(m *mockInternalSecretStore) {},
			path:      "cache/redis/password",
			wantErr:   true,
		},
		{
			name:      "empty path",
			setupFunc: func(m *mockInternalSecretStore) {},
			path:      "",
			wantErr:   true,
		},
		{
			name: "store returns error",
			setupFunc: func(m *mockInternalSecretStore) {
				m.getErr = errors.New("storage error")
			},
			path:    "cache/redis/password",
			wantErr: true,
		},
		{
			name: "empty secret value",
			setupFunc: func(m *mockInternalSecretStore) {
				m.secrets["cache/redis/password"] = []byte("")
			},
			path:    "cache/redis/password",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockInternalSecretStore()
			tt.setupFunc(store)

			fetcher, err := NewInternalSecretsFetcher(store)
			if err != nil {
				t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
			}

			value, err := fetcher.GetSecret(ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if string(value) != tt.wantValue {
					t.Errorf("GetSecret() = %q, want %q", string(value), tt.wantValue)
				}
			}
		})
	}
}

// TestInternalSecretsFetcher_GetSecret_NilContext tests GetSecret with nil context.
func TestInternalSecretsFetcher_GetSecret_NilContext(t *testing.T) {
	store := newMockInternalSecretStore()
	store.secrets["test/path"] = []byte("test-value")

	fetcher, err := NewInternalSecretsFetcher(store)
	if err != nil {
		t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
	}

	// Should handle nil context gracefully by using Background()
	value, err := fetcher.GetSecret(nil, "test/path")
	if err != nil {
		t.Errorf("GetSecret() with nil context failed: %v", err)
	}

	if string(value) != "test-value" {
		t.Errorf("GetSecret() = %q, want %q", string(value), "test-value")
	}
}

// TestInternalSecretsFetcher_Store tests storing secrets.
func TestInternalSecretsFetcher_Store(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setupFunc func(*mockInternalSecretStore)
		path      string
		value     []byte
		wantErr   bool
	}{
		{
			name:      "successful store",
			setupFunc: func(m *mockInternalSecretStore) {},
			path:      "cache/redis/password",
			value:     []byte("newsecret"),
			wantErr:   false,
		},
		{
			name:      "empty path",
			setupFunc: func(m *mockInternalSecretStore) {},
			path:      "",
			value:     []byte("newsecret"),
			wantErr:   true,
		},
		{
			name:      "empty value",
			setupFunc: func(m *mockInternalSecretStore) {},
			path:      "cache/redis/password",
			value:     []byte(""),
			wantErr:   true,
		},
		{
			name:      "nil value",
			setupFunc: func(m *mockInternalSecretStore) {},
			path:      "cache/redis/password",
			value:     nil,
			wantErr:   true,
		},
		{
			name: "store returns error",
			setupFunc: func(m *mockInternalSecretStore) {
				m.storeErr = errors.New("storage error")
			},
			path:    "cache/redis/password",
			value:   []byte("newsecret"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockInternalSecretStore()
			tt.setupFunc(store)

			fetcher, err := NewInternalSecretsFetcher(store)
			if err != nil {
				t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
			}

			err = fetcher.Store(ctx, tt.path, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Store() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify the secret was stored
				stored, exists := store.secrets[tt.path]
				if !exists {
					t.Error("Store() did not store the secret")
				} else if string(stored) != string(tt.value) {
					t.Errorf("Store() stored %q, want %q", string(stored), string(tt.value))
				}
			}
		})
	}
}

// TestInternalSecretsFetcher_Store_NilContext tests Store with nil context.
func TestInternalSecretsFetcher_Store_NilContext(t *testing.T) {
	store := newMockInternalSecretStore()

	fetcher, err := NewInternalSecretsFetcher(store)
	if err != nil {
		t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
	}

	// Should handle nil context gracefully by using Background()
	err = fetcher.Store(nil, "test/path", []byte("test-value"))
	if err != nil {
		t.Errorf("Store() with nil context failed: %v", err)
	}

	// Verify the secret was stored
	value, exists := store.secrets["test/path"]
	if !exists {
		t.Error("Store() did not store the secret")
	} else if string(value) != "test-value" {
		t.Errorf("Store() stored %q, want %q", string(value), "test-value")
	}
}

// TestInternalSecretsFetcher_Exists tests checking if secrets exist.
func TestInternalSecretsFetcher_Exists(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupFunc  func(*mockInternalSecretStore)
		path       string
		wantExists bool
		wantErr    bool
	}{
		{
			name: "secret exists",
			setupFunc: func(m *mockInternalSecretStore) {
				m.secrets["cache/redis/password"] = []byte("secret")
			},
			path:       "cache/redis/password",
			wantExists: true,
			wantErr:    false,
		},
		{
			name:       "secret does not exist",
			setupFunc:  func(m *mockInternalSecretStore) {},
			path:       "cache/redis/password",
			wantExists: false,
			wantErr:    false,
		},
		{
			name:       "empty path",
			setupFunc:  func(m *mockInternalSecretStore) {},
			path:       "",
			wantExists: false,
			wantErr:    true,
		},
		{
			name: "store returns error",
			setupFunc: func(m *mockInternalSecretStore) {
				m.existsErr = errors.New("storage error")
			},
			path:       "cache/redis/password",
			wantExists: false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockInternalSecretStore()
			tt.setupFunc(store)

			fetcher, err := NewInternalSecretsFetcher(store)
			if err != nil {
				t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
			}

			exists, err := fetcher.Exists(ctx, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Exists() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if exists != tt.wantExists {
				t.Errorf("Exists() = %v, want %v", exists, tt.wantExists)
			}
		})
	}
}

// TestInternalSecretsFetcher_Exists_NilContext tests Exists with nil context.
func TestInternalSecretsFetcher_Exists_NilContext(t *testing.T) {
	store := newMockInternalSecretStore()
	store.secrets["test/path"] = []byte("test-value")

	fetcher, err := NewInternalSecretsFetcher(store)
	if err != nil {
		t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
	}

	// Should handle nil context gracefully by using Background()
	exists, err := fetcher.Exists(nil, "test/path")
	if err != nil {
		t.Errorf("Exists() with nil context failed: %v", err)
	}

	if !exists {
		t.Error("Exists() should return true for existing secret")
	}
}

// TestInternalSecretsFetcher_Integration tests the full workflow.
func TestInternalSecretsFetcher_Integration(t *testing.T) {
	ctx := context.Background()
	store := newMockInternalSecretStore()

	fetcher, err := NewInternalSecretsFetcher(store)
	if err != nil {
		t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
	}

	// Test storing a secret
	path := "cache/redis/password"
	password := []byte("my-secure-password")

	err = fetcher.Store(ctx, path, password)
	if err != nil {
		t.Fatalf("Store() failed: %v", err)
	}

	// Test checking existence
	exists, err := fetcher.Exists(ctx, path)
	if err != nil {
		t.Fatalf("Exists() failed: %v", err)
	}
	if !exists {
		t.Error("Exists() should return true for stored secret")
	}

	// Test retrieving the secret
	retrieved, err := fetcher.GetSecret(ctx, path)
	if err != nil {
		t.Fatalf("GetSecret() failed: %v", err)
	}
	if string(retrieved) != string(password) {
		t.Errorf("GetSecret() = %q, want %q", string(retrieved), string(password))
	}

	// Test updating the secret
	newPassword := []byte("new-secure-password")
	err = fetcher.Store(ctx, path, newPassword)
	if err != nil {
		t.Fatalf("Store() (update) failed: %v", err)
	}

	// Verify updated value
	retrieved, err = fetcher.GetSecret(ctx, path)
	if err != nil {
		t.Fatalf("GetSecret() (after update) failed: %v", err)
	}
	if string(retrieved) != string(newPassword) {
		t.Errorf("GetSecret() = %q, want %q", string(retrieved), string(newPassword))
	}
}

// TestInternalSecretsFetcher_ImplementsInterface tests that InternalSecretsFetcher
// properly implements the SecretsFetcher interface.
func TestInternalSecretsFetcher_ImplementsInterface(t *testing.T) {
	store := newMockInternalSecretStore()
	fetcher, err := NewInternalSecretsFetcher(store)
	if err != nil {
		t.Fatalf("NewInternalSecretsFetcher() failed: %v", err)
	}

	// Verify it implements SecretsFetcher
	var _ SecretsFetcher = fetcher
}
