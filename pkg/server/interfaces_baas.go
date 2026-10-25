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

package server

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// BaasBarrierService is the barrier lifecycle service interface used by the
// BaaS composition layer to delegate barrier operations (initialize, seal,
// unseal, status, rekey, recovery, root token). It is satisfied by
// qrdbsdk.BarrierServicer (a re-export of go-qrdb pkg/service.BarrierServicer).
type BaasBarrierService = qrdbsdk.BarrierServicer

// BaasTenantService is the multi-tenant lifecycle service interface used by
// the BaaS composition layer for per-tenant barrier delegation. It is satisfied
// by qrdbsdk.ServiceTenantServicer (a re-export of pkg/tenant.TenantServicer).
type BaasTenantService = qrdbsdk.ServiceTenantServicer

// BasCeremonyService is the Shamir initialization ceremony service interface
// used by the BaaS composition layer. It is satisfied by
// qrdbsdk.ServiceCeremonyServicer.
type BasCeremonyService = qrdbsdk.ServiceCeremonyServicer

// BaasCredentialService is the named-credential management service interface
// used by the BaaS composition layer. It is satisfied by
// qrdbsdk.ServiceCredentialServicer.
type BaasCredentialService = qrdbsdk.ServiceCredentialServicer

// BaasLockService defines the distributed lock management interface used by
// the BaaS composition layer. It mirrors the qrdb LockServicer interface to
// provide blocking and non-blocking lock operations with fencing tokens.
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
