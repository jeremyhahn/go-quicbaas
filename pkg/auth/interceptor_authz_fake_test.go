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

package auth

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// allowListAuthzFake is a minimal interfaces.AuthorizationService used by the
// interceptor tests to exercise the live authorizeWithService seam without the
// deleted forked provider/service authz layer. It allows a request iff its
// "resource:action" matches one of the configured allow-list entries (with
// wildcard support), so the interceptor's allow/deny behaviour under the
// service path can be driven without the deleted fork role manager.
type allowListAuthzFake struct {
	allowed []string
}

// newAllowListAuthzFake returns an interfaces.AuthorizationService that permits
// requests whose resource:action matches one of allowed ("*", "resource:*" and
// "*:action" wildcards are supported).
func newAllowListAuthzFake(allowed ...string) interfaces.AuthorizationService {
	cp := make([]string, len(allowed))
	copy(cp, allowed)
	return &allowListAuthzFake{allowed: cp}
}

func (f *allowListAuthzFake) matches(resource, action string) bool {
	for _, entry := range f.allowed {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if (parts[0] == "*" || parts[0] == resource) && (parts[1] == "*" || parts[1] == action) {
			return true
		}
	}
	return false
}

// Authorize allows the request when its resource:action is on the allow-list.
func (f *allowListAuthzFake) Authorize(_ context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	allowed := f.matches(req.Resource, req.Action)
	reason := "permitted by allow-list policy"
	if !allowed {
		reason = "resource:action not permitted"
	}
	return &interfaces.AuthorizationDecision{
		Allowed:  allowed,
		Reason:   reason,
		Provider: "rbac",
	}, nil
}

// BatchAuthorize evaluates each request via Authorize.
func (f *allowListAuthzFake) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
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

// GetProvider returns nil; the fake does not wrap a provider.
func (f *allowListAuthzFake) GetProvider() interfaces.AuthorizationProvider {
	return nil
}

// GetProviderType reports the RBAC provider type.
func (f *allowListAuthzFake) GetProviderType() string {
	return "rbac"
}

// Close is a no-op for the fake.
func (f *allowListAuthzFake) Close() error {
	return nil
}
