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

package lease

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// faultKV is a FAULT INJECTOR, not a mock of the store: it delegates every
// verb to a real qrdb memory engine and fails only the operation a test has
// explicitly armed. The distinction matters — a hand-written in-memory map
// would be a second storage implementation to keep in sync, while this
// exercises the same engine production uses and adds exactly one behaviour
// (the fault) that no real store can be made to produce on demand.
type faultKV struct {
	inner interfaces.KVStore

	putErr    error
	getErr    error
	deleteErr error
	scanErr   error
}

// newFaultKV returns an injector over a fresh qrdb memory engine.
func newFaultKV() *faultKV {
	return &faultKV{inner: kvstore.NewMemory()}
}

var _ interfaces.KVStore = (*faultKV)(nil)

func (f *faultKV) Put(ctx context.Context, key string, value []byte) error {
	if f.putErr != nil {
		return f.putErr
	}
	return f.inner.Put(ctx, key, value)
}

func (f *faultKV) Get(ctx context.Context, key string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.inner.Get(ctx, key)
}

func (f *faultKV) Delete(ctx context.Context, key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.inner.Delete(ctx, key)
}

func (f *faultKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	return f.inner.Scan(ctx, prefix, fn)
}

func (f *faultKV) List(ctx context.Context, prefix string) ([]string, error) {
	return f.inner.List(ctx, prefix)
}

func (f *faultKV) Exists(ctx context.Context, key string) (bool, error) {
	return f.inner.Exists(ctx, key)
}
