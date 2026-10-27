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

package credentials

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"

	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
)

var (
	// ErrMemoryStoreNil is returned by NewMemory when no credential store is
	// given.
	ErrMemoryStoreNil = stderrors.New("memory credential store is nil")

	// ErrMemoryNameEmpty is returned by NewMemory when the credential name is
	// empty.
	ErrMemoryNameEmpty = stderrors.New("memory credential name is empty")

	// ErrMemoryDestroyed is returned by WithBytes once the Memory has been
	// destroyed.
	ErrMemoryDestroyed = stderrors.New("memory is destroyed")
)

// MemoryRetrieveError reports that the credential store could not supply the
// named credential, for example because it was never provisioned.
type MemoryRetrieveError struct {
	Name string
	Err  error
}

// Error implements the error interface.
func (e *MemoryRetrieveError) Error() string {
	return fmt.Sprintf("retrieve credential %q: %v", e.Name, e.Err)
}

// Unwrap returns the store's error.
func (e *MemoryRetrieveError) Unwrap() error {
	return e.Err
}

// CredentialStore is the qrdb credential support a Memory draws its bytes
// from. qrdbsdk.VersionedCredentialService satisfies it.
type CredentialStore interface {
	RetrieveCredentialBuffer(ctx context.Context, name string) (*qrdbsecure.Buffer, error)
}

// Memory holds one named credential from qrdb credential support in
// protected memory for its whole life. It is the single go-quicbaas surface
// that holds such key bytes: callers borrow them with WithBytes and never
// receive a copy. Destroy zeroizes and releases them. A Memory is safe for
// concurrent use.
type Memory struct {
	mu  sync.RWMutex
	buf *qrdbsecure.Buffer
}

// NewMemory retrieves the named credential from store into a new Memory.
// It blocks until the store can supply the credential or ctx is done.
func NewMemory(ctx context.Context, store CredentialStore, name string) (*Memory, error) {
	if store == nil {
		return nil, ErrMemoryStoreNil
	}
	if name == "" {
		return nil, ErrMemoryNameEmpty
	}
	buf, err := store.RetrieveCredentialBuffer(ctx, name)
	if err != nil {
		return nil, &MemoryRetrieveError{Name: name, Err: err}
	}
	return &Memory{buf: buf}, nil
}

// Size returns the length of the held bytes, or 0 once m is destroyed.
func (m *Memory) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.buf == nil {
		return 0
	}
	return m.buf.Size()
}

// IsAlive reports whether m still holds its bytes.
func (m *Memory) IsAlive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buf != nil && m.buf.IsAlive()
}

// WithBytes lends the held bytes to fn for the duration of the call. fn must
// not retain the slice. Destroy waits for a running WithBytes to return.
func (m *Memory) WithBytes(fn func([]byte) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.buf == nil || !m.buf.IsAlive() {
		return ErrMemoryDestroyed
	}
	return m.buf.WithBytes(fn)
}

// Destroy zeroizes and releases the held bytes. It is idempotent.
func (m *Memory) Destroy() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buf != nil {
		m.buf.Destroy()
		m.buf = nil
	}
}
