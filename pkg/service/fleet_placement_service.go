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

// Fleet placement: the declarative placement specs of SPEC row
// declarative-placement (go-quicbaas#1681) and their read-only drift report,
// as a service the transports call. Not to be confused with PlacementService,
// the scheduler's per-workload placement engine: a fleet placement spec says
// where a CLASS of work may run, across the fleet's hosts.
//
// Authorization is go-qrdb's: the transports map get, list and drift to
// placement:read and declare and withdraw to placement:manage. The service
// adds no role checks. The tenant is the caller's; a request naming another
// is refused through tenantctx.Target like every fleet verb.

package service

import (
	"context"
	stderrors "errors"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// FleetPlacementService declares, reads and withdraws placement specs and
// reports their drift. Every verb takes the tenant the request names; empty
// means the caller's own.
type FleetPlacementService interface {
	DeclarePlacementSpec(ctx context.Context, tenantID string, spec *orchestration.PlacementSpec) (*orchestration.PlacementSpec, error)
	GetPlacementSpec(ctx context.Context, tenantID, name string) (*orchestration.PlacementSpec, error)
	ListPlacementSpecs(ctx context.Context, tenantID string) ([]*orchestration.PlacementSpec, error)
	WithdrawPlacementSpec(ctx context.Context, tenantID, name string) error
	GetPlacementDrift(ctx context.Context, tenantID, name string) (*orchestration.DriftReport, error)
	// ListConsentRequests returns the tenant's per-move consent requests in
	// state (all of them when state is empty), ordered by id. It never
	// returns a nil slice.
	ListConsentRequests(ctx context.Context, tenantID string, state orchestration.ConsentState) ([]*orchestration.PlacementConsentRequest, error)
	// AnswerConsent grants or refuses one pending consent request. The
	// answer's By is the authenticated principal, whatever the caller passed.
	AnswerConsent(ctx context.Context, tenantID, requestID string, answer orchestration.ConsentAnswer) (*orchestration.PlacementConsentRequest, error)
}

// FleetPlacementProvider is implemented by a QuicBaaS that serves fleet
// placement. It is a separate interface rather than a QuicBaaS method so
// that implementations without it (test doubles, alternate facades) answer
// "unavailable" instead of having to stub it.
type FleetPlacementProvider interface {
	FleetPlacement() FleetPlacementService
}

// ErrFleetPlacementUnbound is returned by every verb of a fleet placement
// service the server has not bound to its stores yet.
var ErrFleetPlacementUnbound = stderrors.New("fleet placement: not bound to a spec store and drift reporter")

// FleetPlacementBinder binds a fleet placement service to its stores. The
// server calls it once the orchestration KV, fleet registry and session
// store exist, which is after the service facade is built.
type FleetPlacementBinder interface {
	BindFleetPlacement(specs orchestration.PlacementStore, drift *orchestration.DriftReporter)
}

// ErrFleetPlacementConsentUnbound is returned by the consent verbs of a fleet
// placement service the server has not bound to a consent ledger yet. The
// spec verbs do not depend on it.
var ErrFleetPlacementConsentUnbound = stderrors.New("fleet placement: not bound to a consent ledger")

// FleetPlacementConsentBinder binds a fleet placement service to the ledger
// of per-move consent requests.
type FleetPlacementConsentBinder interface {
	BindFleetPlacementConsent(ledger orchestration.ConsentLedger)
}

type fleetPlacementService struct {
	mu       sync.RWMutex
	specs    orchestration.PlacementStore
	drift    *orchestration.DriftReporter
	consents orchestration.ConsentLedger
}

// NewFleetPlacementService returns a fleet placement service. With nil
// arguments it is unbound and every verb fails with ErrUnavailable until
// BindFleetPlacement supplies both.
func NewFleetPlacementService(specs orchestration.PlacementStore, drift *orchestration.DriftReporter) FleetPlacementService {
	s := &fleetPlacementService{}
	s.BindFleetPlacement(specs, drift)
	return s
}

// BindFleetPlacement implements FleetPlacementBinder.
func (s *fleetPlacementService) BindFleetPlacement(specs orchestration.PlacementStore, drift *orchestration.DriftReporter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.specs, s.drift = specs, drift
}

func (s *fleetPlacementService) bound(op string) (orchestration.PlacementStore, *orchestration.DriftReporter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.specs == nil || s.drift == nil {
		return nil, nil, &errors.QBError{Code: errors.ErrUnavailable, Op: "FleetPlacementService." + op, Err: ErrFleetPlacementUnbound}
	}
	return s.specs, s.drift, nil
}

// begin resolves the tenant and the bound stores for one verb. The KV under
// the stores routes by the tenant bound to the context, so a context that
// names none is bound to the resolved tenant; one that names a tenant is left
// as it is (the rule of the fleet registry's fleetTenantContext).
func (s *fleetPlacementService) begin(ctx context.Context, op, requested string) (context.Context, string, orchestration.PlacementStore, *orchestration.DriftReporter, error) {
	tenantID, err := tenantctx.Target(ctx, "fleet_placement."+op, requested)
	if err != nil {
		return ctx, "", nil, nil, err
	}
	specs, drift, err := s.bound(op)
	if err != nil {
		return ctx, "", nil, nil, err
	}
	if orchestration.TenantFromContext(ctx) == "" {
		ctx = orchestration.ContextWithTenant(ctx, tenantID)
	}
	return ctx, tenantID, specs, drift, nil
}

// DeclarePlacementSpec implements FleetPlacementService.
func (s *fleetPlacementService) DeclarePlacementSpec(ctx context.Context, tenantID string, spec *orchestration.PlacementSpec) (*orchestration.PlacementSpec, error) {
	const op = "DeclarePlacementSpec"
	ctx, tenantID, specs, _, err := s.begin(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "FleetPlacementService." + op, Err: stderrors.New("a placement spec is required")}
	}
	stored, err := specs.DeclarePlacement(ctx, tenantID, spec)
	return stored, classifyFleetPlacementError(op, err)
}

// GetPlacementSpec implements FleetPlacementService.
func (s *fleetPlacementService) GetPlacementSpec(ctx context.Context, tenantID, name string) (*orchestration.PlacementSpec, error) {
	const op = "GetPlacementSpec"
	ctx, tenantID, specs, _, err := s.begin(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	spec, err := specs.GetPlacement(ctx, tenantID, name)
	return spec, classifyFleetPlacementError(op, err)
}

// ListPlacementSpecs implements FleetPlacementService.
func (s *fleetPlacementService) ListPlacementSpecs(ctx context.Context, tenantID string) ([]*orchestration.PlacementSpec, error) {
	const op = "ListPlacementSpecs"
	ctx, tenantID, specs, _, err := s.begin(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	list, err := specs.ListPlacements(ctx, tenantID)
	if err != nil {
		return nil, classifyFleetPlacementError(op, err)
	}
	if list == nil {
		list = []*orchestration.PlacementSpec{}
	}
	return list, nil
}

// WithdrawPlacementSpec implements FleetPlacementService.
func (s *fleetPlacementService) WithdrawPlacementSpec(ctx context.Context, tenantID, name string) error {
	const op = "WithdrawPlacementSpec"
	ctx, tenantID, specs, _, err := s.begin(ctx, op, tenantID)
	if err != nil {
		return err
	}
	return classifyFleetPlacementError(op, specs.WithdrawPlacement(ctx, tenantID, name))
}

// GetPlacementDrift implements FleetPlacementService.
func (s *fleetPlacementService) GetPlacementDrift(ctx context.Context, tenantID, name string) (*orchestration.DriftReport, error) {
	const op = "GetPlacementDrift"
	ctx, tenantID, _, drift, err := s.begin(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	report, err := drift.ReportDrift(ctx, tenantID, name)
	return report, classifyFleetPlacementError(op, err)
}

// BindFleetPlacementConsent implements FleetPlacementConsentBinder.
func (s *fleetPlacementService) BindFleetPlacementConsent(ledger orchestration.ConsentLedger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consents = ledger
}

// beginConsent resolves the tenant and the bound ledger for one consent
// verb, binding the tenant to the context as begin does.
func (s *fleetPlacementService) beginConsent(ctx context.Context, op, requested string) (context.Context, string, orchestration.ConsentLedger, error) {
	tenantID, err := tenantctx.Target(ctx, "fleet_placement."+op, requested)
	if err != nil {
		return ctx, "", nil, err
	}
	s.mu.RLock()
	ledger := s.consents
	s.mu.RUnlock()
	if ledger == nil {
		return ctx, "", nil, &errors.QBError{Code: errors.ErrUnavailable, Op: "FleetPlacementService." + op, Err: ErrFleetPlacementConsentUnbound}
	}
	if orchestration.TenantFromContext(ctx) == "" {
		ctx = orchestration.ContextWithTenant(ctx, tenantID)
	}
	return ctx, tenantID, ledger, nil
}

// ListConsentRequests implements FleetPlacementService.
func (s *fleetPlacementService) ListConsentRequests(ctx context.Context, tenantID string, state orchestration.ConsentState) ([]*orchestration.PlacementConsentRequest, error) {
	const op = "ListConsentRequests"
	ctx, tenantID, ledger, err := s.beginConsent(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	requests, err := ledger.List(ctx, tenantID, state)
	if err != nil {
		return nil, classifyFleetPlacementError(op, err)
	}
	if requests == nil {
		requests = []*orchestration.PlacementConsentRequest{}
	}
	return requests, nil
}

// AnswerConsent implements FleetPlacementService. The answer is recorded as
// the authenticated principal's: a By the caller passed is overwritten.
func (s *fleetPlacementService) AnswerConsent(ctx context.Context, tenantID, requestID string, answer orchestration.ConsentAnswer) (*orchestration.PlacementConsentRequest, error) {
	const op = "AnswerConsent"
	principal := auth.PrincipalFromContext(ctx)
	if principal == "" {
		return nil, &errors.QBError{Code: errors.ErrForbidden, Op: "FleetPlacementService." + op, Err: stderrors.New("no authenticated principal")}
	}
	ctx, tenantID, ledger, err := s.beginConsent(ctx, op, tenantID)
	if err != nil {
		return nil, err
	}
	answer.By = principal
	req, err := ledger.Answer(ctx, tenantID, requestID, answer)
	if err != nil {
		return nil, classifyFleetPlacementError(op, err)
	}
	return req, nil
}

// fleetPlacementErrorCode maps the orchestration placement failures to the
// codes the transports turn into statuses. The first match wins.
var fleetPlacementErrorCode = []struct {
	match func(error) bool
	code  errors.ErrorCode
}{
	{func(err error) bool { var e *orchestration.PlacementSpecInvalidError; return stderrors.As(err, &e) }, errors.ErrInvalidInput},
	{func(err error) bool { var e *orchestration.PlacementNotFoundError; return stderrors.As(err, &e) }, errors.ErrNotFound},
	{func(err error) bool { var e *orchestration.PlacementSpecStaleError; return stderrors.As(err, &e) }, errors.ErrConflict},
	// The report refuses to answer rather than answer wrongly; the refusal
	// names the data that has to be fixed first.
	{func(err error) bool {
		var e *orchestration.PlacementInventoryAmbiguousError
		return stderrors.As(err, &e)
	}, errors.ErrPreconditionFailed},
	{func(err error) bool {
		var e *orchestration.PlacementInventoryInvalidError
		return stderrors.As(err, &e)
	}, errors.ErrPreconditionFailed},
	{func(err error) bool { var e *orchestration.PlacementWorkUnnameableError; return stderrors.As(err, &e) }, errors.ErrPreconditionFailed},
	{func(err error) bool { var e *orchestration.PlacementRoleUnobservedError; return stderrors.As(err, &e) }, errors.ErrPreconditionFailed},
	{func(err error) bool { var e *orchestration.ConsentInvalidError; return stderrors.As(err, &e) }, errors.ErrInvalidInput},
	{func(err error) bool { var e *orchestration.ConsentNotFoundError; return stderrors.As(err, &e) }, errors.ErrNotFound},
	// An answer is final, and a stale swap means another answer stands.
	{func(err error) bool { var e *orchestration.ConsentNotPendingError; return stderrors.As(err, &e) }, errors.ErrConflict},
	{func(err error) bool { var e *orchestration.ConsentStaleError; return stderrors.As(err, &e) }, errors.ErrConflict},
	// An incoherent request grants nothing and cannot be answered.
	{func(err error) bool { var e *orchestration.ConsentIncoherentError; return stderrors.As(err, &e) }, errors.ErrPreconditionFailed},
}

// classifyFleetPlacementError wraps a store or reporter failure in a QBError
// naming the verb. A failure it does not recognise is internal.
func classifyFleetPlacementError(op string, err error) error {
	if err == nil {
		return nil
	}
	var qb *errors.QBError
	if stderrors.As(err, &qb) {
		return err
	}
	for _, entry := range fleetPlacementErrorCode {
		if entry.match(err) {
			return &errors.QBError{Code: entry.code, Op: "FleetPlacementService." + op, Err: err}
		}
	}
	return &errors.QBError{Code: errors.ErrInternal, Op: "FleetPlacementService." + op, Err: err}
}

var (
	_ FleetPlacementService = (*fleetPlacementService)(nil)
	_ FleetPlacementBinder  = (*fleetPlacementService)(nil)
)
