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

package server

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// BaasLockService defines the distributed lock management interface required
// by the BaaS embedded transport. It mirrors the qrdb LockServicer interface
// to provide blocking and non-blocking lock operations with fencing tokens.
type BaasLockService interface {
	// Lock acquires a distributed lock, blocking until acquired or the
	// context is cancelled.
	Lock(ctx context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*qrdbsdk.Lock, error)

	// TryLock attempts to acquire a lock immediately without blocking.
	TryLock(ctx context.Context, key, holderID string, ttl time.Duration, reentrant bool) (*qrdbsdk.Lock, error)

	// Unlock releases a held lock validated by fencing token.
	Unlock(ctx context.Context, key string, fencingToken int64) error

	// Extend refreshes the TTL on a held lock without releasing it.
	Extend(ctx context.Context, key string, fencingToken int64) (*qrdbsdk.Lock, error)

	// Status returns the current lock state for a key.
	Status(ctx context.Context, key string) (*qrdbsdk.Lock, error)

	// List returns all active locks, optionally filtered by key prefix.
	List(ctx context.Context, prefix string) ([]*qrdbsdk.Lock, error)
}

// ---------------------------------------------------------------------------
// transport.LockClient — delegates to BaasLockService when available
// ---------------------------------------------------------------------------

// LockAcquire acquires a distributed lock, blocking until acquired or the
// context is cancelled. Delegates to the injected BaasLockService.
func (t *baasTransport) LockAcquire(ctx context.Context, req *transport.LockAcquireRequest) (*transport.LockInfo, error) {
	if t.lockService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "LockAcquire"}
	}
	l, err := t.lockService.Lock(ctx, req.Key, req.HolderID, time.Duration(req.TTL)*time.Second, req.Reentrant)
	if err != nil {
		return nil, err
	}
	return mapLockToTransport(l), nil
}

// LockTryLock attempts to acquire the lock without blocking. Returns an
// error immediately when the lock is held by another owner.
func (t *baasTransport) LockTryLock(ctx context.Context, req *transport.LockAcquireRequest) (*transport.LockInfo, error) {
	if t.lockService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "LockTryLock"}
	}
	l, err := t.lockService.TryLock(ctx, req.Key, req.HolderID, time.Duration(req.TTL)*time.Second, req.Reentrant)
	if err != nil {
		return nil, err
	}
	return mapLockToTransport(l), nil
}

// LockRelease releases the distributed lock identified by key and fencing token.
func (t *baasTransport) LockRelease(ctx context.Context, req *transport.LockReleaseRequest) error {
	if t.lockService == nil {
		return &BaasTransportNotSupportedError{Operation: "LockRelease"}
	}
	return t.lockService.Unlock(ctx, req.Key, req.FencingToken)
}

// LockExtend refreshes the TTL of an existing lock via lease keepalive.
func (t *baasTransport) LockExtend(ctx context.Context, req *transport.LockExtendRequest) (*transport.LockInfo, error) {
	if t.lockService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "LockExtend"}
	}
	l, err := t.lockService.Extend(ctx, req.Key, req.FencingToken)
	if err != nil {
		return nil, err
	}
	return mapLockToTransport(l), nil
}

// LockStatus returns the current lock state for the given key, or nil
// when the key is not currently locked.
func (t *baasTransport) LockStatus(ctx context.Context, key string) (*transport.LockInfo, error) {
	if t.lockService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "LockStatus"}
	}
	l, err := t.lockService.Status(ctx, key)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, nil
	}
	return mapLockToTransport(l), nil
}

// LockList returns all active locks whose keys start with the given prefix.
// An empty prefix returns all active locks.
func (t *baasTransport) LockList(ctx context.Context, prefix string) (*transport.LockListResponse, error) {
	if t.lockService == nil {
		return nil, &BaasTransportNotSupportedError{Operation: "LockList"}
	}
	locks, err := t.lockService.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	infos := make([]*transport.LockInfo, 0, len(locks))
	for _, l := range locks {
		infos = append(infos, mapLockToTransport(l))
	}
	return &transport.LockListResponse{Locks: infos}, nil
}

// mapLockToTransport converts a qrdbsdk.Lock to a transport.LockInfo.
func mapLockToTransport(l *qrdbsdk.Lock) *transport.LockInfo {
	return &transport.LockInfo{
		Key:          l.Key,
		FencingToken: l.FencingToken,
		LeaseID:      l.LeaseID,
		HolderID:     l.HolderID,
		Reentrant:    l.Reentrant,
		AcquireCount: l.AcquireCount,
		TTL:          l.TTL,
		AcquiredAt:   l.AcquiredAt,
	}
}
