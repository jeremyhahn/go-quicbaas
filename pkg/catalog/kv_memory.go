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

package catalog

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

// errMemoryKVClosed is the cause of every error a closed memory KV returns.
var errMemoryKVClosed = errors.New("memory kv is closed")

// memoryKV is an in-process KV over a map.
type memoryKV struct {
	mu     sync.RWMutex
	data   map[string][]byte
	closed bool
}

// NewMemoryKV returns an in-process KV for tests and tools. Nothing it holds
// is durable: the data lives in a map and is gone when the process exits.
// The server's catalog uses NewQRDBKV.
func NewMemoryKV() KV {
	return &memoryKV{data: make(map[string][]byte)}
}

func (m *memoryKV) Get(ctx context.Context, key []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, NewStorageError("get", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, NewStorageError("get", errMemoryKVClosed)
	}
	v, ok := m.data[string(key)]
	if !ok {
		return nil, &KVKeyNotFoundError{Key: string(key)}
	}
	return cloneBytes(v), nil
}

func (m *memoryKV) Scan(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	if err := ctx.Err(); err != nil {
		return NewStorageError("scan", err)
	}
	type kv struct {
		key   string
		value []byte
	}
	p := string(prefix)

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return NewStorageError("scan", errMemoryKVClosed)
	}
	var matches []kv
	for k, v := range m.data {
		if strings.HasPrefix(k, p) {
			matches = append(matches, kv{key: k, value: cloneBytes(v)})
		}
	}
	m.mu.RUnlock()

	sort.Slice(matches, func(i, j int) bool { return matches[i].key < matches[j].key })
	for _, e := range matches {
		if err := ctx.Err(); err != nil {
			return NewStorageError("scan", err)
		}
		if !fn([]byte(e.key), e.value) {
			break
		}
	}
	return nil
}

func (m *memoryKV) Apply(ctx context.Context, batch *KVBatch) error {
	if err := ctx.Err(); err != nil {
		return NewStorageError("apply", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return NewStorageError("apply", errMemoryKVClosed)
	}
	if batch == nil {
		return nil
	}
	for _, op := range batch.Ops() {
		if op.Delete {
			delete(m.data, string(op.Key))
			continue
		}
		m.data[string(op.Key)] = cloneBytes(op.Value)
	}
	return nil
}

func (m *memoryKV) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.data = nil
	return nil
}
