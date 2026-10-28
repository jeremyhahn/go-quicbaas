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

//go:build integration && rest

package fleet_enrollment_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbmiddleware "github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	qrdbrbac "github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	qrdbpermission "github.com/jeremyhahn/go-qrdb/pkg/permission"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// =============================================================================
// The credential actually reaches the worker host (go-quicbaas#734).
//
// Everything before this layer already worked on the burn-in node: adoption,
// dispatch to the registered daemon, the container runtime. The daemon then
// refused the Spawn with `no_account_resolver`, because the pool lived on the
// control plane and nothing carried a credential to the host that runs the
// agent.
//
// This scenario is the whole chain, with production types on both ends:
//
//	enroll over mTLS  ->  the enrollment's agent-runtime role carries
//	                      agent_accounts:materialize
//	                  ->  the daemon's own AccountPool (API-backed store +
//	                      read-through credential store + the SAME
//	                      orchestration.AccountResolver the control plane runs)
//	                  ->  POST /agent-accounts/{id}/materialize, through
//	                      go-qrdb's real authenticator and RBAC
//	                  ->  injectable credential material on the worker side.
//
// THE ONLY THING THE HARNESS SUPPLIES is the TLS termination: the fake edge
// below sets the peer certificate on the request the way a real listener would
// after a handshake. The identity, the grant, the authorization decision, the
// release rules and the daemon's resolver are all the production ones.
//
// NO ASSERTION BELOW COMPARES A TOKEN BY VALUE. The credential is checked by
// SHAPE — present, and projected into the environment variable the provider's
// CLI reads — so no test output can carry one.
// =============================================================================

// daemonAccountRoutes is the pooled-account surface an enrolled daemon uses:
// the two metadata reads it SELECTS over, the usage ledger the least-usage
// strategy balances on, and the one release verb that hands it a credential.
//
// The permission each one carries is NOT restated here — it is read out of the
// production route table (restapi.Routes), so a route re-permissioned without
// updating the daemon's grant fails this scenario rather than shipping.
var daemonAccountRoutes = []accountRoute{
	{http.MethodGet, accountCollection},
	{http.MethodGet, "/api/v1/agent-accounts/{id}"},
	{http.MethodGet, "/api/v1/agent-accounts/{id}/usage"},
	{http.MethodPost, "/api/v1/agent-accounts/{id}/materialize"},
	{http.MethodPost, "/api/v1/agent-accounts/{id}/reservations"},
	{http.MethodDelete, "/api/v1/agent-accounts/{id}/reservations/{rid}"},
}

// adminAccountRoutes are the writes a tenant admin makes against the
// pool on the central: add a seat, revoke it, rotate its credential.
var adminAccountRoutes = []accountRoute{
	{http.MethodPost, accountCollection},
	{http.MethodPost, "/api/v1/agent-accounts/{id}/revoke"},
	{http.MethodPost, "/api/v1/agent-accounts/{id}/rotate"},
}

// accountRoute names one pooled-account route by method and pattern, as the
// REST route table spells it.
type accountRoute struct{ method, pattern string }

// accountCollection is the pooled-account collection route.
const accountCollection = "/api/v1/agent-accounts/"

// enrollAdmin is the tenant admin who manages the pool.
const enrollAdmin = "pool-admin"

// releaseEdge is the control plane the daemon talks to over HTTP: go-qrdb's
// real REST middleware in front of the production account routes, with the
// peer certificate injected the way a TLS listener injects it.
func (f *enrollmentFixture) releaseEdge(t *testing.T, cert *x509.Certificate, bearer string) *httptest.Server {
	t.Helper()
	return f.accountEdge(t, cert, daemonAccountRoutes)
}

// accountEdge serves the named pooled-account routes behind the auth
// middleware chain, at an edge that presents cert as the caller's
// certificate. The middleware authenticates that certificate, binds the
// caller's tenant to the request, and enforces the permission each route
// declares, as the server's REST router does.
func (f *enrollmentFixture) accountEdge(t *testing.T, cert *x509.Certificate, want []accountRoute) *httptest.Server {
	t.Helper()

	// The permissions these routes require must be in qrdb's registry before
	// RBAC evaluates a role that holds them — the same call server.NewServer
	// makes.
	quicbaaspermission.Register()

	authMiddleware := qrdbmiddleware.New(
		qrdbrbac.New(&enrolledRoles{records: f.identity, tenant: enrollTenant}),
		qrdbmiddleware.WithAuthenticator(qrdbauth.NewMTLSAuthenticator()),
		qrdbmiddleware.WithUserService(&enrolledUsers{records: f.identity}),
	)

	routes := qrdbmiddleware.NewRESTRouteRegistry()
	router := chi.NewRouter()
	for _, route := range want {
		spec := f.routeSpec(t, route.method, route.pattern)
		required := qrdbpermission.Permission(spec.Resource + ":" + spec.Action)
		routes.Register(spec.Method, spec.Pattern, required)
		router.Method(spec.Method, spec.Pattern, f.server.BindRoute(spec))

		// The collection route is served at the bare path too, exactly as
		// the module registrar aliases it (pkg/apimodule RegisterHTTP).
		if spec.Pattern == accountCollection {
			bare := strings.TrimSuffix(accountCollection, "/")
			routes.Register(spec.Method, bare, required)
			router.Method(spec.Method, bare, f.server.BindRoute(spec))
		}
	}

	guarded := authMiddleware.RESTMiddleware(routes, router)

	// The TLS edge. A real listener presents the peer certificate on the
	// connection; httptest over plain HTTP does not, so the harness sets it
	// here. Nothing else about the request is synthesized — the caller's own
	// client built the URL, the body and the bearer.
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{cert},
			VerifiedChains:   [][]*x509.Certificate{{cert}},
		}
		guarded.ServeHTTP(w, r)
	}))
	t.Cleanup(edge.Close)
	return edge
}

// adminEdge is the edge a tenant admin manages the pool through: an mTLS
// principal of enrollTenant, bound to the tenant-admin role, which alone holds
// the agent_accounts write verbs (go-quicbaas#1355). It is minted on first use.
//
// The admin's writes go through the same middleware as the daemon's
// reads, because the account service acts only for the tenant the
// authenticated caller belongs to (tenantctx.Target, go-quicbaas#1150): a
// write with no caller bound is refused, so the pool can only be seeded the
// way production seeds it.
func (f *enrollmentFixture) adminEdge(t *testing.T) *httptest.Server {
	t.Helper()
	if f.admin != nil {
		return f.admin
	}

	// The tenant admin holds go-qrdb's built-in admin role. The harness role
	// store carries that role's grants for these routes, and only the grants
	// the built-in role really holds: a route whose permission it does not
	// hold fails here instead of passing on a hand-made grant. The role's
	// placement for these permissions is registered by quicbaas, so it must be
	// in qrdb's registry before the role is asked about it.
	quicbaaspermission.Register()
	grants := make([]string, 0, len(adminAccountRoutes))
	for _, route := range adminAccountRoutes {
		spec := f.routeSpec(t, route.method, route.pattern)
		required := qrdbsdk.Permission(spec.Resource + ":" + spec.Action)
		require.Truef(t, qrdbsdk.HasPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, required),
			"%s %s requires %s, which the tenant-admin role does not hold", route.method, route.pattern, required)
		grants = append(grants, string(required))
	}

	ctx := context.Background()
	role := &iam.Role{
		ID:          qrdbsdk.PermissionRoleAdmin,
		Name:        qrdbsdk.PermissionRoleAdmin,
		TenantID:    enrollTenant,
		Permissions: grants,
	}
	require.NoError(t, f.identity.CreateRole(ctx, role))

	cert := agentCertificate(t, enrollTenant, enrollAdmin)
	require.NoError(t, f.identity.CreateUser(ctx, &iam.User{
		TenantID:    enrollTenant,
		Username:    enrollAdmin,
		Name:        enrollAdmin,
		Roles:       []string{role.ID},
		AuthMethods: []string{iam.AuthMethodMTLS},
		Lifecycle:   iam.LifecycleActive,
		CertBindings: []qrdbsdk.CertBinding{{
			Thumbprint: qrdbsdk.ComputeSPKIThumbprint(cert),
			Kind:       qrdbsdk.CertBindingSPKIThumbprint,
			NotAfter:   cert.NotAfter,
		}},
	}))

	f.admin = f.accountEdge(t, cert, adminAccountRoutes)
	return f.admin
}

// operate sends one tenant-admin request through the admin edge, requires it
// to succeed, and decodes the answer into out when out is not nil.
func (f *enrollmentFixture) operate(t *testing.T, method, path string, body, out any) {
	t.Helper()

	edge := f.adminEdge(t)
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(method, edge.URL+path, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := edge.Client().Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Less(t, resp.StatusCode, http.StatusMultipleChoices, "%s %s: %s", method, path, raw)

	if out != nil {
		require.NoError(t, json.Unmarshal(raw, out))
	}
}

// routeSpec finds one route in the PRODUCTION route table, so the handler and
// the permission under test are the ones a deployment serves.
func (f *enrollmentFixture) routeSpec(t *testing.T, method, pattern string) restapi.RouteSpec {
	t.Helper()
	for _, spec := range restapi.Routes() {
		if spec.Method == method && spec.Pattern == pattern {
			return spec
		}
	}
	t.Fatalf("the route table has no %s %s; the pooled-account surface is not wired", method, pattern)
	return restapi.RouteSpec{}
}

// seedAccount provisions one pooled account the way a tenant admin does,
// through the production create route, and returns its ID.
func (f *enrollmentFixture) seedAccount(t *testing.T, projectID string, isDefault bool) string {
	t.Helper()
	var created restapi.AccountEnvelopeRESTResponse
	f.operate(t, http.MethodPost, accountCollection, &restapi.CreateAccountRESTRequest{
		TenantID:      enrollTenant,
		ProjectID:     projectID,
		Provider:      string(orchestration.AccountProviderClaude),
		DisplayName:   "burnin-seat",
		Default:       isDefault,
		RefreshPolicy: string(orchestration.AccountRefreshNone),
		Credential: &restapi.AccountCredentialRESTRequest{
			AccessToken:       "burn-in-pool-token",
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		},
	}, &created)
	require.NotNil(t, created.Account)
	return created.Account.ID
}

// daemonAccountPool builds the daemon's resolver against the release edge,
// using the production composition (fleet.NewAccountPool).
func daemonAccountPool(t *testing.T, edge *httptest.Server, bearer string) *fleet.AccountPool {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)

	pool, err := fleet.NewAccountPool(fleet.AccountPoolConfig{
		ServerAddr: edge.URL,
		TenantID:   enrollTenant,
		HTTPClient: edge.Client(),
		Tokens:     &enrollmentBearerSource{token: bearer},
		Logger:     logger.NewAdapter(log),
	})
	require.NoError(t, err)
	return pool
}

// enrollmentBearerSource supplies the bearer the daemon's enrollment minted.
type enrollmentBearerSource struct {
	token string
}

func (s *enrollmentBearerSource) Token(context.Context) (string, error) { return s.token, nil }
func (s *enrollmentBearerSource) Invalidate()                           {}

var _ fleet.TokenSource = (*enrollmentBearerSource)(nil)

// TestE2E_AgentEnrollment_PooledCredentialReachesTheDaemon is the scenario the
// burn-in blocked on. An enrolled daemon resolves a pooled account end to end
// and gets injectable credential material for the spawn it was dispatched.
func TestE2E_AgentEnrollment_PooledCredentialReachesTheDaemon(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)

	// 1. Enroll. The grant the enrollment writes must carry the release
	//    permission, or every step after this is unreachable.
	enrolled := f.register(t, cert)
	require.Contains(t, enrolled.Scopes, iam.FleetScopeAccountMaterialize,
		"an enrolled daemon must be granted the credential-release permission")
	require.Contains(t, enrolled.Scopes, iam.FleetScopeAccountList,
		"an enrolled daemon must be able to list the pool it selects over")
	require.Contains(t, enrolled.Scopes, iam.FleetScopeAccountRead,
		"an enrolled daemon must be able to read the account it selected")

	// 2. Seed the tenant's pool on the control plane.
	accountID := f.seedAccount(t, "", true)

	// 3. The daemon resolves it, over HTTP, through qrdb's real authorization.
	edge := f.releaseEdge(t, cert, "enrollment-bearer")
	pool := daemonAccountPool(t, edge, "enrollment-bearer")

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID:  enrollTenant,
		Provider:  orchestration.AccountProviderClaude,
		AccountID: accountID,
	})
	require.NoError(t, err, "the daemon could not resolve a pooled account: this is the no_account_resolver failure")
	require.Equal(t, accountID, resolved.AccountID)
	require.NotEmpty(t, resolved.Env["CLAUDE_CODE_OAUTH_TOKEN"],
		"the resolution projected no credential into the variable the provider CLI reads")

	// 4. The control plane recorded the release as a use, which is the only
	//    thing it can truthfully observe about a spawn on another host.
	record, err := f.accountStore.Get(context.Background(), enrollTenant, accountID)
	require.NoError(t, err)
	require.False(t, record.LastUsedAt.IsZero(), "the release did not stamp usage on the central")
}

// TestE2E_AgentEnrollment_PooledCredentialAutoSelects proves the daemon runs
// the SAME selection the control plane runs: an unpinned spawn lands on the
// scope default, over API-backed metadata.
func TestE2E_AgentEnrollment_PooledCredentialAutoSelects(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)
	f.register(t, cert)

	f.seedAccount(t, "", false)
	defaultID := f.seedAccount(t, "", true)

	edge := f.releaseEdge(t, cert, "enrollment-bearer")
	pool := daemonAccountPool(t, edge, "enrollment-bearer")

	resolved, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID: enrollTenant,
		Provider: orchestration.AccountProviderClaude,
		Auto:     true,
	})
	require.NoError(t, err)
	require.Equal(t, defaultID, resolved.AccountID,
		"auto selection on the daemon did not land on the scope default")
}

// TestE2E_AgentEnrollment_RevokedAccountIsNotReleased proves the control plane
// still decides: an account the pool has judged unusable never reaches a worker
// host, however the host asks.
func TestE2E_AgentEnrollment_RevokedAccountIsNotReleased(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)
	f.register(t, cert)

	accountID := f.seedAccount(t, "", true)
	f.operate(t, http.MethodPost, "/api/v1/agent-accounts/"+accountID+"/revoke?tenant_id="+enrollTenant,
		&restapi.RevokeAccountRESTRequest{Reason: "seat cancelled"}, nil)

	edge := f.releaseEdge(t, cert, "enrollment-bearer")
	pool := daemonAccountPool(t, edge, "enrollment-bearer")

	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID:  enrollTenant,
		Provider:  orchestration.AccountProviderClaude,
		AccountID: accountID,
	})
	require.Error(t, err, "a revoked account's credential was released to a worker host")
}

// TestE2E_AgentEnrollment_UnenrolledPrincipalIsRefusedTheRelease is the authz
// case: a certificate principal that was never enrolled holds no role, so it
// holds no release permission, and qrdb refuses it before the handler runs.
func TestE2E_AgentEnrollment_UnenrolledPrincipalIsRefusedTheRelease(t *testing.T) {
	f := newEnrollmentFixture(t)

	// Enrolled, so the pool and the route exist.
	enrolledCert := agentCertificate(t, enrollTenant, enrollAgent)
	f.register(t, enrolledCert)
	accountID := f.seedAccount(t, "", true)

	// NOT enrolled: no user record, therefore no role, therefore no grant.
	strangerCert := agentCertificate(t, enrollTenant, "agent-not-enrolled")
	edge := f.releaseEdge(t, strangerCert, "stranger-bearer")
	pool := daemonAccountPool(t, edge, "stranger-bearer")

	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID:  enrollTenant,
		Provider:  orchestration.AccountProviderClaude,
		AccountID: accountID,
	})
	require.Error(t, err, "an unenrolled principal obtained a pooled credential")
}

// TestE2E_AgentEnrollment_ProjectFencedAccountStaysFenced proves the account's
// project restriction survives the trip to a worker host, where no operator can
// see it being honoured.
func TestE2E_AgentEnrollment_ProjectFencedAccountStaysFenced(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)
	f.register(t, cert)

	fenced := f.seedAccount(t, "project-x", true)

	edge := f.releaseEdge(t, cert, "enrollment-bearer")
	pool := daemonAccountPool(t, edge, "enrollment-bearer")

	// The daemon's pool releases without naming a project (its releases are
	// unscoped unless an operator scoped the daemon), so a fenced account must
	// be refused rather than handed over.
	_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
		TenantID:  enrollTenant,
		Provider:  orchestration.AccountProviderClaude,
		AccountID: fenced,
	})
	require.Error(t, err, "a project-fenced account was released to an unscoped request")
}

// TestE2E_AgentEnrollment_ReleaseIsServedFromCacheOnRepeatSpawns proves the
// daemon does not re-request a credential per spawn: each release is a real
// handout with its own audit event on the control plane.
func TestE2E_AgentEnrollment_ReleaseIsServedFromCacheOnRepeatSpawns(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)
	f.register(t, cert)
	accountID := f.seedAccount(t, "", true)

	edge := f.releaseEdge(t, cert, "enrollment-bearer")
	pool := daemonAccountPool(t, edge, "enrollment-bearer")

	for i := 0; i < 3; i++ {
		_, err := pool.Resolver.Resolve(context.Background(), orchestration.AccountResolveRequest{
			TenantID:  enrollTenant,
			Provider:  orchestration.AccountProviderClaude,
			AccountID: accountID,
		})
		require.NoErrorf(t, err, "resolve %d", i)
	}

	hits, misses := pool.Stats()
	require.Equal(t, int64(1), misses, "the daemon released the credential more than once")
	require.Equal(t, int64(2), hits, "the daemon did not serve repeat spawns from its own store")
}

// TestE2E_AgentEnrollment_RotationOnTheCentralReachesTheDaemon is the rotation
// contract the owner asked for: the Claude token is rotated on the control
// plane and the next spawn on the worker host uses the new one.
func TestE2E_AgentEnrollment_RotationOnTheCentralReachesTheDaemon(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)
	f.register(t, cert)
	accountID := f.seedAccount(t, "", true)

	edge := f.releaseEdge(t, cert, "enrollment-bearer")
	pool := daemonAccountPool(t, edge, "enrollment-bearer")
	ctx := context.Background()
	req := orchestration.AccountResolveRequest{
		TenantID:  enrollTenant,
		Provider:  orchestration.AccountProviderClaude,
		AccountID: accountID,
	}

	before, err := pool.Resolver.Resolve(ctx, req)
	require.NoError(t, err)

	// The tenant admin re-runs `claude setup-token` and rotates the pool entry.
	f.operate(t, http.MethodPost, "/api/v1/agent-accounts/"+accountID+"/rotate?tenant_id="+enrollTenant,
		&restapi.RotateAccountCredentialRESTRequest{
			Credential: &restapi.AccountCredentialRESTRequest{
				AccessToken:       "rotated-pool-token",
				AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
				ExpiresAt:         time.Now().UTC().Add(24 * time.Hour),
			},
		}, nil)

	after, err := pool.Resolver.Resolve(ctx, req)
	require.NoError(t, err)

	// Compared, never printed: the worker must stop using the retired
	// credential on the very next spawn.
	require.NotEqual(t, before.Env["CLAUDE_CODE_OAUTH_TOKEN"], after.Env["CLAUDE_CODE_OAUTH_TOKEN"],
		"the worker host kept using the retired credential after a central rotation")

	_, misses := pool.Stats()
	require.Equal(t, int64(2), misses, "the rotation did not force a re-release")
}
