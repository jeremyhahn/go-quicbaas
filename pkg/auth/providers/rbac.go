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

// Package providers implements authorization providers for QuicBaaS.
// It includes RBAC (Role-Based Access Control) and ABAC (Attribute-Based Access Control) providers.
package providers

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ProviderType constants for authorization providers.
const (
	ProviderTypeRBAC = "rbac"
)

// RBACProvider implements the AuthorizationProvider interface using role-based access control.
// It delegates permission checks to an underlying RoleManager for efficient O(1) lookups.
//
// Thread-safety: This provider is fully thread-safe. The RoleManager handles its own
// synchronization, and this provider uses atomic operations for decision ID generation.
type RBACProvider struct {
	roleManager interfaces.RoleManager
	logger      *logger.Logger
	decisionSeq atomic.Uint64
	closed      atomic.Bool

	// scopes is an optional tenant-aware permission scope resolver
	// (typically a *permission.TenantScopeStore built over the embedded
	// qrdb node's StoreResolver). When non-nil, an admitted permission is
	// further gated by its scope resolved in the SUBJECT's own tenant
	// (per-tenant store -> built-in default -> ScopeSelf), applying the same
	// system-tenant rule the qrdb RBAC authorizer uses:
	//
	//   - ScopeCross  : admitted only for a system-tenant subject.
	//   - ScopeSelf   : admitted when the subject is in the system tenant or
	//                   the subject's tenant equals the request's target tenant.
	//   - ScopeTenantOnly: admitted only when the subject's tenant equals the
	//                   target tenant AND the subject is NOT the system tenant.
	//
	// SECURITY: scope resolution fails closed. When the resolver returns an
	// error — most importantly tenant.ErrTenantSealed for a sealed barrier
	// tenant — the request is DENIED; the scope is never defaulted to an
	// admitting value. When scopes is nil the provider keeps its prior
	// scope-unaware behaviour (back-compat).
	scopes permission.ScopeResolver
}

// SystemTenantID mirrors qrdb's structurally privileged tenant. A subject in
// this tenant is granted cross-tenant scope for whatever roles it holds, per
// the qrdb system-tenant rule. Kept as a package constant so the scope gate
// matches qrdb without importing the value at every call site.
const SystemTenantID = qrdbtenant.SystemTenantID

// WithScopeResolver installs a tenant-aware permission.ScopeResolver so that
// admitted permissions are gated by their per-tenant scope. Passing nil leaves
// the back-compat scope-unaware behaviour in place. It is a no-op once the
// provider is closed.
func (p *RBACProvider) WithScopeResolver(resolver permission.ScopeResolver) *RBACProvider {
	p.scopes = resolver
	return p
}

// NewRBACProvider creates a new RBAC authorization provider.
// It wraps an existing RoleManager to provide the AuthorizationProvider interface.
//
// Parameters:
//   - rm: The RoleManager to delegate permission checks to. Must not be nil.
//   - log: The logger for diagnostic output. If nil, a default logger is created.
//
// Returns:
//   - *RBACProvider: The configured provider.
//   - error: ErrInvalidInput if rm is nil.
func NewRBACProvider(rm interfaces.RoleManager, log *logger.Logger) (*RBACProvider, error) {
	if rm == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "providers.NewRBACProvider",
			Err:  ErrNilRoleManager,
		}
	}

	if log == nil {
		var err error
		log, err = logger.New(logger.LogConfig{
			Level:     "info",
			Format:    "json",
			Output:    "stdout",
			Component: "rbac-provider",
		})
		if err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "providers.NewRBACProvider",
				Err:  err,
			}
		}
	}

	p := &RBACProvider{
		roleManager: rm,
		logger:      log.WithField("provider", ProviderTypeRBAC),
	}

	p.logger.Debug("RBAC provider initialized")
	return p, nil
}

// Authorize checks if the request is allowed based on RBAC rules.
// It delegates to the RoleManager.HasPermission method for the actual check.
//
// Performance: Target latency < 1us for simple role lookups.
//
// Context is respected for cancellation; if the context is canceled before
// the check completes, ErrCanceled is returned.
func (p *RBACProvider) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	if p.closed.Load() {
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "RBACProvider.Authorize",
			Err:  ErrProviderClosed,
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "RBACProvider.Authorize",
			Err:  err,
		}
	}

	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "RBACProvider.Authorize",
			Err:  ErrNilRequest,
		}
	}

	startTime := time.Now()
	decision := p.evaluateRequest(req)
	p.applyScopeGate(ctx, req, decision)
	decision.EvaluationTimeNs = time.Since(startTime).Nanoseconds()

	return decision, nil
}

// applyScopeGate narrows an already-admitted decision by the requested
// permission's per-tenant scope. It is a no-op when no scope resolver is
// installed or when the decision is already a deny. On any scope-resolution
// error — most importantly tenant.ErrTenantSealed for a sealed barrier
// tenant — the decision is flipped to DENY (fail closed); the scope is never
// defaulted to an admitting value.
//
// The target tenant of the request is taken from the context (the same
// convention qrdb uses via tenant.FromContext); the subject's own tenant is
// req.Subject.TenantID. A subject in the system tenant is granted cross-tenant
// reach for ScopeSelf / ScopeCross per the qrdb system-tenant rule.
func (p *RBACProvider) applyScopeGate(ctx context.Context, req *interfaces.AuthorizationRequest, decision *interfaces.AuthorizationDecision) {
	if p.scopes == nil || decision == nil || !decision.Allowed {
		return
	}

	subjectTenant := req.Subject.TenantID
	targetTenant := targetTenantFor(ctx, req)
	required := permission.Permission(req.Resource + ":" + req.Action)

	scope, err := p.scopes.ResolveScope(ctx, subjectTenant, required)
	if err != nil {
		p.denyDecision(decision, "scope resolution failed: "+err.Error())
		return
	}

	inSystem := subjectTenant == SystemTenantID
	switch scope {
	case permission.ScopeCross:
		if !inSystem {
			p.denyDecision(decision, "cross-scope permission requires system tenant")
		}
	case permission.ScopeSelf:
		if !inSystem && subjectTenant != targetTenant {
			p.denyDecision(decision, "self-scope permission: subject not in target tenant")
		}
	case permission.ScopeTenantOnly:
		if inSystem {
			p.denyDecision(decision, "tenant-only permission denied to system tenant")
		} else if subjectTenant != targetTenant {
			p.denyDecision(decision, "tenant-only permission: subject not in target tenant")
		}
	default:
		p.denyDecision(decision, "unknown permission scope")
	}
}

// denyDecision flips an admitted decision to a deny with the given reason,
// clearing the matched policy so audit records reflect the scope rejection.
func (p *RBACProvider) denyDecision(decision *interfaces.AuthorizationDecision, reason string) {
	decision.Allowed = false
	decision.Reason = reason
	decision.MatchedPolicy = ""
}

// targetTenantExtractor resolves the request's target tenant from the context.
// It is a package-level variable so the iam build can install the canonical
// iam.TenantFromContext extractor (see scope_tenant_iam.go) without coupling
// this scope-unaware-by-default file to the iam-tagged package. The default
// extractor returns "" (no tenant bound), in which case the scope gate falls
// back to the subject's own tenant as the target — the correct behaviour for
// the common self-scoped request that acts on the caller's own tenant.
var targetTenantExtractor = func(ctx context.Context) string { return "" }

// targetTenantFor resolves the effective target tenant for a request: the
// context-bound tenant when present, else the subject's own tenant.
func targetTenantFor(ctx context.Context, req *interfaces.AuthorizationRequest) string {
	if t := targetTenantExtractor(ctx); t != "" {
		return t
	}
	return req.Subject.TenantID
}

// evaluateRequest performs the actual RBAC evaluation.
func (p *RBACProvider) evaluateRequest(req *interfaces.AuthorizationRequest) *interfaces.AuthorizationDecision {
	decisionID := p.generateDecisionID()

	// Check for anonymous access first
	if !req.Subject.Authenticated {
		if p.roleManager.AllowsAnonymous(req.Resource, req.Action) {
			return &interfaces.AuthorizationDecision{
				Allowed:       true,
				Reason:        "anonymous access allowed",
				Provider:      ProviderTypeRBAC,
				MatchedPolicy: "anonymous",
				DecisionID:    decisionID,
			}
		}

		return &interfaces.AuthorizationDecision{
			Allowed:       false,
			Reason:        "authentication required",
			Provider:      ProviderTypeRBAC,
			MatchedPolicy: "",
			DecisionID:    decisionID,
		}
	}

	// Check role-based permissions
	roles := req.Subject.Roles
	if len(roles) == 0 {
		return &interfaces.AuthorizationDecision{
			Allowed:       false,
			Reason:        "no roles assigned",
			Provider:      ProviderTypeRBAC,
			MatchedPolicy: "",
			DecisionID:    decisionID,
		}
	}

	if p.roleManager.HasPermission(roles, req.Resource, req.Action) {
		// Find the first matching role for the matched policy
		matchedRole := p.findMatchingRole(roles, req.Resource, req.Action)
		return &interfaces.AuthorizationDecision{
			Allowed:       true,
			Reason:        fmt.Sprintf("role %s grants %s on %s", matchedRole, req.Action, req.Resource),
			Provider:      ProviderTypeRBAC,
			MatchedPolicy: matchedRole,
			DecisionID:    decisionID,
		}
	}

	return &interfaces.AuthorizationDecision{
		Allowed:       false,
		Reason:        p.buildDenyReason(roles, req.Resource, req.Action),
		Provider:      ProviderTypeRBAC,
		MatchedPolicy: "",
		DecisionID:    decisionID,
	}
}

// findMatchingRole finds the first role that grants the requested permission.
func (p *RBACProvider) findMatchingRole(roles []string, resource, action string) string {
	for _, role := range roles {
		if p.roleManager.HasPermission([]string{role}, resource, action) {
			return role
		}
	}
	return ""
}

// buildDenyReason constructs a descriptive denial message.
func (p *RBACProvider) buildDenyReason(roles []string, resource, action string) string {
	roleList := strings.Join(roles, ", ")
	return fmt.Sprintf("roles [%s] do not have %s permission on %s", roleList, action, resource)
}

// generateDecisionID creates a unique identifier for the decision.
func (p *RBACProvider) generateDecisionID() string {
	seq := p.decisionSeq.Add(1)
	return fmt.Sprintf("rbac-%d-%d", time.Now().UnixNano(), seq)
}

// BatchAuthorize performs multiple authorization checks efficiently.
// Each request is evaluated independently, and the results are returned
// in the same order as the requests.
//
// If the context is canceled, processing stops and ErrCanceled is returned.
// Successfully evaluated decisions up to that point are not returned.
func (p *RBACProvider) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	if p.closed.Load() {
		return nil, &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "RBACProvider.BatchAuthorize",
			Err:  ErrProviderClosed,
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "RBACProvider.BatchAuthorize",
			Err:  err,
		}
	}

	if len(reqs) == 0 {
		return []*interfaces.AuthorizationDecision{}, nil
	}

	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))

	for i, req := range reqs {
		// Check context cancellation between requests
		if err := ctx.Err(); err != nil {
			return nil, &errors.QBError{
				Code: errors.ErrCanceled,
				Op:   "RBACProvider.BatchAuthorize",
				Err:  err,
			}
		}

		if req == nil {
			decisions[i] = &interfaces.AuthorizationDecision{
				Allowed:       false,
				Reason:        "nil request",
				Provider:      ProviderTypeRBAC,
				MatchedPolicy: "",
				DecisionID:    p.generateDecisionID(),
			}
			continue
		}

		startTime := time.Now()
		decisions[i] = p.evaluateRequest(req)
		p.applyScopeGate(ctx, req, decisions[i])
		decisions[i].EvaluationTimeNs = time.Since(startTime).Nanoseconds()
	}

	return decisions, nil
}

// GetProviderType returns the provider type identifier.
func (p *RBACProvider) GetProviderType() string {
	return ProviderTypeRBAC
}

// Close releases any resources held by the provider.
// After Close is called, all authorization requests will return ErrProviderClosed.
func (p *RBACProvider) Close() error {
	if p.closed.Swap(true) {
		// Already closed
		return nil
	}

	p.logger.Debug("RBAC provider closed")
	return nil
}
