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
	"crypto/x509"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// buildHomeRealmDirectory constructs the home-realm directory over the shared
// platform store, or returns nil when the shared store is not yet available.
// It is the single construction point shared by the REST login wiring
// (attachHomeRealmResolver) and the gRPC/QUIC tenant-context wiring, so HRD is
// built identically across every transport.
func (s *Server) buildHomeRealmDirectory() *iam.HomeRealmDirectory {
	// The home-realm directory reads the shared platform store directly. Skip
	// wiring when the qrdb SDK client is not yet available — GetKVStore panics
	// on a nil client (a programming error in the normal startup path, but the
	// server can be started in isolation, e.g. in unit tests, before the SDK
	// client is initialized). Without a store HRD is simply disabled.
	if s.qrdbClient == nil {
		return nil
	}
	shared := s.GetKVStore(qbshards.PlatformShardID)
	if shared == nil {
		return nil
	}
	dir, err := iam.NewHomeRealmDirectory(shared)
	if err != nil {
		s.logger.WithError(err).Warn("iam: failed to build home-realm directory; HRD disabled")
		return nil
	}
	return dir
}

// tenantContextFunc builds the best-effort, permissive tenant-derivation hook
// shared by the gRPC and QUIC transports. It derives the caller's tenant from
// the VERIFIED peer certificate (pinned SPIFFE trust domain, then home-realm
// discovery) and, on success, stamps it onto the request context so
// tenant-scoped operations (e.g. SPIFFE-stamped certificate issuance) observe
// it. It NEVER rejects a call: when no tenant can be derived (certless callers,
// KV/health/cluster traffic, foreign certs) it returns the input context
// unchanged.
//
// The returned func is consumed by grpcapi.UnaryTenantInterceptor /
// StreamTenantInterceptor and the QUIC handler's SetTenantResolver hook,
// keeping those transport packages free of any iam dependency.
func (s *Server) tenantContextFunc() grpcapi.TenantContextFunc {
	trustDomain := s.config.IAM.SPIFFETrustDomain
	// The directory is built lazily on the first call so the shared store does
	// not have to exist at server-construction time; it commonly comes online
	// only after the platform barrier is initialized.
	return func(ctx context.Context, cert *x509.Certificate) context.Context {
		if cert == nil {
			return ctx
		}
		var hrd iam.HomeRealmResolver
		if dir := s.buildHomeRealmDirectory(); dir != nil {
			hrd = dir
		}
		// HRD without per-request SNI/email hints rarely resolves on its own,
		// but supplying it keeps parity with REST and lets a future hint source
		// (e.g. metadata) participate. SPIFFE SAN is the dominant gRPC/QUIC path.
		tenantID, err := iam.TenantFromPeerCert(ctx, cert, trustDomain, hrd, iam.RealmHints{})
		if err != nil || tenantID == "" {
			// Best-effort: proceed without a tenant. Never reject.
			return ctx
		}
		return tenantctx.WithTenant(ctx, tenantID)
	}
}
