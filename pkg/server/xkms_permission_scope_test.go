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
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/permscope"
)

// ---------------------------------------------------------------------------
// The composed go-xkms vocabulary must be scope-declared, or nobody logs in
//
// go-qrdb#504 removed the implicit self-scope: the authorizer refuses a
// permission with no declared scope, and rbac.ListPermissions fails on the
// FIRST unscoped permission an identity holds. That call is the token-issuance
// path (iam.TokenService.resolveEffectivePermissions), so one undeclared
// permission does not deny one request — it denies login for every identity
// whose roles reach it.
//
// go-xkms v0.14.0-alpha contributed its whole gRPC/REST/MCP vocabulary to
// qrdb's registry and declared no scope for any of it. Four names were left
// undeclared — keys:read, keys:update, keys:write, ssh:use — and on
// go-quicbaas v0.11.0-alpha every login for a role carrying keys:read was
// rejected with "permission: no scope declared for keys:read". The fix is
// go-xkms's, in its own module (go-xkms#617 / PR #618); these tests are what
// keeps this composition root from regressing once it consumes the fix.
// ---------------------------------------------------------------------------

// xkmsScopedPermission is the permission the login failure was measured on. It
// is named rather than derived: a derived list would pass over the exact name
// the outage was reported under if the derivation ever changed.
const xkmsScopedPermission = qrdbsdk.Permission("keys:read")

// composeXKMSServiceModule performs the same construction the composition root
// does in qrdb_node.go — xkmssdk.NewServiceModule — which is what contributes
// go-xkms's vocabulary, and its scope declarations, to qrdb's process-global
// registry. The service resolver yields nil because no test here calls a
// keystore RPC; the module accepts a resolver that has not produced a service
// yet by design (its RPCs answer Unavailable until one does).
func composeXKMSServiceModule(t *testing.T) {
	t.Helper()
	if _, err := xkmssdk.NewServiceModule(func() *xkmssdk.XKMSService { return nil }); err != nil {
		t.Fatalf("compose the go-xkms service module: %v", err)
	}
}

// ---------------------------------------------------------------------------
// A scope-resolving RBAC client
// ---------------------------------------------------------------------------

// errAuthorizeOutsideFixture reports an Authorize call this fixture makes no
// claim about. ListPermissions is the issuance path under test; answering
// Authorize with a bare nil would let a future test believe an admission
// decision was made here when none was.
var errAuthorizeOutsideFixture = errors.New("server: this RBAC fixture answers ListPermissions only")

// scopeResolvingRBAC is a qrdb RBAC client that reproduces the ONE contract
// this test is about: go-qrdb's embedded rbacClient.ListPermissions projects
// every resolved permission through permissionToTransport, which calls
// LookupScope and RETURNS ITS ERROR — so the first permission with no declared
// scope fails the whole call, and with it the token.
//
// The role-to-permission map is supplied by the test, because who holds what
// is not the property under test. The SCOPE side is not doubled at all: it
// resolves through permscope, which reads the real process-global declaration
// layer via the SDK's tenant scope store — the same resolution the authorizer
// performs. A double that answered scopes from its own table would agree with
// itself and prove nothing about what go-xkms declared.
type scopeResolvingRBAC struct {
	resolver *permscope.Resolver
	byRole   map[string][]qrdbsdk.Permission
}

func newScopeResolvingRBAC(t *testing.T, byRole map[string][]qrdbsdk.Permission) *scopeResolvingRBAC {
	t.Helper()
	resolver, err := permscope.New()
	if err != nil {
		t.Fatalf("permscope.New: %v", err)
	}
	return &scopeResolvingRBAC{resolver: resolver, byRole: byRole}
}

// Authorize implements qrdbtransport.RBACClient.
func (r *scopeResolvingRBAC) Authorize(context.Context, *qrdbtransport.Identity, string, string) error {
	return errAuthorizeOutsideFixture
}

// ListPermissions implements qrdbtransport.RBACClient, mirroring go-qrdb's
// embedded implementation: resolve the identity's role permissions, sort them,
// then project each with its declared scope — failing the entire call on the
// first one that has none.
func (r *scopeResolvingRBAC) ListPermissions(ctx context.Context, identity *qrdbtransport.Identity) ([]qrdbtransport.PermissionInfo, error) {
	if identity == nil || identity.Subject == "" || identity.TenantID == "" {
		return nil, &qrdbtransport.InvalidIdentityError{Reason: "identity is nil or incomplete"}
	}

	seen := map[qrdbsdk.Permission]struct{}{}
	for _, role := range identity.Roles {
		for _, p := range r.byRole[role] {
			seen[p] = struct{}{}
		}
	}
	names := make([]qrdbsdk.Permission, 0, len(seen))
	for p := range seen {
		names = append(names, p)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	out := make([]qrdbtransport.PermissionInfo, 0, len(names))
	for _, p := range names {
		scope, err := r.resolver.Scope(ctx, p)
		if err != nil {
			return nil, err
		}
		resource, action, _ := strings.Cut(string(p), ":")
		out = append(out, qrdbtransport.PermissionInfo{
			Name:     string(p),
			Resource: resource,
			Action:   action,
			// The scope labels are the same string on both sides by
			// contract: transport.PermissionScope's doc states its values
			// are the ones permission.Scope.String produces.
			Scope: qrdbtransport.PermissionScope(scope.String()),
		})
	}
	return out, nil
}

// fixedPermissionResolver hands the token service one RBAC client, mirroring
// the late-bound serverPermissionResolver without the server lifecycle.
type fixedPermissionResolver struct {
	client qrdbtransport.RBACClient
}

// RBACClient implements iam.PermissionResolver.
func (r *fixedPermissionResolver) RBACClient() qrdbtransport.RBACClient { return r.client }

// unscopedPermissionCounter makes each test's throwaway permission name
// unique. The permission registry and the scope registry are process-global
// and a registration cannot be withdrawn, so a fixed name would be registered
// by the first run of a `-count=2` pass and inherited by the second — where it
// would prove nothing, because the second run did not put it there.
var unscopedPermissionCounter atomic.Uint64

// registerUnscopedPermission contributes a permission to qrdb's registry and
// DELIBERATELY declares no scope for it, returning its name.
//
// Its resource family is unique to this call, so it cannot collide with a
// family this repository declares — TestNewServer_RegistersOnlyTheDeclaredPermissions
// checks exactly that, by family.
func registerUnscopedPermission(t *testing.T) qrdbsdk.Permission {
	t.Helper()
	p := qrdbsdk.Permission(fmt.Sprintf("quicbaasunscoped%d:read", unscopedPermissionCounter.Add(1)))
	qrdbsdk.RegisterPermissions(p)
	return p
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestComposedXKMSVocabulary_DeclaresAScopeForKeysRead is the narrowest
// statement of the defect: after the composition root builds the go-xkms
// service module, the permission the outage was reported on must resolve a
// scope. Under go-xkms v0.14.0-alpha it resolves none, and everything below
// follows from that.
func TestComposedXKMSVocabulary_DeclaresAScopeForKeysRead(t *testing.T) {
	composeXKMSServiceModule(t)

	resolver, err := permscope.New()
	if err != nil {
		t.Fatalf("permscope.New: %v", err)
	}

	scope, err := resolver.Scope(context.Background(), xkmsScopedPermission)
	if err != nil {
		t.Fatalf("the composed go-xkms vocabulary leaves %q without a declared scope (%v); "+
			"qrdb's authorizer refuses it and token issuance fails for every identity holding it — "+
			"see go-xkms#617", xkmsScopedPermission, err)
	}
	if scope != qrdbsdk.PermissionScopeSelf {
		t.Errorf("Scope(%q) = %v, want %v: self-scope is the reach it had before go-qrdb#504 removed the implicit default",
			xkmsScopedPermission, scope, qrdbsdk.PermissionScopeSelf)
	}
}

// TestIssueSessionToken_ResolvesPermissionsForAScopedXKMSVerb is the
// regression: an identity whose role carries keys:read obtains its effective
// permissions and the bearer carries them in scp.
//
// It runs the real issuance path — OIDCProvider.IssueSessionToken joins the
// IAM record, mints claims, and TokenService.GenerateAccessToken resolves the
// scp claim through resolveEffectivePermissions. Only the role-to-permission
// mapping is supplied by the fixture; the scope every resolved permission is
// projected with comes from the process-global declaration layer go-xkms
// contributed to.
func TestIssueSessionToken_ResolvesPermissionsForAScopedXKMSVerb(t *testing.T) {
	composeXKMSServiceModule(t)

	const role = "xkms-key-reader"
	rbac := newScopeResolvingRBAC(t, map[string][]qrdbsdk.Permission{
		role: {xkmsScopedPermission},
	})
	provider, users := newTestOIDCProviderWithResolver(t, &fixedPermissionResolver{client: rbac})
	seedSessionUser(t, users, "acme", "key-reader", []string{role})

	token, _, err := issueSessionTokenForTest(t, provider, "acme", "key-reader")
	if err != nil {
		t.Fatalf("IssueSessionToken: %v — an identity holding %q could not obtain a token, which is the login outage go-xkms#617 describes",
			err, xkmsScopedPermission)
	}

	scp := scpClaimOf(t, provider, token)
	if !slices.Contains(scp, string(xkmsScopedPermission)) {
		t.Errorf("scp = %v, want it to carry %q: the bearer must report the authority its role actually grants",
			scp, xkmsScopedPermission)
	}
}

// TestIssueSessionToken_RefusesAnIdentityHoldingAnUnscopedPermission is the
// other half, and the one that keeps the fix from becoming a fail-open: the
// scope requirement must still bite. An identity holding a permission that is
// registered but whose tenancy reach nobody declared gets no token at all —
// not a token minus that permission.
//
// Dropping the offending permission and issuing anyway would be the tempting
// repair, and it is the wrong one: the holder's authority would then be
// silently narrower than their roles say, and the undeclared permission would
// stay undeclared with nothing failing. go-qrdb fails the whole call; so must
// this path.
func TestIssueSessionToken_RefusesAnIdentityHoldingAnUnscopedPermission(t *testing.T) {
	composeXKMSServiceModule(t)

	unscoped := registerUnscopedPermission(t)
	const role = "holder-of-an-unscoped-verb"
	rbac := newScopeResolvingRBAC(t, map[string][]qrdbsdk.Permission{
		// Both, so the failure cannot be "the role resolved nothing": the
		// scoped verb alone would issue fine, as the test above proves.
		role: {xkmsScopedPermission, unscoped},
	})
	provider, users := newTestOIDCProviderWithResolver(t, &fixedPermissionResolver{client: rbac})
	seedSessionUser(t, users, "acme", "unscoped-holder", []string{role})

	token, _, err := issueSessionTokenForTest(t, provider, "acme", "unscoped-holder")
	if err == nil {
		t.Fatalf("IssueSessionToken succeeded for an identity holding %q, which has no declared scope; "+
			"issuance must fail closed rather than mint a bearer over a permission nobody scoped (token=%q)",
			unscoped, token)
	}

	var undeclared *qrdbsdk.PermissionUndeclaredScopeError
	if !errors.As(err, &undeclared) {
		t.Fatalf("IssueSessionToken error = %T (%v), want it to wrap *qrdbsdk.PermissionUndeclaredScopeError naming the undeclared permission",
			err, err)
	}
	if undeclared.Permission != unscoped {
		t.Errorf("refusal names %q, want %q", undeclared.Permission, unscoped)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// issueSessionTokenForTest mints a bearer through the login-ceremony issuance
// path for a seeded principal. The auth method is recorded as mtls because the
// value is carried into the claims verbatim and an empty one would leave the
// fixture describing a login that used no factor at all.
func issueSessionTokenForTest(t *testing.T, provider *iam.OIDCProvider, tenantID, subject string) (string, time.Time, error) {
	t.Helper()
	return provider.IssueSessionToken(context.Background(), iam.SessionIdentity{
		Subject:    subject,
		TenantID:   tenantID,
		AuthMethod: "mtls",
	})
}

// scpClaimOf validates the bearer through the provider that minted it and
// returns its scp claim — the array-form list carrying the subject's
// qrdb-resolved effective permissions.
func scpClaimOf(t *testing.T, provider *iam.OIDCProvider, token string) []string {
	t.Helper()
	claims, err := provider.ValidateAccessToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	return claims.SCP
}
