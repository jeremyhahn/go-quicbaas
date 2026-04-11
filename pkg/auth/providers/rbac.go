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
	decision.EvaluationTimeNs = time.Since(startTime).Nanoseconds()

	return decision, nil
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
