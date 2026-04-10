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

package backup

import (
	"context"
	"io"
	"sync"
)

// MockStorageWithErrors extends MockStorage with error injection for testing.
type MockStorageWithErrors struct {
	*MockStorage
	mu sync.RWMutex

	// Error injection fields
	putError      error
	putErrorAfter int
	putCount      int
	getError      error
	deleteError   error
	listError     error
	closeError    error
}

// NewMockStorageWithErrors creates a MockStorage that supports error injection.
func NewMockStorageWithErrors() *MockStorageWithErrors {
	return &MockStorageWithErrors{
		MockStorage: NewMockStorage(),
	}
}

// PutWithContext stores data with error injection support.
func (s *MockStorageWithErrors) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.putCount++

	// Check if we should return an error after N successful puts
	if s.putErrorAfter > 0 && s.putCount > s.putErrorAfter {
		return s.putError
	}

	// Check if we should return an error immediately
	if s.putErrorAfter == 0 && s.putError != nil {
		return s.putError
	}

	// Call parent implementation
	return s.MockStorage.PutWithContext(ctx, key, data)
}

// GetWithContext retrieves data with error injection support.
func (s *MockStorageWithErrors) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.getError != nil {
		return nil, s.getError
	}

	return s.MockStorage.GetWithContext(ctx, key)
}

// DeleteWithContext removes data with error injection support.
func (s *MockStorageWithErrors) DeleteWithContext(ctx context.Context, key string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.deleteError != nil {
		return s.deleteError
	}

	return s.MockStorage.DeleteWithContext(ctx, key)
}

// ListWithContext returns keys with error injection support.
func (s *MockStorageWithErrors) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.listError != nil {
		return nil, s.listError
	}

	return s.MockStorage.ListWithContext(ctx, prefix)
}

// Close closes the storage with error injection support.
func (s *MockStorageWithErrors) Close() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closeError != nil {
		return s.closeError
	}

	return s.MockStorage.Close()
}

// SetPutError configures put error injection.
func (s *MockStorageWithErrors) SetPutError(err error, afterCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putError = err
	s.putErrorAfter = afterCount
	s.putCount = 0
}

// SetGetError configures get error injection.
func (s *MockStorageWithErrors) SetGetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getError = err
}

// SetDeleteError configures delete error injection.
func (s *MockStorageWithErrors) SetDeleteError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteError = err
}

// SetListError configures list error injection.
func (s *MockStorageWithErrors) SetListError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listError = err
}

// SetCloseError configures close error injection.
func (s *MockStorageWithErrors) SetCloseError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeError = err
}
