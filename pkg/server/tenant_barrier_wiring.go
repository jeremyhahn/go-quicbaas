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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-tenant barrier facade wiring
//
// QuicBaaS does not own per-tenant barriers — go-qrdb does. This builder
// constructs the broker-side TenantBarrierServicer facade from a qrdb SDK
// TenantClient over the canonical qrdb embedded transport, exactly as
// buildBarrierCeremonyServices builds the system-barrier facade. The facade is
// shared by every transport (REST, gRPC, the qrdb-quic ALPN bridge, MCP) so all
// of them operate the single embedded-node per-tenant barrier surface.
//
// The node's OWN SDK client (node.Client()) does not carry a TenantServicer on
// its embedded transport — qrdb wires the tenant service into its
// multi-protocol server only. quicbaas therefore composes a dedicated
// in-process SDK client here whose embedded transport registers the node's
// TenantServicer (the same instance the IAM tenant lifecycle provisions
// through), mirroring the newBootstrapCeremonyClient pattern one capability
// over. quicbaas adds NO barrier or tenant logic — the transport is a pure
// in-process bridge to the node's servicer.
//
// When the qrdb node (or its tenant service) is not yet available the facade
// is nil; callers gate on a non-nil facade before registering the per-tenant
// barrier endpoints, and the transport handlers respond with a typed
// service-unavailable error.
// =============================================================================

// TenantBarrierWiringError describes a failure composing the in-process qrdb
// SDK client that backs the per-tenant barrier facade.
type TenantBarrierWiringError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *TenantBarrierWiringError) Error() string {
	if e.Cause == nil {
		return "tenant barrier wiring: " + e.Stage + " failed"
	}
	return "tenant barrier wiring: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *TenantBarrierWiringError) Unwrap() error { return e.Cause }

// buildTenantBarrierService constructs the TenantBarrierServicer facade over
// the tenant-capable in-process qrdb SDK client. Returns nil when the node's
// tenant service is unavailable (typical pre-wire / sealed-window state), so
// callers can skip registering the per-tenant barrier surface until the
// embedded node is live.
//
// The facade holds no client: it asks currentTenantClient on every call. A
// facade a transport took at boot therefore follows the node when it replaces
// its service graph, and answers unavailable once shutdown has begun.
func (s *Server) buildTenantBarrierService() service.TenantBarrierServicer {
	if s.qrdbTenantClient() == nil {
		return nil
	}
	return service.NewTenantBarrierServiceFrom(s.currentTenantClient, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
}

// currentTenantClient returns the qrdb tenant client over the node's current
// service graph, or nil while there is none. It is the per-call source of
// every per-tenant barrier facade this server builds.
func (s *Server) currentTenantClient() qrdbtransport.TenantClient {
	client := s.qrdbTenantClient()
	if client == nil {
		return nil
	}
	return client.Tenants()
}

// =============================================================================
// Attention-inbox barrier_sealed adapter
//
// The SDLC module's attention inbox reads each tenant's own barrier through
// this thin adapter rather than a new barrier client: it delegates to the
// SAME broker-side TenantBarrierServicer facade every transport uses, which
// itself forwards to the embedded qrdb node. No new barrier logic is added
// here — see docs/software-factory and go-quicbaas#1020 (owner ruling: the
// alert watches the CALLING tenant's own barrier, never the system barrier,
// since a sealed system barrier takes the whole API down before the inbox
// could render anything).
// =============================================================================

// tenantBarrierAttentionReader adapts TenantBarrierServicer to
// sdlc.AttentionBarrierReader.
type tenantBarrierAttentionReader struct {
	barrier service.TenantBarrierServicer
}

var _ sdlc.AttentionBarrierReader = (*tenantBarrierAttentionReader)(nil)

// newTenantBarrierAttentionReader wraps a live TenantBarrierServicer for the
// attention inbox. barrier MUST be non-nil; callers gate construction on
// buildTenantBarrierService returning one.
func newTenantBarrierAttentionReader(barrier service.TenantBarrierServicer) sdlc.AttentionBarrierReader {
	return &tenantBarrierAttentionReader{barrier: barrier}
}

// BarrierStatus implements sdlc.AttentionBarrierReader by delegating to the
// facade's own read verb, which re-applies the tenant read binding (a
// tenant principal may read only its own barrier; the system tenant may
// read any, for control-plane observability) beneath it.
func (r *tenantBarrierAttentionReader) BarrierStatus(ctx context.Context, tenantID string) (sdlc.AttentionBarrierStatus, error) {
	status, err := r.barrier.TenantBarrierStatus(ctx, tenantID)
	if err != nil {
		return sdlc.AttentionBarrierStatus{}, err
	}
	return sdlc.AttentionBarrierStatus{Scope: tenantID, Initialized: status.Initialized, Sealed: status.Sealed}, nil
}

// qrdbTenantClient returns the in-process qrdb SDK client whose embedded
// transport carries the node's TenantServicer, building and caching it on
// first use. The cached client belongs to one node service graph: the node
// replaces its graph when the system barrier is unsealed again and when its
// encryption is migrated, and the client is then built again over the new
// graph's KV, Admin and Tenant servicers, all three taken from one snapshot.
// The replaced client stays open for the calls that already hold it.
//
// The node's native transports call this on every tenant unseal, at any time
// and from any goroutine, so the cache is guarded by tenantSDKMu. Once
// Shutdown has begun (stopTenantBarrierClient) it builds and returns nothing.
//
// Returns nil when the node, its service graph, or the tenant service is not
// available — e.g. before the node boots or during the sealed window, where
// the per-tenant barrier surface is intentionally unreachable.
func (s *Server) qrdbTenantClient() *qrdbsdk.Client {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.Tenant == nil || svcs.KV == nil || svcs.Admin == nil {
		return nil
	}

	s.tenantSDKMu.Lock()
	defer s.tenantSDKMu.Unlock()
	if s.tenantSDKStopped {
		return nil
	}
	if s.tenantSDKClient != nil && s.tenantSDKGraph == any(svcs) {
		return s.tenantSDKClient
	}

	client, err := newTenantBarrierClient(svcs.KV, svcs.Admin, svcs.Tenant)
	if err != nil {
		s.logger.Warnf("Tenant barrier facade bound to nil transport: %v", err)
		return nil
	}
	if s.tenantSDKClient != nil {
		s.tenantSDKRetired = append(s.tenantSDKRetired, s.tenantSDKClient)
	}
	s.tenantSDKClient = client
	s.tenantSDKGraph = svcs
	return client
}

// stopTenantBarrierClient ends the building of tenant-barrier SDK clients:
// from here qrdbTenantClient returns nil, so a per-tenant barrier call is
// answered as unavailable. The clients already built stay open for the calls
// that hold them until closeTenantBarrierClients.
func (s *Server) stopTenantBarrierClient() {
	s.tenantSDKMu.Lock()
	defer s.tenantSDKMu.Unlock()
	s.tenantSDKStopped = true
}

// closeTenantBarrierClients closes the current and every replaced
// tenant-barrier SDK client and returns the first close error. Shutdown calls
// it after the node has stopped its transports, so no handler is still inside
// one of them. It also stops the building of new clients.
func (s *Server) closeTenantBarrierClients() error {
	s.tenantSDKMu.Lock()
	defer s.tenantSDKMu.Unlock()
	s.tenantSDKStopped = true
	clients := s.tenantSDKRetired
	if s.tenantSDKClient != nil {
		clients = append(clients, s.tenantSDKClient)
	}
	s.tenantSDKClient, s.tenantSDKGraph, s.tenantSDKRetired = nil, nil, nil

	var first error
	for _, client := range clients {
		if err := client.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// newTenantBarrierClient composes the qrdb embedded transport + SDK client
// over the supplied node servicers. The tenant servicer MUST be non-nil; KV
// and Admin are required by the qrdb embedded transport contract.
func newTenantBarrierClient(
	kv qrdbsdk.KVServicer,
	admin qrdbsdk.AdminServicer,
	tenantSvc qrdbsdk.ServiceTenantServicer,
) (*qrdbsdk.Client, error) {
	if tenantSvc == nil {
		return nil, &TenantBarrierWiringError{Stage: "tenant servicer"}
	}
	tp, err := qrdbembedded.New(kv, admin,
		qrdbembedded.WithTenantService(tenantSvc),
	)
	if err != nil {
		return nil, &TenantBarrierWiringError{Stage: "embedded transport", Cause: err}
	}
	// Connect is a no-op for the qrdb embedded transport (it never dials
	// anything), so the client is usable immediately.
	client, err := qrdbsdk.NewClient(tp)
	if err != nil {
		return nil, &TenantBarrierWiringError{Stage: "sdk client", Cause: err}
	}
	return client, nil
}
