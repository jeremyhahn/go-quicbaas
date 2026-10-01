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

func (b *clientBackend) KeepAlive(ctx context.Context, leaseID int64) (int64, error) {
	info, err := b.client.Lease().LeaseKeepAlive(ctx, leaseID)
	if err != nil {
		return 0, err
	}
	return info.TTL, nil
}

func (b *clientBackend) Revoke(ctx context.Context, leaseID int64) error {
	return b.client.Lease().LeaseRevoke(ctx, leaseID)
}

func (b *clientBackend) PutWithLease(ctx context.Context, key string, value []byte, leaseID int64) error {
	_, err := b.client.MVCC().PutWithLease(ctx, key, value, leaseID)
	return err
}

func (b *clientBackend) Exists(ctx context.Context, key string) (bool, error) {
	return b.client.KV().Exists(ctx, key)
}

func (b *clientBackend) Range(ctx context.Context, prefix string) (map[string][]byte, int64, error) {
	resp, err := b.client.MVCC().Range(ctx, []byte(prefix), prefixEnd(prefix), qrdbtransport.RangeOptions{})
	if err != nil {
		return nil, 0, err
	}
	out := make(map[string][]byte, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		if kv == nil {
			continue
		}
		out[string(kv.Key)] = kv.Value
	}
	return out, resp.Revision, nil
}

func (b *clientBackend) WatchPrefix(ctx context.Context, prefix string, startRevision int64) (<-chan qrdbtransport.WatchResponse, error) {
	// PrevKV lets the registry recover the instance on lease-expiry deletes.
	return b.client.Watch().WatchPrefix(ctx, prefix, qrdbtransport.WatchOptions{
		StartRevision: startRevision,
		PrevKV:        true,
	})
}

// prefixEnd returns the exclusive upper bound of the key range sharing
// prefix: the prefix with its last byte below 0xff incremented and everything
// after it dropped. The registry's prefixes are ASCII and end in '/', so the
// all-0xff case never arises; it returns nil (an open range) for completeness.
func prefixEnd(prefix string) []byte {
	end := []byte(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}
