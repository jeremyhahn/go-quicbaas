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

package registry

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// clientBackend adapts a qrdb SDK client to the registry Backend, mapping each
// registry primitive onto the qrdb Lease / MVCC / KV / Watch SDK surface.
type clientBackend struct {
	client *qrdbsdk.Client
}

// NewClientBackend returns a Backend backed by a qrdb SDK client.
func NewClientBackend(client *qrdbsdk.Client) Backend {
	return &clientBackend{client: client}
}

// NewFromClient is a convenience constructor returning a Registry backed by a
// qrdb SDK client. The registry is multi-tenant: each call scopes to the
// tenant carried in the request context.
func NewFromClient(client *qrdbsdk.Client) *Registry {
	return New(NewClientBackend(client))
}

func (b *clientBackend) Grant(ctx context.Context, ttlSeconds int64) (int64, error) {
	info, err := b.client.Lease().LeaseGrant(ctx, ttlSeconds)
	if err != nil {
		return 0, err
	}
	return info.ID, nil
}

func (b *clientBackend) KeepAlive(ctx context.Context, leaseID int64) error {
	_, err := b.client.Lease().LeaseKeepAlive(ctx, leaseID)
	return err
}

func (b *clientBackend) Revoke(ctx context.Context, leaseID int64) error {
	return b.client.Lease().LeaseRevoke(ctx, leaseID)
}

func (b *clientBackend) PutWithLease(ctx context.Context, key string, value []byte, leaseID int64) error {
	_, err := b.client.MVCC().PutWithLease(ctx, key, value, leaseID)
	return err
}

func (b *clientBackend) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	return b.client.KV().Scan(ctx, prefix)
}

func (b *clientBackend) WatchPrefix(ctx context.Context, prefix string) (<-chan qrdbtransport.WatchResponse, error) {
	// PrevKV lets the registry recover the instance on lease-expiry deletes.
	return b.client.Watch().WatchPrefix(ctx, prefix, qrdbtransport.WatchOptions{PrevKV: true})
}
