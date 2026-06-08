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

// Package auth provides tests for persistent authentication manager
package auth

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// authMockStorage is a local mock storage implementation to avoid import cycles.
// This is a simplified version of testutil.MockStorage without external dependencies.
type authMockStorage struct {
	mu            sync.RWMutex
	data          map[string][]byte
	closed        bool
	errorInjector map[string]error
}

func newAuthMockStorage() *authMockStorage {
	return &authMockStorage{
		data:          make(map[string][]byte),
		errorInjector: make(map[string]error),
	}
}

// InjectError allows injecting an error for the specified operation (Get, Set, Delete).
func (m *authMockStorage) InjectError(operation string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorInjector[operation] = err
}

func (m *authMockStorage) checkError(operation string) error {
	if err, exists := m.errorInjector[operation]; exists {
		delete(m.errorInjector, operation) // One-time injection
		return err
	}
	return nil
}

func (m *authMockStorage) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, errors.Newf(errors.ErrStorage, "authMockStorage.Get", "storage is closed")
	}

	if err := m.checkError("Get"); err != nil {
		return nil, err
	}

	value, exists := m.data[string(key)]
	if !exists {
		return nil, errors.Newf(errors.ErrNotFound, "authMockStorage.Get", "key not found")
	}

	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

func (m *authMockStorage) Set(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Newf(errors.ErrStorage, "authMockStorage.Set", "storage is closed")
	}

	if err := m.checkError("Set"); err != nil {
		return err
	}

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	m.data[string(key)] = valueCopy
	return nil
}

func (m *authMockStorage) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.Newf(errors.ErrStorage, "authMockStorage.Delete", "storage is closed")
	}

	if err := m.checkError("Delete"); err != nil {
		return err
	}

	delete(m.data, string(key))
	return nil
}

func (m *authMockStorage) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	return m.Get(key)
}

func (m *authMockStorage) SetWithContext(ctx context.Context, key, value []byte) error {
	return m.Set(key, value)
}

func (m *authMockStorage) Compact(start, end []byte) error {
	return nil // No-op for mock
}

func (m *authMockStorage) Flush() error {
	return nil // No-op for mock
}

func (m *authMockStorage) Batch() storage.Batch {
	return &authMockBatch{storage: m, mutations: make([]authBatchMutation, 0)}
}

func (m *authMockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}

	return &authMockIterator{
		storage: m,
		keys:    keys,
		index:   -1,
	}
}

func (m *authMockStorage) Snapshot() (storage.Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, errors.Newf(errors.ErrStorage, "authMockStorage.Snapshot", "storage is closed")
	}

	snapshot := make(map[string][]byte, len(m.data))
	for k, v := range m.data {
		valueCopy := make([]byte, len(v))
		copy(valueCopy, v)
		snapshot[k] = valueCopy
	}

	return &authMockSnapshot{data: snapshot}, nil
}

func (m *authMockStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.data = make(map[string][]byte)
	return nil
}

// authMockBatch is a simple batch implementation for tests.
type authMockBatch struct {
	storage   *authMockStorage
	mutations []authBatchMutation
	committed bool
}

type authBatchMutation struct {
	key    string
	value  []byte
	delete bool
}

func (b *authMockBatch) Set(key, value []byte) error {
	if b.committed {
		return errors.Newf(errors.ErrInvalidInput, "authMockBatch.Set", "batch already committed")
	}
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	b.mutations = append(b.mutations, authBatchMutation{key: string(key), value: valueCopy})
	return nil
}

func (b *authMockBatch) Delete(key []byte) error {
	if b.committed {
		return errors.Newf(errors.ErrInvalidInput, "authMockBatch.Delete", "batch already committed")
	}
	b.mutations = append(b.mutations, authBatchMutation{key: string(key), delete: true})
	return nil
}

func (b *authMockBatch) Commit() error {
	if b.committed {
		return errors.Newf(errors.ErrInvalidInput, "authMockBatch.Commit", "batch already committed")
	}
	b.storage.mu.Lock()
	defer b.storage.mu.Unlock()

	for _, m := range b.mutations {
		if m.delete {
			delete(b.storage.data, m.key)
		} else {
			b.storage.data[m.key] = m.value
		}
	}
	b.committed = true
	return nil
}

func (b *authMockBatch) Rollback() error {
	if b.committed {
		return errors.Newf(errors.ErrInvalidInput, "authMockBatch.Rollback", "batch already committed")
	}
	b.mutations = nil
	b.committed = true
	return nil
}

func (b *authMockBatch) Close() error {
	if !b.committed {
		return b.Rollback()
	}
	return nil
}

// authMockIterator is a simple iterator for tests.
type authMockIterator struct {
	storage *authMockStorage
	keys    []string
	index   int
	valid   bool
}

func (i *authMockIterator) SeekToFirst() bool {
	i.index = 0
	i.valid = i.index < len(i.keys)
	return i.valid
}

func (i *authMockIterator) SeekToLast() bool {
	i.index = len(i.keys) - 1
	i.valid = i.index >= 0
	return i.valid
}

func (i *authMockIterator) Seek(target []byte) bool {
	targetStr := string(target)
	for idx, k := range i.keys {
		if k >= targetStr {
			i.index = idx
			i.valid = true
			return true
		}
	}
	i.valid = false
	return false
}

func (i *authMockIterator) Next() bool {
	i.index++
	i.valid = i.index < len(i.keys)
	return i.valid
}

func (i *authMockIterator) Prev() bool {
	i.index--
	i.valid = i.index >= 0
	return i.valid
}

func (i *authMockIterator) Valid() bool {
	return i.valid && i.index >= 0 && i.index < len(i.keys)
}

func (i *authMockIterator) Key() []byte {
	if !i.Valid() {
		return nil
	}
	return []byte(i.keys[i.index])
}

func (i *authMockIterator) Value() []byte {
	if !i.Valid() {
		return nil
	}
	i.storage.mu.RLock()
	defer i.storage.mu.RUnlock()
	value := i.storage.data[i.keys[i.index]]
	result := make([]byte, len(value))
	copy(result, value)
	return result
}

func (i *authMockIterator) Error() error { return nil }
func (i *authMockIterator) Close() error {
	i.keys = nil
	i.valid = false
	return nil
}

// authMockSnapshot is a simple snapshot for tests.
type authMockSnapshot struct {
	data map[string][]byte
}

func (s *authMockSnapshot) Get(key []byte) ([]byte, error) {
	value, exists := s.data[string(key)]
	if !exists {
		return nil, errors.Newf(errors.ErrNotFound, "authMockSnapshot.Get", "key not found")
	}
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

func (s *authMockSnapshot) NewIterator(opts *storage.IteratorOptions) storage.Iterator {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return &authMockIterator{
		storage: &authMockStorage{data: s.data},
		keys:    keys,
		index:   -1,
	}
}

func (s *authMockSnapshot) Close() error {
	s.data = nil
	return nil
}

// Helper function to create a test logger.
func createPersistentTestLogger(t *testing.T) *logger.Logger {
	config := logger.LogConfig{
		Level:     "error", // Reduce noise in tests
		Format:    "json",
		Output:    "stdout",
		Component: "persistent-auth-test",
	}

	log, err := logger.New(config)
	require.NoError(t, err)
	return log
}

// TestNewPersistentAuthManager tests the constructor.
func TestNewPersistentAuthManager(t *testing.T) {
	// Valid test case
	t.Run("valid config with defaults", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		storage := newAuthMockStorage()
		log := createPersistentTestLogger(t)

		pam, err := NewPersistentAuthManager(config, storage, log)
		assert.NoError(t, err)
		assert.NotNil(t, pam)
		if pam != nil {
			pam.Close()
		}
	})

	// Error test cases
	t.Run("nil config", func(t *testing.T) {
		storage := newAuthMockStorage()
		log := createPersistentTestLogger(t)

		pam, err := NewPersistentAuthManager(nil, storage, log)
		assert.Error(t, err)
		assert.Nil(t, pam)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("nil storage", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		log := createPersistentTestLogger(t)

		pam, err := NewPersistentAuthManager(config, nil, log)
		assert.Error(t, err)
		assert.Nil(t, pam)
		assert.Contains(t, err.Error(), "storage cannot be nil")
	})

	t.Run("nil logger", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		storage := newAuthMockStorage()

		pam, err := NewPersistentAuthManager(config, storage, nil)
		assert.Error(t, err)
		assert.Nil(t, pam)
		assert.Contains(t, err.Error(), "logger cannot be nil")
	})

	t.Run("missing JWT secret when enabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "",
		}
		storage := newAuthMockStorage()
		log := createPersistentTestLogger(t)

		pam, err := NewPersistentAuthManager(config, storage, log)
		assert.Error(t, err)
		assert.Nil(t, pam)
		assert.Contains(t, err.Error(), "JWT secret or signing key ID is required")
	})

	t.Run("auth disabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled: false,
		}
		storage := newAuthMockStorage()
		log := createPersistentTestLogger(t)

		pam, err := NewPersistentAuthManager(config, storage, log)
		assert.NoError(t, err)
		assert.NotNil(t, pam)
		if pam != nil {
			pam.Close()
		}
	})
}

// TestPersistentAuthorizeDisabled tests authorization when disabled.
func TestPersistentAuthorizeDisabled(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled: false,
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	ctx := context.Background()
	err = pam.Authorize(ctx, "", interfaces.ResourceKeys, interfaces.ActionRead)
	assert.NoError(t, err) // Should allow all when disabled
}

// TestPersistentRefreshToken moved to persistent_manager_integration_test.go

// TestPersistentRevokeToken tests the RevokeToken method.
func TestPersistentRevokeToken(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Hour,
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user and get token via generateTokens (password auth is disabled)
	user, err := pam.CreateUser("testuser", []string{"readwrite"})
	require.NoError(t, err)

	// Install an allow decision point so a valid token authorizes; the
	// revocation check runs before the decision point.
	pam.SetQRDBDecisionPoint(&recordingDecisionPoint{ret: nil})

	ctx := context.Background()
	token, err := pam.generateTokens(user)
	require.NoError(t, err)

	err = pam.Authorize(ctx, token.AccessToken, interfaces.ResourceKeys, interfaces.ActionRead)
	assert.NoError(t, err)

	err = pam.RevokeToken(ctx, token.AccessToken)
	assert.NoError(t, err)

	err = pam.Authorize(ctx, token.AccessToken, interfaces.ResourceKeys, interfaces.ActionRead)
	assert.Error(t, err)
}

// TestPersistentCreateUser tests the CreateUser method.
func TestPersistentCreateUser(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	tests := []struct {
		name        string
		username    string
		roles       []string
		expectError bool
	}{
		{
			name:        "valid user",
			username:    "user1",
			roles:       []string{"readwrite"},
			expectError: false,
		},
		{
			name:        "empty username",
			username:    "",
			roles:       []string{"readwrite"},
			expectError: true,
		},
		{
			name:        "duplicate username",
			username:    "user1",
			roles:       []string{"readonly"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := pam.CreateUser(tt.username, tt.roles)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, user)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, user)
				assert.Equal(t, tt.username, user.Username)
			}
		})
	}
}

// TestPersistentDeleteUser tests the DeleteUser method.
func TestPersistentDeleteUser(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user, err := pam.CreateUser("testuser", []string{"readwrite"})
	require.NoError(t, err)

	err = pam.DeleteUser(user.ID)
	assert.NoError(t, err)

	foundUser := pam.userStore.GetByID(user.ID)
	assert.Nil(t, foundUser)
}

// TestPersistentUpdateUserRoles tests the UpdateUserRoles method.
func TestPersistentUpdateUserRoles(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user, err := pam.CreateUser("testuser", []string{"readonly"})
	require.NoError(t, err)

	newRoles := []string{"admin"}
	err = pam.UpdateUserRoles(user.ID, newRoles)
	assert.NoError(t, err)

	updatedUser := pam.userStore.GetByID(user.ID)
	assert.Equal(t, newRoles, updatedUser.Roles)
}

// TestPersistentClose tests the Close method.
func TestPersistentClose(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)

	err = pam.Close()
	assert.NoError(t, err)
}

// TestPersistentGetters tests the getter methods.
func TestPersistentGetters(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	userStore := pam.GetUserStore()
	assert.NotNil(t, userStore)

	tokenStore := pam.GetTokenStore()
	assert.NotNil(t, tokenStore)
}

// TestPersistentConcurrentAccess tests concurrent access.
func TestPersistentConcurrentAccess(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Hour,
	}

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Install an allow decision point so issued tokens authorize.
	pam.SetQRDBDecisionPoint(&recordingDecisionPoint{ret: nil})

	// Create users and store their references for token generation
	users := make([]*User, 5)
	for i := 0; i < 5; i++ {
		username := fmt.Sprintf("user%d", i)
		user, err := pam.CreateUser(username, []string{"readwrite"})
		require.NoError(t, err)
		users[i] = user
	}

	ctx := context.Background()
	done := make(chan bool, 5)

	for i := 0; i < 5; i++ {
		go func(id int) {
			// Generate tokens directly (password auth is disabled)
			token, err := pam.generateTokens(users[id])
			assert.NoError(t, err)
			assert.NotNil(t, token)

			err = pam.Authorize(ctx, token.AccessToken, interfaces.ResourceKeys, interfaces.ActionRead)
			assert.NoError(t, err)

			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}
}
