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
	"sort"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
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
// leaves the previously active set fully in force. A tenant whose barrier is
// sealed does not fail the reload: its unreadable policies are replaced by
// sealedTenantDenyPolicy until it is unsealed.
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

	policies, sealed, err := a.allPolicies(ctx)
	if err != nil {
		return err
	}

	var logger iamStorageLogger
	if a.server.logger != nil {
		logger = a.server.logger
	}
	converted, err := policiesToABAC(policies, logger)
	if err != nil {
		return err
	}
	for _, tenantID := range sealed {
		if logger != nil {
			logger.Warnf("abac sync: tenant %q is sealed, its stored policies are not loaded: every request in it except its barrier's is denied until it is unsealed", tenantID)
		}
		converted = append(converted, sealedTenantDenyPolicy(tenantID))
	}

	return reloader.Reload(converted)
}

// policiesToABAC converts the stored policies for qrdb's ABAC engine. A
// flagged entry (a stored record that did not decode strictly) is never
// converted: none of its rules take effect, so what it would have allowed
// is denied. Each one is logged at WARN with its name and unknown keys.
func policiesToABAC(policies []*iam.Policy, logger iamStorageLogger) ([]*qrdbsdk.ABACPolicy, error) {
	converted := make([]*qrdbsdk.ABACPolicy, 0, len(policies))
	for _, p := range policies {
		if p.LoadError != nil {
			if logger != nil {
				logger.Warnf("abac sync: stored policy not applied, every request in its tenant is denied until it is repaired or deleted: tenant=%q name=%q unknown_keys=%q reason=%q",
					p.LoadError.TenantID, p.LoadError.Name, strings.Join(p.LoadError.UnknownKeys, ","), p.LoadError.Reason)
			}
			converted = append(converted, flaggedRecordDenyPolicy(p))
			continue
		}
		abacPolicy, err := policyToABAC(p)
		if err != nil {
			return nil, err
		}
		converted = append(converted, abacPolicy)
	}
	return converted, nil
}

// flaggedRecordDenyRule names the rule of the policy that stands in for a
// stored record that did not decode strictly.
const flaggedRecordDenyRule = "stored-record-not-applied"

// flaggedRecordDenyPolicy returns the policy that stands in for a stored
// record that did not decode strictly. Its rules are unknown, so it may have
// held denies: skipping it could let another allow policy or the RBAC
// fallback admit a request it blocked. It therefore denies every request in
// the record's tenant (its storage key names the tenant even when its body
// does not decode). A record whose tenant is unknown is scoped to the system
// tenant, whose policies apply to every tenant, so it denies everywhere.
func flaggedRecordDenyPolicy(p *iam.Policy) *qrdbsdk.ABACPolicy {
	tenant := p.LoadError.TenantID
	if tenant == "" {
		tenant = qrdbsdk.SystemTenantID
	}
	return &qrdbsdk.ABACPolicy{
		APIVersion: iam.PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata: qrdbsdk.ABACPolicyMetadata{
			Name:   p.LoadError.Name,
			Tenant: tenant,
		},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Any: true},
			Rules: []qrdbsdk.ABACRule{{
				Name:      flaggedRecordDenyRule,
				Effect:    qrdbsdk.ABACEffectDeny,
				Actions:   []string{"*"},
				Resources: []string{"**"},
			}},
		},
	}
}

// allPolicies enumerates every tenant (via TenantManager) plus the system
// tenant and lists each one's stored policies. PolicyStorage only supports
// per-tenant listing, so a global scan is composed here rather than in
// pkg/iam, which must not depend on qrdb's cross-tenant tenant.SystemTenantID
// concept. Storage routes by the context's tenant, so the catalog is read in
// the system namespace and each tenant's policies under that tenant's own
// context, whatever tenant the triggering request was bound to.
//
// A tenant whose barrier is sealed cannot be listed; it is returned in sealed
// instead of failing the scan, so one sealed tenant does not stop every other
// tenant's reload. The system tenant is never skipped: its policies apply to
// every tenant, so a failure to read them still fails the scan.
func (a *qrdbABACSync) allPolicies(ctx context.Context) (all []*iam.Policy, sealed []string, err error) {
	tenantIDs := map[string]struct{}{qrdbsdk.SystemTenantID: {}}

	sysCtx := tenantctx.WithSystem(ctx)
	if a.tenantManager != nil {
		tenants, err := a.tenantManager.ListTenants(sysCtx, iam.ListOptions{})
		if err != nil {
			return nil, nil, err
		}
		for _, t := range tenants {
			tenantIDs[t.ID] = struct{}{}
		}
	}

	for tenantID := range tenantIDs {
		policies, err := a.storage.ListPolicies(tenantctx.WithTenant(ctx, tenantID), tenantID, iam.ListOptions{})
		if err != nil {
			if tenantID != qrdbsdk.SystemTenantID && abacTenantSealed(err) {
				sealed = append(sealed, tenantID)
				continue
			}
			return nil, nil, err
		}
		all = append(all, policies...)
	}
	sort.Strings(sealed)
	return all, sealed, nil
}

// abacTenantSealed reports whether err says a tenant's own store cannot be
// opened because its barrier is sealed (or not open on this node).
func abacTenantSealed(err error) bool {
	return errors.Is(err, qrdbsdk.ErrTenantStoreSealed) || errors.Is(err, qrdbsdk.ErrTenantSealed)
}

const (
	// sealedTenantDenyPolicyName names the policy that stands in for a
	// sealed tenant's unreadable policies.
	sealedTenantDenyPolicyName = "tenant-sealed"
	// sealedTenantDenyRule names its one rule.
	sealedTenantDenyRule = "tenant-sealed-policies-not-loaded"
	// sealedTenantBarrierResource is the permission resource of the tenant
	// barrier routes (status, unseal), the one thing a sealed tenant must
	// still be able to reach.
	sealedTenantBarrierResource = "tenant_barrier"
)

// sealedTenantDenyPolicy returns the policy that stands in for a sealed
// tenant's stored policies. They cannot be read, so they may hold denies:
// leaving the tenant with no policy could let the RBAC fallback or a
// platform-wide allow admit a request one of them blocked. It therefore
// denies every request in that tenant except those on the tenant's barrier,
// which stay decided by the platform-wide policies and RBAC so the tenant
// can still be unsealed. The next reload after the unseal replaces it with
// the tenant's stored policies.
func sealedTenantDenyPolicy(tenantID string) *qrdbsdk.ABACPolicy {
	return &qrdbsdk.ABACPolicy{
		APIVersion: iam.PolicyYAMLAPIVersion,
		Kind:       "Policy",
		Metadata: qrdbsdk.ABACPolicyMetadata{
			Name:   sealedTenantDenyPolicyName,
			Tenant: tenantID,
		},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Any: true},
			Rules: []qrdbsdk.ABACRule{{
				Name:      sealedTenantDenyRule,
				Effect:    qrdbsdk.ABACEffectDeny,
				Actions:   []string{"*"},
				Resources: []string{"**"},
				Unless:    []qrdbsdk.ABACCondition{{Expression: "resource == '" + sealedTenantBarrierResource + "'"}},
			}},
		},
	}
}

// resyncABACOnTenantUnseal reloads the live policy set each time a tenant's
// barrier opens, so the tenant's stored policies replace its sealed stand-in.
func (s *Server) resyncABACOnTenantUnseal(sync func(context.Context) error) {
	s.tenantUnsealHooks.Register(tenantUnsealFunc(func(ctx context.Context, tenantID string) {
		if err := sync(ctx); err != nil && s.logger != nil {
			s.logger.WithError(err).Warnf("ABAC policy sync after tenant %s was unsealed failed; its stored policies are not enforced yet", tenantID)
		}
	}))
}

// policyToABAC converts an iam.Policy to qrdb's abac.Policy wire shape
// (qrdbsdk.ABACPolicy). The two schemas are structurally identical by
// construction (qrdb's ABAC engine was ported from this package's own
// evaluator/compiler/trie, since deleted in favor of this live seam), so
// the conversion is a faithful, lossless field-by-field copy: every
// operator, condition expression, and resource-pattern syntax quicbaas's
// policy documents use is understood verbatim by qrdb's condition parser
// and resource trie. The one exception is a nested And/Or/Not condition,
// which qrdb cannot hold; it is refused (see conditionsToABAC).
func policyToABAC(p *iam.Policy) (*qrdbsdk.ABACPolicy, error) {
	if p == nil {
		return nil, nil
	}
	rules := make([]qrdbsdk.ABACRule, 0, len(p.Spec.Rules))
	for _, r := range p.Spec.Rules {
		when, err := conditionsToABAC("when", r.When)
		if err != nil {
			return nil, err
		}
		unless, err := conditionsToABAC("unless", r.Unless)
		if err != nil {
			return nil, err
		}
		rules = append(rules, qrdbsdk.ABACRule{
			Name:      r.Name,
			Effect:    qrdbsdk.ABACEffect(r.Effect),
			Priority:  r.Priority,
			Actions:   r.Actions,
			Resources: r.Resources,
			When:      when,
			Unless:    unless,
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
	}, nil
}

// conditionsToABAC converts iam.Condition to qrdb's flat Expression-string
// ABAC condition DSL. iam.Condition also carries And/Or/Not sub-condition
// fields that qrdb's condition cannot hold; a condition using them is
// refused with iam.ErrConditionNested rather than dropped, because dropping
// an authorization condition would widen what the rule matches. Writes are
// already refused by iam.Rule.Validate; this check is defence in depth for
// policies stored before that validation existed. Boolean composition
// ("a and b", "a or b", "not a") is written textually WITHIN a single
// Expression string, using a parser grammar identical in both engines (verified
// operator-for-operator: ==, !=, eq, neq, >=, <=, >, <, between, endswith,
// startswith, contains, matches, like, in, notin, containsall, containsany,
// notcontains, overlaps, disjoint, exists, notexists).
func conditionsToABAC(field string, conds []iam.Condition) ([]qrdbsdk.ABACCondition, error) {
	if err := iam.ValidateConditions(field, conds); err != nil {
		return nil, err
	}
	if len(conds) == 0 {
		return nil, nil
	}
	out := make([]qrdbsdk.ABACCondition, 0, len(conds))
	for _, c := range conds {
		out = append(out, qrdbsdk.ABACCondition{Expression: c.Expression})
	}
	return out, nil
}
