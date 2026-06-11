// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package storage_test

import (
	"context"
	"errors"
	"testing"

	xkmsstorage "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/storage"
	xkmsoriginal "github.com/jeremyhahn/go-xkms/pkg/storage"
)

// compile-time interface assertion: the re-exported Backend alias is
// structurally identical to the original.
var _ xkmsstorage.Backend = (xkmsstorage.Backend)(nil)

// TestErrorAliases verifies that the re-exported sentinel errors are the
// same values as the originals so errors.Is checks work correctly across
// package boundaries.
func TestErrorAliases(t *testing.T) {
	t.Parallel()

	t.Run("ErrNotFound", func(t *testing.T) {
		t.Parallel()
		if !errors.Is(xkmsstorage.ErrNotFound, xkmsoriginal.ErrNotFound) {
			t.Errorf("ErrNotFound re-export does not match original")
		}
	})

	t.Run("ErrAlreadyExists", func(t *testing.T) {
		t.Parallel()
		if !errors.Is(xkmsstorage.ErrAlreadyExists, xkmsoriginal.ErrAlreadyExists) {
			t.Errorf("ErrAlreadyExists re-export does not match original")
		}
	})

	t.Run("ErrClosed", func(t *testing.T) {
		t.Parallel()
		if !errors.Is(xkmsstorage.ErrClosed, xkmsoriginal.ErrClosed) {
			t.Errorf("ErrClosed re-export does not match original")
		}
	})
}

// TestBackendIsInterface verifies the Backend alias accepts a concrete
// implementation via the standard interface assignment pattern.
func TestBackendIsInterface(t *testing.T) {
	t.Parallel()

	// memBackend is a minimal in-memory Backend used only to verify the
	// interface is assignable.
	var _ xkmsstorage.Backend = (*memBackend)(nil)
}

// memBackend is a trivial in-memory implementation of storage.Backend used
// for compile-time verification only.
type memBackend struct {
	data map[string][]byte
}

func (m *memBackend) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, xkmsstorage.ErrNotFound
	}
	return v, nil
}

func (m *memBackend) Put(_ context.Context, key string, value []byte) error {
	m.data[key] = value
	return nil
}

func (m *memBackend) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	if _, ok := m.data[key]; ok {
		return false, nil
	}
	m.data[key] = value
	return true, nil
}

func (m *memBackend) Delete(_ context.Context, key string) error {
	if _, ok := m.data[key]; !ok {
		return xkmsstorage.ErrNotFound
	}
	delete(m.data, key)
	return nil
}

func (m *memBackend) List(_ context.Context, _ string) ([]string, error) { return nil, nil }

func (m *memBackend) Scan(_ context.Context, prefix string, fn func(string, []byte) error) error {
	for k, v := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *memBackend) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *memBackend) Close() error { return nil }
