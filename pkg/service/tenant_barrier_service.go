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

package service

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Per-tenant barrier facade — broker-facing, delegates to the embedded qrdb node
// =============================================================================
//
// Per-tenant barrier lifecycle (none/passphrase/shamir) is owned end-to-end by
// go-qrdb; quicbaas adds NO seal logic. This facade is the single broker-side
// abstraction for the per-tenant barrier surface: every quicbaas transport
// (REST, gRPC, the qrdb-quic ALPN bridge, MCP) delegates to it, and it forwards
// each operation to the embedded qrdb node's TenantClient — the same transport
// the broker already uses for tenant lifecycle (CreateTenant). It mirrors the
// system-barrier client adapter (qrdbBarrierClientServicer) one scope down and
// carries no barrier state.
//
// Authorization (mutating verbs require an admin of the target tenant; the
// system tenant may READ status for control-plane observability but is denied
// every mutation) is enforced at the transport layer before reaching this
// facade; this facade re-applies tenant binding and validates shamir
// parameters as a second defence-in-depth layer so all four transports (REST,
// gRPC, QUIC, MCP) inherit the protection from a single seam.
//
// qrdb also re-applies its ScopeTenantOnly rule beneath it (third layer).
// Types are the canonical qrdb SDK transport types — quicbaas declares no shadow
// request/response structs.
// =============================================================================

// TenantBarrierServicer is the broker-facing facade for per-tenant barrier
// lifecycle. It delegates every operation to the embedded qrdb node.
type TenantBarrierServicer interface {
	// TenantBarrierInit initializes the per-tenant barrier with the selected
	// strategy. For the Shamir strategy the response carries the generated
	// M-of-N shares (returned exactly once, never persisted); for none/passphrase
	// the share fields are empty.
	TenantBarrierInit(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)

	// TenantBarrierUnseal unseals the per-tenant barrier with a single key
	// (passphrase strategy). Empty key material is rejected by qrdb.
	TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error

	// TenantBarrierUnsealShare submits a single Shamir share toward the
	// per-tenant barrier's quorum threshold, automatically unsealing once enough
	// distinct shares have been submitted.
	TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error)

	// TenantBarrierSeal seals the per-tenant barrier.
	TenantBarrierSeal(ctx context.Context, tenantID string) error

	// TenantBarrierStatus reports the per-tenant barrier's sealed/initialized
	// state and active strategy metadata (including Shamir threshold/total).
	TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)
}

// tenantBarrierService implements TenantBarrierServicer by delegating to a qrdb
// TenantClient bound to the platform shard. It carries no barrier state — it is
// a pure delegation facade, mirroring databaseService.
type tenantBarrierService struct {
	// source returns the qrdb tenant client for one call. The facade asks on
	// every call, so a client rebuilt over a replaced node service graph is
	// the one used; nil is answered as unavailable.
	source func() qrdbtransport.TenantClient

	// unsealed is told after every successful unseal; nil tells no one.
	unsealed *TenantUnsealHooks
}

// Compile-time interface check.
var _ TenantBarrierServicer = (*tenantBarrierService)(nil)

// NewTenantBarrierService creates a TenantBarrierServicer backed by the embedded
// qrdb node's TenantClient. The client MUST be non-nil; callers gate
// construction on a live platform-shard client.
func NewTenantBarrierService(qrdb qrdbtransport.TenantClient, opts ...TenantBarrierOption) TenantBarrierServicer {
	return NewTenantBarrierServiceFrom(func() qrdbtransport.TenantClient { return qrdb }, opts...)
}

// NewTenantBarrierServiceFrom creates a TenantBarrierServicer that asks source
// for the qrdb TenantClient on every call. It is for a caller whose client can
// be replaced while the facade lives: the embedded node builds a new service
// graph when the system barrier is unsealed again or its encryption is
// migrated. While source returns nil every verb answers ErrUnavailable.
func NewTenantBarrierServiceFrom(source func() qrdbtransport.TenantClient, opts ...TenantBarrierOption) TenantBarrierServicer {
	svc := &tenantBarrierService{source: source}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// client returns the qrdb tenant client for one call, or ErrUnavailable while
// there is none.
func (s *tenantBarrierService) client(op string) (qrdbtransport.TenantClient, error) {
	if s.source != nil {
		if qrdb := s.source(); qrdb != nil {
			return qrdb, nil
		}
	}
	return nil, NewError(errors.ErrUnavailable, op, "tenant barrier service unavailable: the embedded qrdb node is not running")
}

// enforceTenantBinding is the MUTATION gate: it ensures the authenticated
// caller (from ctx) is mutating their own tenant's barrier. Two invariants are
// enforced:
//
//  1. System-tenant callers (auth.IsSystemSubject: TenantID is the literal
//     SystemTenantID) are denied — the system admin holds no
//     per-tenant barrier key, consistent with qrdb's ScopeTenantOnly
//     posture; tenant admins self-bootstrap their own barriers per the
//     locked architecture.
//  2. The caller's TenantID must equal targetTenantID — cross-tenant barrier
//     operations are denied regardless of which permissions are in the token scp.
//
// Read-only verbs (status) use enforceTenantReadBinding instead, which admits
// system-tenant callers for control-plane observability.
//
// An authenticated caller with an empty TenantID is refused outright: it is
// bound to no tenant, and an empty tenant is never the system tenant (#348).
//
// This check runs in the service facade and is therefore shared by all four
// transports (REST, gRPC, QUIC, MCP).
func enforceTenantBinding(ctx context.Context, op, targetTenantID string) error {
	subject, err := barrierCaller(ctx, op)
	if err != nil {
		return err
	}
	if auth.IsSystemSubject(subject) {
		// System-tenant callers carry no per-tenant key; barrier mutations are
		// explicitly scoped to tenant principals (ScopeTenantOnly).
		return NewError(errors.ErrForbidden, op, "system-tenant callers may not perform per-tenant barrier operations")
	}
	if subject.TenantID != targetTenantID {
		return NewError(errors.ErrForbidden, op, "caller tenant does not match target tenant")
	}
	return nil
}

// barrierCaller returns the authenticated subject both barrier gates check.
// A missing or unauthenticated subject, and an authenticated one bound to no
// tenant, are refused with ErrForbidden.
func barrierCaller(ctx context.Context, op string) (interfaces.Subject, error) {
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated {
		return interfaces.Subject{}, NewError(errors.ErrForbidden, op, "no authenticated principal in request context")
	}
	if subject.TenantID == "" {
		return interfaces.Subject{}, NewError(errors.ErrForbidden, op, "authenticated principal carries no tenant binding")
	}
	return subject, nil
}

// enforceTenantReadBinding is the READ gate for the per-tenant barrier
// surface. Unlike enforceTenantBinding it ADMITS system-tenant callers:
// reading a tenant barrier's sealed/initialized state is control-plane
// observability, carries no key material, and is required by the system
// operator to supervise tenant lifecycle. Tenant principals remain strictly
// bound to their own tenant — cross-tenant reads are denied.
func enforceTenantReadBinding(ctx context.Context, op, targetTenantID string) error {
	subject, err := barrierCaller(ctx, op)
	if err != nil {
		return err
	}
	if auth.IsSystemSubject(subject) {
		// Control-plane observability: system-tenant callers may read any
		// tenant barrier's status (never mutate it).
		return nil
	}
	if subject.TenantID != targetTenantID {
		return NewError(errors.ErrForbidden, op, "caller tenant does not match target tenant")
	}
	return nil
}

// TenantBarrierInit delegates to qrdb, building the canonical init request.
// It validates Shamir parameters before delegating when strategy == "shamir".
func (s *tenantBarrierService) TenantBarrierInit(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.init", tenantID); err != nil {
		return nil, err
	}
	if strategy == "shamir" {
		if threshold < 1 {
			return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "shamir threshold must be >= 1")
		}
		if shares < 1 {
			return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "shamir shares must be >= 1")
		}
		if threshold > shares {
			return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "shamir threshold must be <= shares")
		}
	}
	qrdb, err := s.client("tenant_barrier.init")
	if err != nil {
		return nil, err
	}
	return qrdb.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{
		TenantID:   tenantID,
		Passphrase: passphrase,
		Threshold:  threshold,
		Shares:     shares,
		Strategy:   strategy,
	})
}

// TenantBarrierUnseal delegates a single-key unseal to qrdb.
func (s *tenantBarrierService) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if err := enforceTenantBinding(ctx, "tenant_barrier.unseal", tenantID); err != nil {
		return err
	}
	qrdb, err := s.client("tenant_barrier.unseal")
	if err != nil {
		return err
	}
	if err := qrdb.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{
		TenantID: tenantID,
		Key:      key,
	}); err != nil {
		return err
	}
	s.unsealed.notify(ctx, tenantID)
	return nil
}

// TenantBarrierUnsealShare delegates a single Shamir share to qrdb and returns
// the quorum progress.
func (s *tenantBarrierService) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.unseal_share", tenantID); err != nil {
		return nil, err
	}
	qrdb, err := s.client("tenant_barrier.unseal_share")
	if err != nil {
		return nil, err
	}
	progress, err := qrdb.TenantBarrierUnsealShare(ctx, &qrdbtransport.TenantBarrierUnsealShareRequest{
		TenantID: tenantID,
		Share:    share,
	})
	if err != nil {
		return nil, err
	}
	if progress != nil && progress.Complete {
		s.unsealed.notify(ctx, tenantID)
	}
	return progress, nil
}

// TenantBarrierSeal delegates a seal to qrdb.
func (s *tenantBarrierService) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	if err := enforceTenantBinding(ctx, "tenant_barrier.seal", tenantID); err != nil {
		return err
	}
	qrdb, err := s.client("tenant_barrier.seal")
	if err != nil {
		return err
	}
	return qrdb.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{TenantID: tenantID})
}

// TenantBarrierStatus delegates a status query to qrdb. Status is the
// read-only verb of the surface: system-tenant callers are admitted
// (control-plane observability) while tenant callers stay bound to their own
// tenant.
func (s *tenantBarrierService) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	if err := enforceTenantReadBinding(ctx, "tenant_barrier.status", tenantID); err != nil {
		return nil, err
	}
	qrdb, err := s.client("tenant_barrier.status")
	if err != nil {
		return nil, err
	}
	return qrdb.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{TenantID: tenantID})
}
