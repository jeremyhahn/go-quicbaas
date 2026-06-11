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

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// platformRoleClientResolver resolves the qrdb RoleClient for the platform
// (IAM) shard. It is the wiring-layer implementation of iam.RoleClientResolver:
// the iam package owns the role-storage contract but must not depend on the
// server's ShardedClient, so the server hands it this narrow resolver instead.
// Every role mutation the IdentityStore performs is therefore routed to the
// SAME qrdb role store the policy decision point reads — the single role ID
// space that Stage 1.2 establishes.
type platformRoleClientResolver struct {
	shards *ShardedClient
}

// newPlatformRoleClientResolver builds the resolver from the server's
// ShardedClient. The client is resolved lazily on each call so the resolver
// tolerates the ShardedClient being replaced (e.g. node-backed swap) after
// IAM wiring.
func newPlatformRoleClientResolver(shards *ShardedClient) *platformRoleClientResolver {
	return &platformRoleClientResolver{shards: shards}
}

// RoleClient returns the qrdb RoleClient for the platform shard. The platform
// shard is the canonical IAM shard (qbshards.PlatformShardID); roles are a
// platform-control-plane concept and are not sharded per tenant.
func (r *platformRoleClientResolver) RoleClient(_ context.Context) (transport.RoleClient, error) {
	if r == nil || r.shards == nil {
		return nil, &QRDBClientNotInitializedError{}
	}
	client, err := r.shards.ForShard(qbshards.PlatformShardID)
	if err != nil {
		return nil, &ShardClientError{ShardID: qbshards.PlatformShardID, Cause: err}
	}
	return client.Roles(), nil
}

// Compile-time interface check.
var _ iam.RoleClientResolver = (*platformRoleClientResolver)(nil)
