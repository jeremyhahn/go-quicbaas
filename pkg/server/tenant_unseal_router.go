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
	"errors"
	"sync"

	"github.com/jeremyhahn/go-qrdb/pkg/api/errclass"
	"github.com/jeremyhahn/go-qrdb/pkg/ceremony"
	"github.com/jeremyhahn/go-qrdb/pkg/quota"
	"github.com/jeremyhahn/go-qrdb/pkg/seal"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// tenantServiceUnavailableError is the type of errTenantServiceUnavailable. It
// carries go-qrdb's Unavailable error class, so the node's native transports
// answer it as a retryable outage (HTTP 503, gRPC Unavailable) and not as an
// internal error.
type tenantServiceUnavailableError struct{}

// Error implements the error interface.
func (tenantServiceUnavailableError) Error() string {
	return "tenant service unavailable: the embedded qrdb node is not running"
}

// ErrorClass implements errclass.Classed.
func (tenantServiceUnavailableError) ErrorClass() errclass.Class {
	return errclass.Unavailable
}

// errTenantServiceUnavailable is returned by every tenantUnsealRouter call
// while the embedded qrdb node, or the per-tenant barrier facade over it, is
// not running.
var errTenantServiceUnavailable error = tenantServiceUnavailableError{}

// tenantBarrierErrorClasses maps the error codes the per-tenant barrier facade
// refuses an unseal with onto go-qrdb's error classes: a caller the tenant
// binding refuses, and a node that stopped after the facade was built.
var tenantBarrierErrorClasses = map[qberrors.ErrorCode]errclass.Class{
	qberrors.ErrForbidden:   errclass.PermissionDenied,
	qberrors.ErrUnavailable: errclass.Unavailable,
}

// classedTenantBarrierError is a facade error with the go-qrdb error class its
// code maps to. It unwraps to the facade's error, so the code and the message
// are unchanged.
type classedTenantBarrierError struct {
	cause error
	class errclass.Class
}

// Error implements the error interface.
func (e *classedTenantBarrierError) Error() string { return e.cause.Error() }

// Unwrap returns the facade's error for errors.Is/As traversal.
func (e *classedTenantBarrierError) Unwrap() error { return e.cause }

// ErrorClass implements errclass.Classed.
func (e *classedTenantBarrierError) ErrorClass() errclass.Class { return e.class }

// classTenantBarrierError gives a facade error the go-qrdb error class its
// code maps to, so the node's native transports answer a refused caller as
// permission denied and a stopped node as unavailable, not as an internal
// error. An error go-qrdb already classifies (the node's own errors pass
// through the facade) and an error with no mapped code are returned unchanged.
func classTenantBarrierError(err error) error {
	if err == nil || errclass.Classify(err) != errclass.Unclassified {
		return err
	}
	var qe *qberrors.QBError
	if !errors.As(err, &qe) {
		return err
	}
	class, ok := tenantBarrierErrorClasses[qe.Code]
	if !ok {
		return err
	}
	return &classedTenantBarrierError{cause: err, class: class}
}

// tenantUnsealRouter is the tenant servicer the embedded qrdb node's native
// transports (REST, gRPC, QUIC, Unix and MCP) call: it replaces the node's own
// tenant servicer on every one of them, so a native tenant unseal or
// unseal-share runs quicbaas's per-tenant barrier facade (tenant binding and
// unseal observers) with the caller's verified qrdb identity projected onto
// the quicbaas Subject (go-quicbaas 2212). Every other call
// reaches the node's own servicer unchanged. While the node, or the facade, is
// not running the router fails closed with errTenantServiceUnavailable; an
// unseal never falls back to the node's servicer. That error, and the
// facade's refusal of an unseal, carry a go-qrdb error class, which is what
// the native transports map to their status.
//
// The facade's SDK client is built over the node's own servicer
// (qrdbTenantClient), never over this router, so the facade never calls back
// into it.
type tenantUnsealRouter struct {
	inner   func() qrdbsdk.ServiceTenantServicer
	barrier func() service.TenantBarrierServicer

	// mu guards probe, which the qrdb server hands over while the node is
	// still being built; bind passes it on once the node's servicer exists.
	mu    sync.Mutex
	probe qrdbtenant.TenantDataProbe
}

var (
	_ qrdbsdk.ServiceTenantServicer    = (*tenantUnsealRouter)(nil)
	_ qrdbtenant.IsolationChangeWiring = (*tenantUnsealRouter)(nil)
	_ errclass.Classed                 = tenantServiceUnavailableError{}
	_ errclass.Classed                 = (*classedTenantBarrierError)(nil)
)

// newTenantUnsealRouter returns a router over the node's own tenant servicer
// (inner) and the hooked per-tenant barrier facade (barrier). Either may
// return nil while the node is not running.
func newTenantUnsealRouter(inner func() qrdbsdk.ServiceTenantServicer, barrier func() service.TenantBarrierServicer) *tenantUnsealRouter {
	return &tenantUnsealRouter{inner: inner, barrier: barrier}
}

// servicer returns the node's own tenant servicer, or
// errTenantServiceUnavailable while there is none.
func (r *tenantUnsealRouter) servicer() (qrdbsdk.ServiceTenantServicer, error) {
	svc := r.inner()
	if svc == nil {
		return nil, errTenantServiceUnavailable
	}
	return svc, nil
}

// SetTenantDataProbe implements qrdbtenant.IsolationChangeWiring. The probe is
// kept and handed to the node's servicer now if it exists, or by bind once it
// does.
func (r *tenantUnsealRouter) SetTenantDataProbe(p qrdbtenant.TenantDataProbe) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probe = p
	r.applyProbeLocked()
}

// bind hands the kept probe to the node's servicer. It is called once the
// node is set.
func (r *tenantUnsealRouter) bind() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applyProbeLocked()
}

// applyProbeLocked passes the kept probe to the node's servicer when both
// exist. r.mu must be held.
func (r *tenantUnsealRouter) applyProbeLocked() {
	if r.probe == nil {
		return
	}
	wiring, ok := r.inner().(qrdbtenant.IsolationChangeWiring)
	if !ok || wiring == nil {
		return
	}
	wiring.SetTenantDataProbe(r.probe)
}

// inProcessTenantPrincipal names the subject an in-process caller acts as
// when it names a tenant and carries no principal of its own.
const inProcessTenantPrincipal = "in-process"

// inProcessTenantServicer is the tenant servicer an in-process composer wires
// into the SDK's embedded qrdb transport (Server.GetQRDBTenantServicer). It
// is the router with one difference: a tenant unseal or unseal-share from a
// caller that carries no principal acts for the tenant the caller named with
// tenantctx.WithTenant. The embedded transport is the in-process path: there
// is no wire and no authenticator, and the caller is the program that
// composed the server. The facade's tenant binding still applies, so a caller
// that names no tenant, the system tenant, or a tenant other than the target
// is refused. The node's network transports never hold this view; they hold
// the router itself, which accepts a verified identity only.
type inProcessTenantServicer struct {
	*tenantUnsealRouter
}

var (
	_ qrdbsdk.ServiceTenantServicer    = inProcessTenantServicer{}
	_ qrdbtenant.IsolationChangeWiring = inProcessTenantServicer{}
)

// TenantBarrierUnseal runs the router's unseal for the tenant an in-process
// caller named.
func (v inProcessTenantServicer) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	return v.tenantUnsealRouter.TenantBarrierUnseal(inProcessTenantContext(ctx), tenantID, key)
}

// TenantBarrierUnsealShare runs the router's unseal-share for the tenant an
// in-process caller named.
func (v inProcessTenantServicer) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtenant.TenantBarrierUnsealProgress, error) {
	return v.tenantUnsealRouter.TenantBarrierUnsealShare(inProcessTenantContext(ctx), tenantID, share)
}

// inProcessTenantContext binds the in-process principal of the tenant ctx
// names. A context that already carries a Subject or a qrdb identity is
// returned unchanged, so a verified principal is never replaced; so is a
// context that names no tenant, which the facade then refuses.
func inProcessTenantContext(ctx context.Context) context.Context {
	if _, ok := auth.SubjectFromContext(ctx); ok {
		return ctx
	}
	if qrdbsdk.IdentityFromContext(ctx) != nil {
		return ctx
	}
	tenantID, ok := tenantctx.Lookup(ctx)
	if !ok {
		return ctx
	}
	return auth.SubjectIntoContext(ctx, interfaces.Subject{
		ID:            inProcessTenantPrincipal,
		Username:      inProcessTenantPrincipal,
		TenantID:      tenantID,
		Authenticated: true,
	})
}

// TenantBarrierUnseal runs the hooked per-tenant barrier facade, never the
// node's servicer.
func (r *tenantUnsealRouter) TenantBarrierUnseal(ctx context.Context, tenantID string, key []byte) error {
	b := r.barrier()
	if b == nil {
		return errTenantServiceUnavailable
	}
	return classTenantBarrierError(b.TenantBarrierUnseal(auth.ContextWithIdentitySubject(ctx), tenantID, key))
}

// TenantBarrierUnsealShare runs the hooked per-tenant barrier facade, never
// the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierUnsealShare(ctx context.Context, tenantID, share string) (*qrdbtenant.TenantBarrierUnsealProgress, error) {
	b := r.barrier()
	if b == nil {
		return nil, errTenantServiceUnavailable
	}
	progress, err := b.TenantBarrierUnsealShare(auth.ContextWithIdentitySubject(ctx), tenantID, share)
	if err != nil {
		return nil, classTenantBarrierError(err)
	}
	if progress == nil {
		return nil, nil
	}
	return &qrdbtenant.TenantBarrierUnsealProgress{
		TenantID:  progress.TenantID,
		Required:  progress.Required,
		Submitted: progress.Submitted,
		Complete:  progress.Complete,
		Sealed:    progress.Sealed,
	}, nil
}

// CreateTenant forwards to the node's servicer.
func (r *tenantUnsealRouter) CreateTenant(ctx context.Context, id, name string, isolationType qrdbtenant.IsolationType, opts ...qrdbtenant.CreateTenantOption) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.CreateTenant(ctx, id, name, isolationType, opts...)
}

// GetTenant forwards to the node's servicer.
func (r *tenantUnsealRouter) GetTenant(ctx context.Context, tenantID string) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.GetTenant(ctx, tenantID)
}

// ListTenants forwards to the node's servicer.
func (r *tenantUnsealRouter) ListTenants(ctx context.Context) ([]qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ListTenants(ctx)
}

// DeleteTenant forwards to the node's servicer.
func (r *tenantUnsealRouter) DeleteTenant(ctx context.Context, tenantID string) error {
	svc, err := r.servicer()
	if err != nil {
		return err
	}
	return svc.DeleteTenant(ctx, tenantID)
}

// ListDeletedTenants forwards to the node's servicer.
func (r *tenantUnsealRouter) ListDeletedTenants(ctx context.Context, pageToken string, pageSize int) (*qrdbtenant.DeletedTenantsPage, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ListDeletedTenants(ctx, pageToken, pageSize)
}

// ReconcileDeletedTenants forwards to the node's servicer.
func (r *tenantUnsealRouter) ReconcileDeletedTenants(ctx context.Context) (*qrdbtenant.ReconcileReport, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ReconcileDeletedTenants(ctx)
}

// UpdateTenant forwards to the node's servicer.
func (r *tenantUnsealRouter) UpdateTenant(ctx context.Context, tenantID string, patch qrdbtenant.TenantPatch) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.UpdateTenant(ctx, tenantID, patch)
}

// UpdateQuotas forwards to the node's servicer.
func (r *tenantUnsealRouter) UpdateQuotas(ctx context.Context, tenantID string, quotas *quota.Quotas) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.UpdateQuotas(ctx, tenantID, quotas)
}

// UpdateEntitlements forwards to the node's servicer.
func (r *tenantUnsealRouter) UpdateEntitlements(ctx context.Context, tenantID string, entitlements *quota.Entitlements) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.UpdateEntitlements(ctx, tenantID, entitlements)
}

// CheckBarrierMode forwards to the node's servicer.
func (r *tenantUnsealRouter) CheckBarrierMode(ctx context.Context, tenantID string, mode seal.StrategyID) error {
	svc, err := r.servicer()
	if err != nil {
		return err
	}
	return svc.CheckBarrierMode(ctx, tenantID, mode)
}

// IncrementQuotaUsage forwards to the node's servicer.
func (r *tenantUnsealRouter) IncrementQuotaUsage(ctx context.Context, tenantID string, resource quota.Resource, delta int64) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.IncrementQuotaUsage(ctx, tenantID, resource, delta)
}

// DecrementQuotaUsage forwards to the node's servicer.
func (r *tenantUnsealRouter) DecrementQuotaUsage(ctx context.Context, tenantID string, resource quota.Resource, delta int64) (*qrdbtenant.TenantInfo, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.DecrementQuotaUsage(ctx, tenantID, resource, delta)
}

// GetQuotaUsage forwards to the node's servicer.
func (r *tenantUnsealRouter) GetQuotaUsage(ctx context.Context, tenantID string, resource quota.Resource) (int64, error) {
	svc, err := r.servicer()
	if err != nil {
		return 0, err
	}
	return svc.GetQuotaUsage(ctx, tenantID, resource)
}

// TenantBarrierInit forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierInit(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int) (*qrdbtenant.TenantBarrierInitResult, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierInit(ctx, tenantID, passphrase, threshold, shares)
}

// TenantBarrierInitWithStrategy forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierInitWithStrategy(ctx context.Context, tenantID string, passphrase []byte, threshold, shares int, strategyID seal.StrategyID) (*qrdbtenant.TenantBarrierInitResult, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierInitWithStrategy(ctx, tenantID, passphrase, threshold, shares, strategyID)
}

// TenantBarrierSeal forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierSeal(ctx context.Context, tenantID string) error {
	svc, err := r.servicer()
	if err != nil {
		return err
	}
	return svc.TenantBarrierSeal(ctx, tenantID)
}

// TenantBarrierStatus forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierStatus(ctx context.Context, tenantID string) (*qrdbtenant.TenantBarrierStatus, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierStatus(ctx, tenantID)
}

// TenantBarrierOfficerRegister forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierOfficerRegister(ctx context.Context, tenantID string, req *ceremony.RegisterOfficerRequest) (*ceremony.RegisterOfficerResult, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierOfficerRegister(ctx, tenantID, req)
}

// TenantBarrierOfficerList forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*ceremony.OfficerStatus, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierOfficerList(ctx, tenantID)
}

// TenantBarrierShareClaimBegin forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierShareClaimBegin(ctx context.Context, tenantID, username, callerCertThumbprint string) (*ceremony.ChallengeResponse, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierShareClaimBegin(ctx, tenantID, username, callerCertThumbprint)
}

// TenantBarrierShareClaimComplete forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierShareClaimComplete(ctx context.Context, tenantID, username, nonceHex string, signature []byte, callerCertThumbprint string) (*ceremony.ShareClaimResult, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierShareClaimComplete(ctx, tenantID, username, nonceHex, signature, callerCertThumbprint)
}

// TenantBarrierOfficerClaimStatus forwards to the node's servicer.
func (r *tenantUnsealRouter) TenantBarrierOfficerClaimStatus(ctx context.Context, tenantID string) ([]*ceremony.OfficerStatus, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierOfficerClaimStatus(ctx, tenantID)
}

// TenantBarrierShamirSealVote forwards to the node's servicer: it is a seal
// vote, not an unseal.
func (r *tenantUnsealRouter) TenantBarrierShamirSealVote(ctx context.Context, tenantID, share string) (*qrdbtenant.TenantBarrierUnsealProgress, error) {
	svc, err := r.servicer()
	if err != nil {
		return nil, err
	}
	return svc.TenantBarrierShamirSealVote(ctx, tenantID, share)
}
