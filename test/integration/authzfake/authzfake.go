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

//go:build integration

// Package authzfake provides a shared, self-contained test double for the
// protocol authz integration suites.
//
// It exposes an interfaces.AuthorizationService whose allow/deny decision is
// driven by a static allow-list of "resource:action" entries rather than by the
// (now deleted) forked role manager. The double carries no role-manager
// dependency: it decides solely from the entries passed to New plus the
// authentication state on the request Subject. This keeps the transport-seam
// integration tests exercising the real authorizeWithService path while the
// RBAC decision itself remains qrdb's responsibility (covered by qrdb's
// pkg/auth/rbac suite). This is a test-only double, not production code.
package authzfake

import (
	"strings"
	"sync"

	"context"

	"github.com/google/uuid"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// allowListAuthz is an interfaces.AuthorizationService that allows a request iff
// its "resource:action" matches one of the configured allow-list entries.
type allowListAuthz struct {
	// allowed holds the configured "resource:action" grant patterns. Each entry
	// supports wildcards: "*" (any resource and action), "resource:*" (any
	// action on a resource), and "*:action" (an action on any resource).
	allowed []string

	mu     sync.RWMutex
	closed bool
}

// New returns an interfaces.AuthorizationService backed by a static allow-list.
//
// allowed entries are colon-joined "resource:action" grants. A request is
// permitted when its resource:action matches an entry directly or via a
// wildcard ("*", "resource:*", "*:action"). Unauthenticated subjects are
// permitted only when the requested resource:action is on the allow-list (the
// anonymous-access path); otherwise they are denied with "authentication
// required". Authenticated subjects with no roles are denied with "no roles".
func New(allowed ...string) interfaces.AuthorizationService {
	cp := make([]string, len(allowed))
	copy(cp, allowed)
	return &allowListAuthz{allowed: cp}
}

// matches reports whether resource:action is covered by the allow-list.
func (f *allowListAuthz) matches(resource, action string) (string, bool) {
	for _, entry := range f.allowed {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		permResource, permAction := parts[0], parts[1]
		resourceOK := permResource == "*" || permResource == resource
		actionOK := permAction == "*" || permAction == action
		if resourceOK && actionOK {
			return entry, true
		}
	}
	return "", false
}

// Authorize evaluates a single request against the allow-list.
func (f *allowListAuthz) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f.mu.RLock()
	closed := f.closed
	f.mu.RUnlock()
	if closed {
		return nil, &qberrors.MessageError{Message: "authzfake: authorization service is closed"}
	}

	if req == nil {
		return nil, &qberrors.MessageError{Message: "authzfake: authorization request is nil"}
	}

	matchedPolicy, permitted := f.matches(req.Resource, req.Action)

	decision := &interfaces.AuthorizationDecision{
		Provider:         "rbac",
		DecisionID:       uuid.NewString(),
		EvaluationTimeNs: 1,
	}

	switch {
	case permitted && !req.Subject.Authenticated:
		// Anonymous access path: the resource:action is explicitly allowed
		// without authentication.
		decision.Allowed = true
		decision.MatchedPolicy = matchedPolicy
		decision.Reason = "anonymous access allowed"
	case permitted:
		decision.Allowed = true
		decision.MatchedPolicy = matchedPolicy
		decision.Reason = "permitted by allow-list policy"
	case !req.Subject.Authenticated:
		decision.Allowed = false
		decision.MatchedPolicy = "deny"
		decision.Reason = "authentication required"
	case len(req.Subject.Roles) == 0:
		decision.Allowed = false
		decision.MatchedPolicy = "deny"
		decision.Reason = "subject has no roles"
	default:
		decision.Allowed = false
		decision.MatchedPolicy = "deny"
		decision.Reason = "subject roles do not have permission for resource:action"
	}

	return decision, nil
}

// BatchAuthorize evaluates each request via Authorize.
func (f *allowListAuthz) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	if len(reqs) == 0 {
		return nil, &qberrors.MessageError{Message: "authzfake: batch authorization requires at least one request"}
	}
	decisions := make([]*interfaces.AuthorizationDecision, len(reqs))
	for i, req := range reqs {
		decision, err := f.Authorize(ctx, req)
		if err != nil {
			return nil, err
		}
		decisions[i] = decision
	}
	return decisions, nil
}

// GetProvider returns nil; the double does not wrap a separate provider.
func (f *allowListAuthz) GetProvider() interfaces.AuthorizationProvider {
	return nil
}

// GetProviderType reports the RBAC provider type.
func (f *allowListAuthz) GetProviderType() string {
	return "rbac"
}

// Close marks the service closed so subsequent Authorize calls fail.
func (f *allowListAuthz) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}
