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

package iam

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/abac"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
)

// qrdbBackedRBACAuthorizer implements RBACAuthorizer over a REAL go-qrdb
// auth.Authorizer (not a fake) — the composed RBAC-authorizer-decorated-
// with-live-reloaded-ABAC that pkg/server/pep_rbac_authorizer.go's
// qrdbPEPRBACAuthorizer reaches via the embedded node's RBACClient
// (unreachable from this package directly, since pkg/iam carries no hard
// qrdb dependency in its decision path — see RBACAuthorizer's doc comment).
// The end-state of P6.1: this is now PolicyEngine.Evaluate's ENTIRE
// decision, not a fallback path layered under a local ABAC engine.
type qrdbBackedRBACAuthorizer struct {
	inner qrdbauth.Authorizer
}

func (a *qrdbBackedRBACAuthorizer) Authorize(ctx context.Context, subject *Subject, resource, action string) error {
	identity := &qrdbauth.Identity{Subject: subject.ID, TenantID: subject.TenantID, Roles: subject.Roles}
	return a.inner.Authorize(ctx, identity, resource, action)
}

// pepTestRoleServicer is a minimal role.RoleServicer test double (nil embed,
// only ResolvePermissions overridden) backing the real rbac.RBACAuthorizer.
type pepTestRoleServicer struct {
	role.RoleServicer
	perms map[string][]permission.Permission
}

func (r *pepTestRoleServicer) ResolvePermissions(_ context.Context, roleIDs []string) ([]permission.Permission, error) {
	var out []permission.Permission
	for _, id := range roleIDs {
		out = append(out, r.perms[id]...)
	}
	return out, nil
}

// allowAllPolicy returns an abac.Policy whose single rule ALLOWs
// resource:action for every subject.
func allowAllPolicy(name, resource, action string) *abac.Policy {
	return &abac.Policy{
		APIVersion: "v1", Kind: abac.PolicyKind,
		Metadata: abac.PolicyMetadata{Name: name, Tenant: "tenant-a"},
		Spec: abac.PolicySpec{
			Subjects: abac.SubjectSelector{Any: true},
			Rules: []abac.Rule{{
				Name: "allow", Effect: abac.EffectAllow,
				Actions: []string{action}, Resources: []string{resource},
			}},
		},
	}
}

// denyAllPolicy returns an abac.Policy whose single rule DENIES
// resource:action for every subject.
func denyAllPolicy(name, resource, action string) *abac.Policy {
	return &abac.Policy{
		APIVersion: "v1", Kind: abac.PolicyKind,
		Metadata: abac.PolicyMetadata{Name: name, Tenant: "tenant-a"},
		Spec: abac.PolicySpec{
			Subjects: abac.SubjectSelector{Any: true},
			Rules: []abac.Rule{{
				Name: "deny", Effect: abac.EffectDeny,
				Actions: []string{action}, Resources: []string{resource},
			}},
		},
	}
}

// TestPEPService_UnifiedDecision_MatchesQRDBAuthorizer is the P6.1 guard
// test: PEPService's decision (via PolicyEngine.Evaluate, the path
// pep_service.go actually uses) must equal the single qrdb Authorizer's
// decision for the same identity/resource/action, across the four cases
// the unified model defines, now exercising a REAL abac.Decorator
// (abac.Decorate over the real rbac.RBACAuthorizer, abac.WithLiveReload)
// instead of the deleted local compiler/evaluator:
//
//   - RBAC-only grant (no ABAC policy at all): PEP admits iff RBAC admits.
//   - ABAC allow overlay: PEP admits even when RBAC alone would deny.
//   - ABAC deny: PEP denies even when RBAC alone would admit.
//   - Neither an RBAC grant nor an ABAC policy: both deny (RBAC's own
//     default-deny, per the unified model's explicit semantics).
func TestPEPService_UnifiedDecision_MatchesQRDBAuthorizer(t *testing.T) {
	// go-qrdb v0.19 removed the ScopeSelf default for an unregistered
	// permission (go-qrdb#504): resolution now fails outright rather than
	// silently applying the weakest check in the vocabulary. These two are
	// this test's own synthetic permissions, so the test declares their
	// scope. ScopeTenantOnly keeps each one inside the request's tenant,
	// which is what the fixtures assume.
	permission.RegisterScope(permission.ScopeTenantOnly, "widgets:read", "widgets:write")

	roles := &pepTestRoleServicer{perms: map[string][]permission.Permission{
		"viewer": {"widgets:read"},
	}}
	rbacAuthorizer := rbac.New(roles)

	decorated, err := abac.Decorate(rbacAuthorizer, abac.WithLiveReload())
	require.NoError(t, err)
	reloader, ok := decorated.(abac.PolicyReloader)
	require.True(t, ok, "abac.WithLiveReload must return a reloadable decorator")

	fallback := &qrdbBackedRBACAuthorizer{inner: decorated}

	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage:        NewMemoryStorage(),
		RBACAuthorizer: fallback,
	})

	// The RBAC authorizer's system-tenant rule gates a ScopeSelf permission
	// by the REQUEST's target tenant (tenant.FromContext), independent of
	// the identity's own TenantID; bind it once so both engine.Evaluate
	// (via the fallback) and the direct qrdb-authorizer comparison calls
	// observe the same target tenant.
	ctx := tenant.WithTenantID(context.Background(), "tenant-a")
	qrdbIdentity := func(subjectID string, roles []string) *qrdbauth.Identity {
		return &qrdbauth.Identity{Subject: subjectID, TenantID: "tenant-a", Roles: roles}
	}

	t.Run("RBAC-only grant admits", func(t *testing.T) {
		req := &CheckRequest{
			Subject:  Subject{ID: "alice", TenantID: "tenant-a", Roles: []string{"viewer"}},
			Resource: "widgets", Action: "read",
		}
		resp, err := engine.Evaluate(ctx, req)
		require.NoError(t, err)

		qrdbErr := rbacAuthorizer.Authorize(ctx, qrdbIdentity("alice", []string{"viewer"}), "widgets", "read")
		assert.Equal(t, qrdbErr == nil, resp.Allowed,
			"PEP.Allowed=%v must equal (qrdb authorize err == nil)=%v", resp.Allowed, qrdbErr == nil)
		assert.True(t, resp.Allowed)
	})

	t.Run("RBAC-only, no grant denies", func(t *testing.T) {
		req := &CheckRequest{
			Subject:  Subject{ID: "bob", TenantID: "tenant-a", Roles: []string{}},
			Resource: "widgets", Action: "read",
		}
		resp, err := engine.Evaluate(ctx, req)
		require.NoError(t, err)

		qrdbErr := rbacAuthorizer.Authorize(ctx, qrdbIdentity("bob", []string{}), "widgets", "read")
		assert.Equal(t, qrdbErr == nil, resp.Allowed)
		assert.False(t, resp.Allowed, "neither an RBAC grant nor an ABAC policy must deny")
	})

	t.Run("ABAC allow overlay admits beyond RBAC", func(t *testing.T) {
		require.NoError(t, reloader.Reload([]*abac.Policy{allowAllPolicy("allow-gadgets", "gadgets", "delete")}))

		req := &CheckRequest{
			// bob holds no RBAC role at all; RBAC alone would deny.
			Subject:  Subject{ID: "bob", TenantID: "tenant-a", Roles: []string{}},
			Resource: "gadgets", Action: "delete",
		}
		resp, err := engine.Evaluate(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed, "an explicit ABAC allow must grant beyond RBAC")

		qrdbErr := rbacAuthorizer.Authorize(ctx, qrdbIdentity("bob", []string{}), "gadgets", "delete")
		assert.Error(t, qrdbErr, "RBAC alone (no ABAC overlay) must deny the same request")
	})

	t.Run("ABAC deny overrides an RBAC grant", func(t *testing.T) {
		require.NoError(t, reloader.Reload([]*abac.Policy{denyAllPolicy("deny-widgets-write", "widgets", "write")}))
		roles.perms["editor"] = []permission.Permission{"widgets:write"}

		// A distinct subject ID (not "alice", used above) avoids colliding
		// with the qrdb authorizer's own per-(tenant,subject) permission
		// cache, which is keyed by subject identity, not by the role set
		// presented on a given call.
		req := &CheckRequest{
			// carol holds the RBAC role that grants widgets:write...
			Subject:  Subject{ID: "carol", TenantID: "tenant-a", Roles: []string{"editor"}},
			Resource: "widgets", Action: "write",
		}
		resp, err := engine.Evaluate(ctx, req)
		require.NoError(t, err)
		assert.False(t, resp.Allowed, "an explicit ABAC deny must override an RBAC grant")

		qrdbErr := rbacAuthorizer.Authorize(ctx, qrdbIdentity("carol", []string{"editor"}), "widgets", "write")
		assert.NoError(t, qrdbErr, "RBAC alone (no ABAC deny) must admit the same request")
	})
}

// TestPEPService_LiveReload_FlipsDecisionWithoutRestart is the P6.1
// end-state acceptance case: a policy loaded via Reload (the SAME seam
// pkg/server's qrdbABACSync drives after PolicyEngine.CreatePolicy commits
// a new stored policy) flips a live PEP Check decision with no process
// restart and no PolicyEngine re-construction — proving the composed
// authorizer is genuinely live-reloadable end to end, not merely
// constructed once with a static policy set.
func TestPEPService_LiveReload_FlipsDecisionWithoutRestart(t *testing.T) {
	roles := &pepTestRoleServicer{perms: map[string][]permission.Permission{}}
	rbacAuthorizer := rbac.New(roles)

	decorated, err := abac.Decorate(rbacAuthorizer, abac.WithLiveReload())
	require.NoError(t, err)
	reloader, ok := decorated.(abac.PolicyReloader)
	require.True(t, ok)

	engine := NewPolicyEngine(PolicyEngineOptions{
		Storage:        NewMemoryStorage(),
		RBACAuthorizer: &qrdbBackedRBACAuthorizer{inner: decorated},
	})

	ctx := tenant.WithTenantID(context.Background(), "tenant-a")
	req := &CheckRequest{
		Subject:  Subject{ID: "dana", TenantID: "tenant-a"},
		Resource: "reports", Action: "publish",
	}

	// Before any policy exists: dana holds no RBAC role and no ABAC policy
	// resolves the request -- denied.
	before, err := engine.Evaluate(ctx, req)
	require.NoError(t, err)
	assert.False(t, before.Allowed, "must deny before any policy is loaded")

	// Reload with a fresh policy, exactly what qrdbABACSync.Sync does after
	// PolicyEngine.CreatePolicy commits the policy to storage. No restart,
	// no new PolicyEngine, no new Decorator -- the SAME engine/authorizer
	// instances observe the change on the very next call.
	require.NoError(t, reloader.Reload([]*abac.Policy{
		allowAllPolicy("allow-publish", "reports", "publish"),
	}))

	after, err := engine.Evaluate(ctx, req)
	require.NoError(t, err)
	assert.True(t, after.Allowed, "must allow immediately after Reload, without any restart")
}

// TestPolicyEvaluator_NilRBACAuthorizer_PreservesDefaultDeny proves the
// embedded/test-context fallback (no qrdb node composed, RBACAuthorizer
// nil) keeps the historical default-deny-on-no-match behavior rather than
// silently admitting.
func TestPolicyEvaluator_NilRBACAuthorizer_PreservesDefaultDeny(t *testing.T) {
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: NewMemoryStorage()})

	resp, err := engine.Evaluate(context.Background(), &CheckRequest{
		Subject:  Subject{ID: "alice", TenantID: "tenant-a", Roles: []string{"admin"}},
		Resource: "widgets", Action: "read",
	})
	require.NoError(t, err)
	assert.False(t, resp.Allowed)
	assert.Equal(t, "no authorizer configured", resp.Reason)
}
