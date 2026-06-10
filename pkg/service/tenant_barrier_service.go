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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
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
// Authorization (caller must be admin of the target tenant; the system tenant
// is denied) is enforced at the transport layer before reaching this facade;
// this facade re-applies tenant binding and validates shamir parameters as a
// second defence-in-depth layer so all four transports (REST, gRPC, QUIC, MCP)
// inherit the protection from a single seam.
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
	TenantBarrierInit(ctx context.Context, tenantID, passphrase string, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error)

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
	qrdb qrdbtransport.TenantClient
}

// Compile-time interface check.
var _ TenantBarrierServicer = (*tenantBarrierService)(nil)

// NewTenantBarrierService creates a TenantBarrierServicer backed by the embedded
// qrdb node's TenantClient. The client MUST be non-nil; callers gate
// construction on a live platform-shard client.
func NewTenantBarrierService(qrdb qrdbtransport.TenantClient) TenantBarrierServicer {
	return &tenantBarrierService{qrdb: qrdb}
}

// enforceTenantBinding ensures the authenticated caller (from ctx) is operating
// on their own tenant's barrier. Two invariants are enforced:
//
//  1. System-tenant callers (empty TenantID) are denied — the system admin holds
//     no per-tenant barrier key, consistent with qrdb's ScopeTenantOnly posture.
//  2. The caller's TenantID must equal targetTenantID — cross-tenant barrier
//     operations are denied regardless of which permissions are in the token scp.
//
// This check runs in the service facade and is therefore shared by all four
// transports (REST, gRPC, QUIC, MCP).
func enforceTenantBinding(ctx context.Context, op, targetTenantID string) error {
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated {
		return NewError(errors.ErrForbidden, op, "no authenticated principal in request context")
	}
	if subject.TenantID == "" {
		// System-tenant callers carry no per-tenant key; barrier ops are
		// explicitly scoped to tenant principals (ScopeTenantOnly).
		return NewError(errors.ErrForbidden, op, "system-tenant callers may not perform per-tenant barrier operations")
	}
	if subject.TenantID != targetTenantID {
		return NewError(errors.ErrForbidden, op, "caller tenant does not match target tenant")
	}
	return nil
}

// TenantBarrierInit delegates to qrdb, building the canonical init request.
// It validates Shamir parameters before delegating when strategy == "shamir".
func (s *tenantBarrierService) TenantBarrierInit(ctx context.Context, tenantID, passphrase string, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
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
	return s.qrdb.TenantBarrierInit(ctx, &qrdbtransport.TenantBarrierInitRequest{
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
	return s.qrdb.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{
		TenantID: tenantID,
		Key:      key,
	})
}

// TenantBarrierUnsealShare delegates a single Shamir share to qrdb and returns
// the quorum progress.
func (s *tenantBarrierService) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.unseal_share", tenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierUnsealShare(ctx, &qrdbtransport.TenantBarrierUnsealShareRequest{
		TenantID: tenantID,
		Share:    share,
	})
}

// TenantBarrierSeal delegates a seal to qrdb.
func (s *tenantBarrierService) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	if err := enforceTenantBinding(ctx, "tenant_barrier.seal", tenantID); err != nil {
		return err
	}
	return s.qrdb.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{TenantID: tenantID})
}

// TenantBarrierStatus delegates a status query to qrdb.
func (s *tenantBarrierService) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.status", tenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{TenantID: tenantID})
}
