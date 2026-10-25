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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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
	// the share fields are empty. The request is go-qrdb's own type, so an
	// inline Shamir Officers roster and its Policy reach go-qrdb unchanged.
	TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error)

	// TenantBarrierUnseal unseals the per-tenant barrier with a single key
	// (passphrase strategy). Empty key material is rejected by qrdb.
	TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error

	// TenantBarrierSeal seals the per-tenant barrier.
	TenantBarrierSeal(ctx context.Context, tenantID string) error

	// TenantBarrierStatus reports the per-tenant barrier's sealed/initialized
	// state and active strategy metadata (including Shamir threshold/total).
	TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error)

	// TenantBarrierOfficerRegister enrols a share holder for the tenant barrier.
	TenantBarrierOfficerRegister(ctx context.Context, req *qrdbtransport.TenantOfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error)

	// TenantBarrierOfficerList lists the tenant barrier's holders.
	TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error)

	// TenantBarrierShareClaimBegin issues a claim nonce to a tenant holder.
	TenantBarrierShareClaimBegin(ctx context.Context, req *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error)

	// TenantBarrierShareClaimComplete delivers a tenant holder's sealed share
	// envelope exactly once.
	TenantBarrierShareClaimComplete(ctx context.Context, req *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error)

	// TenantBarrierQuorumBegin issues a tenant holder's quorum challenge.
	TenantBarrierQuorumBegin(ctx context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error)

	// TenantBarrierQuorumSubmit submits a tenant holder's share toward the
	// open round; the round's last submission runs the operation.
	TenantBarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error)

	// TenantBarrierRekeyStart opens a rekey round on the tenant barrier.
	TenantBarrierRekeyStart(ctx context.Context, req *qrdbtransport.TenantRekeyStartRequest) (*qrdbtransport.QuorumRound, error)

	// TenantBarrierOfficerRevoke revokes a tenant holder through a
	// quorum-gated rekey.
	TenantBarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.TenantOfficerRevokeRequest) (*qrdbtransport.QuorumRound, error)
}

// tenantBarrierService implements TenantBarrierServicer by delegating to a qrdb
// TenantClient bound to the platform shard. It carries no barrier state — it is
// a pure delegation facade, mirroring databaseService.
type tenantBarrierService struct {
	qrdb qrdbtransport.TenantClient

	// unsealed is told after every successful unseal; nil tells no one.
	unsealed *TenantUnsealHooks
}

// Compile-time interface check.
var _ TenantBarrierServicer = (*tenantBarrierService)(nil)

// NewTenantBarrierService creates a TenantBarrierServicer backed by the embedded
// qrdb node's TenantClient. The client MUST be non-nil; callers gate
// construction on a live platform-shard client.
func NewTenantBarrierService(qrdb qrdbtransport.TenantClient, opts ...TenantBarrierOption) TenantBarrierServicer {
	svc := &tenantBarrierService{qrdb: qrdb}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
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
func (s *tenantBarrierService) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "request is required")
	}
	if err := enforceTenantBinding(ctx, "tenant_barrier.init", req.TenantID); err != nil {
		return nil, err
	}
	if req.Strategy == "shamir" {
		if req.Threshold < 1 {
			return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "shamir threshold must be >= 1")
		}
		if req.Shares < 1 {
			return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "shamir shares must be >= 1")
		}
		if req.Threshold > req.Shares {
			return nil, NewError(errors.ErrInvalidInput, "tenant_barrier.init", "shamir threshold must be <= shares")
		}
	}
	if err := s.requireInitStatement(ctx, req); err != nil {
		return nil, err
	}
	// The request goes to go-qrdb whole: the inline Officers roster and its
	// factor Policy are validated and registered by go-qrdb's holder engine
	// atomically with initialization, so quicbaas neither drops nor checks
	// them here.
	return s.qrdb.TenantBarrierInit(ctx, req)
}

// raftIsolation is go-qrdb's raft isolation type as its transport package
// spells a tenant's isolation, which declares no constant for it.
const raftIsolation = qrdbtransport.TenantIsolationType(qrdbsdk.TenantIsolationRaft)

// requireInitStatement refuses an init that carries no Signer for a
// raft-isolated tenant. go-qrdb signs the owner's init statement with
// req.Signer, and without one it has no statement; every replica of a
// raft-isolated tenant initializes its own key from that statement, so init
// is never called without it. Other isolation types need no statement.
func (s *tenantBarrierService) requireInitStatement(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) error {
	if req.Signer != nil {
		return nil
	}
	resp, err := s.qrdb.GetTenant(ctx, req.TenantID)
	if err != nil {
		return err
	}
	if resp != nil && resp.Tenant.IsolationType == raftIsolation {
		return &TenantBarrierInitStatementUnsupportedError{TenantID: req.TenantID}
	}
	return nil
}

// TenantBarrierUnseal delegates a single-key unseal to qrdb.
func (s *tenantBarrierService) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	if err := enforceTenantBinding(ctx, "tenant_barrier.unseal", tenantID); err != nil {
		return err
	}
	if err := s.qrdb.TenantBarrierUnseal(ctx, &qrdbtransport.TenantBarrierUnsealRequest{
		TenantID: tenantID,
		Key:      key,
	}); err != nil {
		return err
	}
	s.unsealed.notify(ctx, tenantID)
	return nil
}

// TenantBarrierSeal delegates a seal to qrdb.
func (s *tenantBarrierService) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	if err := enforceTenantBinding(ctx, "tenant_barrier.seal", tenantID); err != nil {
		return err
	}
	return s.qrdb.TenantBarrierSeal(ctx, &qrdbtransport.TenantBarrierSealRequest{TenantID: tenantID})
}

// TenantBarrierStatus delegates a status query to qrdb. Status is the
// read-only verb of the surface: system-tenant callers are admitted
// (control-plane observability) while tenant callers stay bound to their own
// tenant.
func (s *tenantBarrierService) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	if err := enforceTenantReadBinding(ctx, "tenant_barrier.status", tenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierStatus(ctx, &qrdbtransport.TenantBarrierStatusRequest{TenantID: tenantID})
}

// TenantBarrierOfficerRegister delegates a holder enrolment to qrdb.
func (s *tenantBarrierService) TenantBarrierOfficerRegister(ctx context.Context, req *qrdbtransport.TenantOfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.officer_register", req.TenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierOfficerRegister(ctx, req)
}

// TenantBarrierOfficerList delegates a holder listing to qrdb. Like status it
// is read-only, so system-tenant callers are admitted.
func (s *tenantBarrierService) TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	if err := enforceTenantReadBinding(ctx, "tenant_barrier.officer_list", tenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierOfficerList(ctx, tenantID)
}

// TenantBarrierShareClaimBegin delegates a share-claim handshake start to qrdb.
func (s *tenantBarrierService) TenantBarrierShareClaimBegin(ctx context.Context, req *qrdbtransport.TenantShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.share_claim_begin", req.TenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierShareClaimBegin(ctx, req)
}

// TenantBarrierShareClaimComplete delegates a share-claim completion to qrdb.
func (s *tenantBarrierService) TenantBarrierShareClaimComplete(ctx context.Context, req *qrdbtransport.TenantShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.share_claim_complete", req.TenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierShareClaimComplete(ctx, req)
}

// TenantBarrierQuorumBegin delegates a quorum challenge request to qrdb.
func (s *tenantBarrierService) TenantBarrierQuorumBegin(ctx context.Context, req *qrdbtransport.TenantQuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.quorum_begin", req.TenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierQuorumBegin(ctx, req)
}

// TenantBarrierQuorumSubmit delegates a quorum submission to qrdb. A
// submission that completes an unseal round tells the unseal hooks.
func (s *tenantBarrierService) TenantBarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.TenantQuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.quorum_submit", req.TenantID); err != nil {
		return nil, err
	}
	outcome, err := s.qrdb.TenantBarrierQuorumSubmit(ctx, req)
	if err != nil {
		return nil, err
	}
	if outcome != nil && outcome.Complete && outcome.Op == qrdbtransport.OpUnseal {
		s.unsealed.notify(ctx, req.TenantID)
	}
	return outcome, nil
}

// TenantBarrierRekeyStart delegates a rekey round start to qrdb.
func (s *tenantBarrierService) TenantBarrierRekeyStart(ctx context.Context, req *qrdbtransport.TenantRekeyStartRequest) (*qrdbtransport.QuorumRound, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.rekey_start", req.TenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierRekeyStart(ctx, req)
}

// TenantBarrierOfficerRevoke delegates a holder revocation to qrdb.
func (s *tenantBarrierService) TenantBarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.TenantOfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
	if err := enforceTenantBinding(ctx, "tenant_barrier.officer_revoke", req.TenantID); err != nil {
		return nil, err
	}
	return s.qrdb.TenantBarrierOfficerRevoke(ctx, req)
}
