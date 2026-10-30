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

// Package interfaces provides core interfaces for QuicBaaS components.
package interfaces

import "context"

// KVStore is a context-aware key-value store interface for service-layer
// and DAO operations. Implementations route through the Raft consensus
// layer for strong consistency, or read from local replicas for
// eventual consistency.
type KVStore interface {
	Put(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error
	List(ctx context.Context, prefix string) ([]string, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// GuardedKVStore is a KVStore that also performs the three guarded writes,
// PutIfAbsent, CompareAndSwap and CompareAndDelete, whose outcomes are
// defined under concurrency.
//
// It is the counterpart of go-qrdb's sdk.AtomicKVStore over the six-verb
// KVStore. go-qrdb's interface embeds its twelve-method KVStore, secondary
// indexes included, which the server's stores do not carry, so asserting it
// refuses a store that can do the guarded work.
//
// Discover it by type assertion and fail closed when it is absent; a read
// followed by a write is never a substitute. The three verbs are one
// capability, as go-qrdb defines them: a store that offers a subset does not
// satisfy it.
type GuardedKVStore interface {
	KVStore
	PutIfAbsent(ctx context.Context, key string, value []byte) (stored bool, err error)
	CompareAndSwap(ctx context.Context, key string, expected, next []byte) (swapped bool, err error)
	CompareAndDelete(ctx context.Context, key string, expected []byte) (deleted bool, err error)
}
