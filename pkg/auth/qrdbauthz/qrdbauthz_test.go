//go:build iam

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

package qrdbauthz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// ---------------------------------------------------------------------------
// Test fixtures: a real qrdb RBAC decision over an in-memory role service.
//
// These exercise the genuine qrdb authorizer (permission union + scope rule),
// not a stub of the decision. The role service resolves role NAMES to their
// canonical built-in permission sets exactly as the production kvRoleStore does
// (BuiltInRoles), without persistence — fast and host-clean for unit tests.
// ---------------------------------------------------------------------------

// builtinRoleService is a minimal, in-memory role.RoleServicer. The qrdb
// RBACAuthorizer only consults ResolvePermissions, so that is the production
// behavior exercised here: it resolves the canonical built-in permission set
// for a role name in the system tenant.
type builtinRoleService struct {
	tenantID string
}

func (s *builtinRoleService) ResolvePermissions(_ context.Context, roleIDs []string) ([]permission.Permission, error) {
	builtins := role.BuiltInRoles(s.tenantID)
	byName := make(map[string][]permission.Permission, len(builtins))
	byID := make(map[string][]permission.Permission, len(builtins))
	for _, r := range builtins {
		byName[r.Name] = r.Permissions
		byID[r.ID] = r.Permissions
	}
	seen := make(map[permission.Permission]struct{})
	var out []permission.Permission
	for _, id := range roleIDs {
		perms, ok := byName[id]
		if !ok {
			perms = byID[id]
		}
		for _, p := range perms {
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out, nil
}

// The remaining RoleServicer methods are unused by the RBAC authorizer's
// decision path; they are present only to satisfy the interface.
func (s *builtinRoleService) CreateRole(context.Context, *role.CreateRoleRequest) (*role.Role, error) {
	return nil, errors.New("not implemented")
}
func (s *builtinRoleService) GetRole(context.Context, string) (*role.Role, error) {
	return nil, errors.New("not implemented")
}
func (s *builtinRoleService) GetRoleByName(context.Context, string, string) (*role.Role, error) {
	return nil, errors.New("not implemented")
}
func (s *builtinRoleService) UpdateRole(context.Context, *role.UpdateRoleRequest) (*role.Role, error) {
	return nil, errors.New("not implemented")
}
func (s *builtinRoleService) DeleteRole(context.Context, string) error {
	return errors.New("not implemented")
}
func (s *builtinRoleService) ListRoles(context.Context, *role.ListRolesRequest) (*role.ListRolesResponse, error) {
	return nil, errors.New("not implemented")
}

var _ role.RoleServicer = (*builtinRoleService)(nil)

// realRBACClient wraps a real rbac.RBACAuthorizer behind the transport.RBACClient
// contract, performing the identical SDK-identity -> domain-identity conversion
// the embedded transport performs in production. The decision itself is the
// genuine qrdb authorizer.
type realRBACClient struct {
	authz *rbac.RBACAuthorizer
}

func (c *realRBACClient) Authorize(ctx context.Context, id *qrdbtransport.Identity, resource, action string) error {
	if id == nil {
		return &qrdbtransport.InvalidIdentityError{Reason: "identity is nil"}
	}
	if id.Subject == "" {
		return &qrdbtransport.InvalidIdentityError{Reason: "subject is empty"}
	}
	if id.TenantID == "" {
		return &qrdbtransport.InvalidIdentityError{Reason: "tenantID is empty"}
	}
	return c.authz.Authorize(ctx, &qrdbauth.Identity{
		Subject:  id.Subject,
		TenantID: id.TenantID,
		Roles:    append([]string(nil), id.Roles...),
	}, resource, action)
}

func (c *realRBACClient) ListPermissions(context.Context, *qrdbtransport.Identity) ([]qrdbtransport.PermissionInfo, error) {
	return nil, errors.New("not implemented")
}

var _ qrdbtransport.RBACClient = (*realRBACClient)(nil)

// newRealRBACResolver returns a resolver that yields a real qrdb RBAC client
// backed by the built-in role permission sets for the system tenant.
func newRealRBACResolver() RBACClientFunc {
	svc := &builtinRoleService{tenantID: qrdbtenant.SystemTenantID}
	authz := rbac.New(svc)
	client := &realRBACClient{authz: authz}
	return func() qrdbtransport.RBACClient { return client }
}

// adminSubject returns an admin identity in the system tenant.
func adminSubject() Subject {
	return Subject{
		ID:       "admin",
		TenantID: qrdbtenant.SystemTenantID,
		Roles:    []string{role.RoleNameAdmin},
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNew_NilResolver(t *testing.T) {
	_, err := New(nil, nil)
	require.Error(t, err)
	var nilErr *NilRBACResolverError
	assert.ErrorAs(t, err, &nilErr)
}

func TestNew_Succeeds(t *testing.T) {
	a, err := New(newRealRBACResolver(), nil)
	require.NoError(t, err)
	require.NotNil(t, a)
}

// ---------------------------------------------------------------------------
// RBAC decision (real qrdb PDP)
// ---------------------------------------------------------------------------

func TestAuthorize_Allowed(t *testing.T) {
	a, err := New(newRealRBACResolver(), nil)
	require.NoError(t, err)

	// admin holds keys:get in its built-in permission set.
	err = a.Authorize(context.Background(), adminSubject(), "keys", "get")
	require.NoError(t, err, "admin must be allowed keys:get")
}

func TestAuthorize_DeniedUnpermitted(t *testing.T) {
	a, err := New(newRealRBACResolver(), nil)
	require.NoError(t, err)

	// admin does NOT hold keys:sign — the qrdb authorizer must deny.
	err = a.Authorize(context.Background(), adminSubject(), "keys", "sign")
	require.Error(t, err, "admin must be denied keys:sign")

	var denied *rbac.PermissionDeniedError
	assert.ErrorAs(t, err, &denied, "denial must be qrdb's typed PermissionDeniedError")
}

func TestAuthorize_MissingTenant(t *testing.T) {
	a, err := New(newRealRBACResolver(), nil)
	require.NoError(t, err)

	subject := adminSubject()
	subject.TenantID = ""
	err = a.Authorize(context.Background(), subject, "keys", "get")
	require.Error(t, err)
	var missing *MissingTenantError
	assert.ErrorAs(t, err, &missing)
}

func TestAuthorize_PDPUnavailable(t *testing.T) {
	// Resolver yields nil — the PDP is not reachable. The adapter must fail
	// closed with a typed error, never silently allow.
	a, err := New(func() qrdbtransport.RBACClient { return nil }, nil)
	require.NoError(t, err)

	err = a.Authorize(context.Background(), adminSubject(), "keys", "get")
	require.Error(t, err)
	var unavailable *PDPUnavailableError
	assert.ErrorAs(t, err, &unavailable)
}

// TestAuthorize_TargetTenantFromContext verifies the adapter threads the
// request's target tenant (iam.TenantFromContext) onto the context so qrdb's
// scope rule resolves against it. A tenant-scoped (ScopeTenantOnly) permission
// granted to a tenant admin is allowed only when the target tenant matches the
// admin's own tenant.
func TestAuthorize_TargetTenantFromContext(t *testing.T) {
	const tenantID = "acme"
	svc := &builtinRoleService{tenantID: tenantID}
	authz := rbac.New(svc)
	client := &realRBACClient{authz: authz}
	a, err := New(func() qrdbtransport.RBACClient { return client }, nil)
	require.NoError(t, err)

	subject := Subject{ID: "acme-admin", TenantID: tenantID, Roles: []string{role.RoleNameAdmin}}

	// Target tenant matches the admin's own tenant: tenant_barrier:init (a
	// ScopeTenantOnly permission in the Admin set) is allowed.
	ctx := iam.ContextWithTenant(context.Background(), tenantID)
	err = a.Authorize(ctx, subject, "tenant_barrier", "init")
	require.NoError(t, err, "tenant admin must init its own tenant's barrier")

	// Target tenant is a DIFFERENT tenant: the ScopeTenantOnly rule denies.
	ctxOther := iam.ContextWithTenant(context.Background(), "other")
	err = a.Authorize(ctxOther, subject, "tenant_barrier", "init")
	require.Error(t, err, "tenant admin must not init another tenant's barrier")
}

// ---------------------------------------------------------------------------
// ABAC overlay (deny-overrides)
// ---------------------------------------------------------------------------

// newDenyEvaluator builds a real PolicyEvaluator whose single compiled policy
// denies (resource, action) for the given tenant. It exercises the genuine ABAC
// compile + evaluate path.
func newDenyEvaluator(t *testing.T, tenantID, resource, action string) *iam.PolicyEvaluator {
	t.Helper()
	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{TTL: time.Hour, BloomFilterSize: 1024, BloomFilterHashes: 4})

	policy := &iam.Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   iam.PolicyMetadata{Name: "deny-" + resource + "-" + action, Tenant: tenantID},
		Spec: iam.PolicySpec{
			Subjects: iam.SubjectSelector{Any: true},
			Rules: []iam.Rule{
				{
					Name:      "deny-rule",
					Effect:    iam.EffectDeny,
					Actions:   []string{action},
					Resources: []string{resource},
				},
			},
		},
	}
	compiled, err := compiler.Compile(policy)
	require.NoError(t, err)
	cache.Set(tenantID, compiled)
	return iam.NewPolicyEvaluator(compiler, cache)
}

func TestAuthorize_ABACDenyOverridesAllow(t *testing.T) {
	evaluator := newDenyEvaluator(t, qrdbtenant.SystemTenantID, "keys", "get")
	a, err := New(newRealRBACResolver(), evaluator)
	require.NoError(t, err)

	// RBAC allows keys:get for admin, but the ABAC deny policy overrides it.
	err = a.Authorize(context.Background(), adminSubject(), "keys", "get")
	require.Error(t, err)
	var denied *AuthorizationDeniedError
	assert.ErrorAs(t, err, &denied)
}

func TestAuthorize_ABACNoMatchPreservesAllow(t *testing.T) {
	// A deny policy that targets a DIFFERENT action must not deny keys:get —
	// "no matching allow policy" carries no opinion and preserves the RBAC allow.
	evaluator := newDenyEvaluator(t, qrdbtenant.SystemTenantID, "keys", "delete")
	a, err := New(newRealRBACResolver(), evaluator)
	require.NoError(t, err)

	err = a.Authorize(context.Background(), adminSubject(), "keys", "get")
	require.NoError(t, err, "ABAC overlay must not deny when no deny policy matches")
}
