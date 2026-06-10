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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// =============================================================================
// Per-tenant barrier facade wiring
//
// QuicBaaS does not own per-tenant barriers — go-qrdb does. This builder
// constructs the broker-side TenantBarrierServicer facade from the qrdb SDK
// TenantClient bound to the platform shard, exactly as buildBarrierCeremonyServices
// builds the system-barrier facade from platformClient.Barrier(). The facade is
// shared by every transport (REST, gRPC, the qrdb-quic ALPN bridge, MCP) so all
// of them operate the single embedded-node per-tenant barrier surface.
//
// When the qrdb client is not yet available the facade is built with a nil
// TenantClient; callers gate on a non-nil facade before registering the
// per-tenant barrier endpoints.
// =============================================================================

// buildTenantBarrierService constructs the TenantBarrierServicer facade from
// the qrdb SDK client bound to the platform shard. Returns nil when the qrdb
// client is unavailable (typical pre-wire state), so callers can skip
// registering the per-tenant barrier surface until the embedded node is live.
func (s *Server) buildTenantBarrierService() service.TenantBarrierServicer {
	if s.qrdbClient == nil {
		return nil
	}
	platformClient, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil || platformClient == nil {
		if err != nil {
			s.logger.Warnf("Tenant barrier facade bound to nil transport: %v", err)
		}
		return nil
	}
	return service.NewTenantBarrierService(platformClient.Tenants())
}
