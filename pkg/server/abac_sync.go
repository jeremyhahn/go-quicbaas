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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// qrdbABACSync implements iam.ABACSync (P6.1 end-state of the unified
// login-plane plan) by recomputing the FULL cross-tenant policy set from
// storage and pushing it into the embedded qrdb node's live ABAC decorator
// via Node.ABACPolicyReloader(). It is the seam iam.PolicyEngine drives
// after every policy CRUD mutation, and once at startup
// (initABACPolicies), so qrdb stays the single authorization decision
// point instead of pkg/iam caching a stale duplicate compiled
// representation.
type qrdbABACSync struct {
	server        *Server
	tenantManager iam.TenantManager
	storage       iam.PolicyStorage
}

// newQRDBABACSync builds the ABACSync the composed PolicyEngine drives.
func newQRDBABACSync(s *Server, tenantManager iam.TenantManager, storage iam.PolicyStorage) *qrdbABACSync {
	return &qrdbABACSync{server: s, tenantManager: tenantManager, storage: storage}
}

// Compile-time interface check.
var _ iam.ABACSync = (*qrdbABACSync)(nil)

// Sync lists every tenant's policies (plus the system tenant's
// platform-wide policies), converts them to qrdb's abac.Policy wire shape,
// and atomically reloads the node's live ABAC decorator. Reload is
// all-or-nothing on the qrdb side: an invalid policy set is rejected and
// leaves the previously active set fully in force.
func (a *qrdbABACSync) Sync(ctx context.Context) error {
	node := a.server.getQRDBNode()
	if node == nil {
		return &QRDBNodeClientUnavailableError{}
	}
	reloader := node.ABACPolicyReloader()
	if reloader == nil {
		// The node was constructed without WithABACLiveReload and has no
		// policies to reload into -- nothing to do (matches qrdb's own
		// zero-overhead-when-unconfigured contract).
		return nil
	}

	policies, err := a.allPolicies(ctx)
	if err != nil {
		return err
	}

	converted := make([]*qrdbsdk.ABACPolicy, 0, len(policies))
	for _, p := range policies {
		converted = append(converted, policyToABAC(p))
	}

	return reloader.Reload(converted)
}

// allPolicies enumerates every tenant (via TenantManager) plus the system
// tenant and lists each one's stored policies. PolicyStorage only supports
// per-tenant listing, so a global scan is composed here rather than in
// pkg/iam, which must not depend on qrdb's cross-tenant tenant.SystemTenantID
// concept.
func (a *qrdbABACSync) allPolicies(ctx context.Context) ([]*iam.Policy, error) {
	tenantIDs := map[string]struct{}{qrdbsdk.SystemTenantID: {}}

	if a.tenantManager != nil {
		tenants, err := a.tenantManager.ListTenants(ctx, iam.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, t := range tenants {
			tenantIDs[t.ID] = struct{}{}
		}
	}

	var all []*iam.Policy
	for tenantID := range tenantIDs {
		policies, err := a.storage.ListPolicies(ctx, tenantID, iam.ListOptions{})
		if err != nil {
			return nil, err
		}
		all = append(all, policies...)
	}
	return all, nil
}

// policyToABAC converts an iam.Policy to qrdb's abac.Policy wire shape
// (qrdbsdk.ABACPolicy). The two schemas are structurally identical by
// construction (qrdb's ABAC engine was ported from this package's own
// evaluator/compiler/trie, since deleted in favor of this live seam), so
// the conversion is a faithful, lossless field-by-field copy: every
// operator, condition expression, and resource-pattern syntax quicbaas's
// policy documents use is understood verbatim by qrdb's condition parser
// and resource trie.
func policyToABAC(p *iam.Policy) *qrdbsdk.ABACPolicy {
	if p == nil {
		return nil
	}
	rules := make([]qrdbsdk.ABACRule, 0, len(p.Spec.Rules))
	for _, r := range p.Spec.Rules {
		rules = append(rules, qrdbsdk.ABACRule{
			Name:      r.Name,
			Effect:    qrdbsdk.ABACEffect(r.Effect),
			Priority:  r.Priority,
			Actions:   r.Actions,
			Resources: r.Resources,
			When:      conditionsToABAC(r.When),
			Unless:    conditionsToABAC(r.Unless),
		})
	}
	return &qrdbsdk.ABACPolicy{
		APIVersion: p.APIVersion,
		Kind:       p.Kind,
		Metadata: qrdbsdk.ABACPolicyMetadata{
			Name:        p.Metadata.Name,
			Tenant:      p.Metadata.Tenant,
			Description: p.Metadata.Description,
			Labels:      p.Metadata.Labels,
			Priority:    p.Metadata.Priority,
			Created:     p.Metadata.Created,
			Updated:     p.Metadata.Updated,
		},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{
				Users:         p.Spec.Subjects.Users,
				Groups:        p.Spec.Subjects.Groups,
				Roles:         p.Spec.Subjects.Roles,
				Services:      p.Spec.Subjects.Services,
				Match:         p.Spec.Subjects.Match,
				Authenticated: p.Spec.Subjects.Authenticated,
				Any:           p.Spec.Subjects.Any,
			},
			Variables: p.Spec.Variables,
			Rules:     rules,
		},
	}
}

// conditionsToABAC converts iam.Condition to qrdb's flat Expression-string
// ABAC condition DSL. iam.Condition carries vestigial And/Or/Not
// sub-condition tree fields that no production code path ever populates
// (quicbaas's own deleted compiler read ONLY .Expression, exactly like
// qrdb's), so no information is lost: both engines express boolean
// composition ("a and b", "a or b", "not a") textually WITHIN a single
// Expression string using an identical parser grammar (verified
// operator-for-operator: ==, !=, eq, neq, >=, <=, >, <, between, endswith,
// startswith, contains, matches, like, in, notin, containsall, containsany,
// notcontains, overlaps, disjoint, exists, notexists).
func conditionsToABAC(conds []iam.Condition) []qrdbsdk.ABACCondition {
	if len(conds) == 0 {
		return nil
	}
	out := make([]qrdbsdk.ABACCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, qrdbsdk.ABACCondition{Expression: c.Expression})
	}
	return out
}
