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
	"strings"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

	var logger iamStorageLogger
	if a.server.logger != nil {
		logger = a.server.logger
	}
	converted, err := policiesToABAC(policies, logger)
	if err != nil {
		return err
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
// context, whatever tenant the triggering request was bound to. A sealed
// non-system tenant is skipped with a warning; its policies load when it is
// unsealed.
func (a *qrdbABACSync) allPolicies(ctx context.Context) ([]*iam.Policy, error) {
	tenantIDs := map[string]struct{}{qrdbsdk.SystemTenantID: {}}

	sysCtx := tenantctx.WithSystem(ctx)
	if a.tenantManager != nil {
		tenants, err := a.tenantManager.ListTenants(sysCtx, iam.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, t := range tenants {
			tenantIDs[t.ID] = struct{}{}
		}
	}

	var all []*iam.Policy
	for tenantID := range tenantIDs {
		policies, err := a.storage.ListPolicies(tenantctx.WithTenant(ctx, tenantID), tenantID, iam.ListOptions{})
		if err != nil {
			if tenantID == qrdbsdk.SystemTenantID || !errors.Is(err, qrdbtenant.ErrTenantSealed) {
				return nil, err
			}
			a.warnf("abac sync: tenant %q is sealed; its policies load when it is unsealed", tenantID)
			policies = nil
		}
		if tenantID != qrdbsdk.SystemTenantID {
			legacy, err := a.storage.ListPolicies(sysCtx, tenantID, iam.ListOptions{})
			if err != nil {
				return nil, err
			}
			policies = mergeLegacyPolicies(policies, legacy)
		}
		all = append(all, policies...)
	}
	return all, nil
}

// policyRecordName returns the stored record name of p, flagged or not.
func policyRecordName(p *iam.Policy) string {
	if p.LoadError != nil {
		return p.LoadError.Name
	}
	return p.Metadata.Name
}

// mergeLegacyPolicies keeps all of own and appends each legacy record whose name own lacks.
func mergeLegacyPolicies(own, legacy []*iam.Policy) []*iam.Policy {
	names := make(map[string]struct{}, len(own))
	for _, p := range own {
		names[policyRecordName(p)] = struct{}{}
	}
	for _, p := range legacy {
		if _, ok := names[policyRecordName(p)]; !ok {
			own = append(own, p)
		}
	}
	return own
}

// migrateLegacyPolicies moves tenant policies out of the system namespace:
// an earlier release stored a system admin's tenant policies there, under
// the tenant's key. A sealed tenant is migrated by the unseal hook.
func (a *qrdbABACSync) migrateLegacyPolicies(ctx context.Context) {
	if a.tenantManager == nil {
		return
	}
	tenants, err := a.tenantManager.ListTenants(tenantctx.WithSystem(ctx), iam.ListOptions{})
	if err != nil {
		a.warnf("abac sync: listing tenants to migrate legacy policies failed: %v", err)
		return
	}
	for _, t := range tenants {
		if t.ID != qrdbsdk.SystemTenantID {
			a.migrateTenantLegacyPolicies(ctx, t.ID)
		}
	}
}

// migrateTenantLegacyPolicies copies tenantID's legacy policies from the
// system namespace into the tenant's own, and deletes each legacy copy only
// after its tenant copy was written. A prefix tenant shares the system store,
// so nothing is written for it.
func (a *qrdbABACSync) migrateTenantLegacyPolicies(ctx context.Context, tenantID string) {
	if tenantID == qrdbsdk.SystemTenantID {
		return
	}
	sysCtx := tenantctx.WithSystem(ctx)
	legacy, err := a.storage.ListPolicies(sysCtx, tenantID, iam.ListOptions{})
	if err != nil {
		a.warnf("abac sync: listing legacy policies of tenant %q failed: %v", tenantID, err)
		return
	}
	tenantCtx := tenantctx.WithTenant(ctx, tenantID)
	for _, p := range legacy {
		if p.LoadError != nil {
			continue
		}
		name := p.Metadata.Name
		if err := a.storage.CreatePolicy(tenantCtx, tenantID, p); err != nil {
			if errors.Is(err, iam.ErrAlreadyExists) {
				continue
			}
			if errors.Is(err, qrdbtenant.ErrTenantSealed) {
				return
			}
			a.warnf("abac sync: migrating legacy policy %q of tenant %q failed: %v", name, tenantID, err)
			continue
		}
		if err := a.storage.DeletePolicy(sysCtx, tenantID, name); err != nil {
			a.warnf("abac sync: deleting migrated legacy policy %q of tenant %q failed: %v", name, tenantID, err)
		}
	}
}

// warnf logs a warning through the server's logger, when there is one.
func (a *qrdbABACSync) warnf(format string, args ...interface{}) {
	if a.server == nil || a.server.logger == nil {
		return
	}
	a.server.logger.Warnf(format, args...)
}

// resyncOnUnseal migrates the unsealed tenant's legacy policies and reloads
// the live ABAC set each time a tenant barrier is unsealed: allPolicies
// skips a sealed tenant, so its policies take effect only then.
func (a *qrdbABACSync) resyncOnUnseal(hooks *service.TenantUnsealHooks, sync func(context.Context) error) {
	hooks.Register(tenantUnsealFunc(func(ctx context.Context, tenantID string) {
		a.migrateTenantLegacyPolicies(ctx, tenantID)
		if err := sync(ctx); err != nil {
			a.warnf("abac sync after unsealing tenant %q failed: %v", tenantID, err)
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
