//go:build rest && mcp

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

package apimodule

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
)

// =============================================================================
// go-quicbaas#901 — what a spawned agent session's certificate may actually do
// =============================================================================
//
// A spawned agent reaches the platform over MCP with a per-session mTLS
// certificate. go-qrdb authenticates that certificate and then resolves the
// principal's roles from the user record its Subject CommonName names
// (finalizeIdentity); MCP is DEFAULT-DENY (AuthorizeMCP). Nothing enrolled the
// certificate, so every session authenticated and held empty roles, and every
// tool but the public ones was refused.
//
// These tests drive go-qrdb's REAL mTLS authenticator, RBAC authorizer and MCP
// authorization gate over the record iam.EnrollAgentSessionIdentity actually
// writes, against the REAL quicbaas MCP tool table. They pin all four halves of
// the property: the session is admitted on exactly its floor, refused on
// everything else in the table, refused entirely once the session closes, and
// refused entirely when nothing enrolled it.
// =============================================================================

const (
	agentSessionTenant = "automatethethings"
	agentSessionCN     = "agent-ses-7f3a91"
)

// Canonical qrdb tool names and permissions for the key-value tools a session
// holds. They are qrdb's own MCP registrations (go-qrdb pkg/api/mcp tools_kv.go
// -> permission.KVGet / KVPut / KVList), restated here because the registry a
// unified server builds spans both servers' tables and this test asserts over
// the union a session actually faces.
const (
	qrdbGetTool  = "qrdb_get"
	qrdbPutTool  = "qrdb_put"
	qrdbListTool = "qrdb_list"
)

// agentSessionPeerCert mints a client certificate shaped exactly like the one
// AgentMCPPresetResolver issues per spawn: O=<tenant>, OU=agents,
// CN=agent-<sessionID>, one-day validity, and the tenant-scoped SPIFFE SAN
// go-qrdb derives the identity's tenant from.
func agentSessionPeerCert(t *testing.T, tenantID, commonName string) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	spiffeID, err := url.Parse("spiffe://burnin.example.internal/tenant/" +
		tenantID + "/svc/" + commonName)
	if err != nil {
		t.Fatalf("parse spiffe id: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{tenantID},
			OrganizationalUnit: []string{"agents"},
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs:        []*url.URL{spiffeID},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

// enrolledAgentSessionUser runs the REAL session enrollment and projects the
// record it wrote into go-qrdb's user type through the production converter, so
// this test is driven by what enrollment actually writes rather than by a
// hand-built approximation of it.
func enrolledAgentSessionUser(t *testing.T, cert *x509.Certificate) *user.User {
	t.Helper()

	records := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: records,
		RoleStorage: records,
	})
	enrolled, err := store.EnrollAgentSessionIdentity(t.Context(), agentSessionTenant, cert)
	if err != nil {
		t.Fatalf("EnrollAgentSessionIdentity() error = %v", err)
	}
	core, err := qrdbadapter.FromIAMUser(enrolled)
	if err != nil {
		t.Fatalf("FromIAMUser() error = %v", err)
	}
	return &core
}

// agentSessionToolRegistry builds the MCP tool registry a spawned session faces
// on the unified server: every non-public quicbaas tool with the permission the
// production table declares for it, plus qrdb's three key-value tools.
func agentSessionToolRegistry(t *testing.T) *middleware.MCPToolRegistry {
	t.Helper()

	registry := middleware.NewMCPToolRegistry()
	registered := 0
	for _, spec := range mcpapi.ModuleTools() {
		if spec.Public || spec.Resource == "" || spec.Action == "" {
			continue
		}
		registry.Register(spec.Name, permission.Permission(spec.Resource+":"+spec.Action))
		registered++
	}
	if registered == 0 {
		t.Fatal("the quicbaas MCP tool table contributed no permissioned tools; this test would prove nothing")
	}
	registry.Register(qrdbGetTool, permission.KVGet)
	registry.Register(qrdbPutTool, permission.KVPut)
	registry.Register(qrdbListTool, permission.KVList)
	return registry
}

// agentSessionMiddleware wires go-qrdb's REAL mTLS authenticator and RBAC
// authorizer over the roles the session enrollment grants, with the supplied
// user records standing in for the store.
func agentSessionMiddleware(users map[string]*user.User) *middleware.AuthMiddleware {
	grant := make([]permission.Permission, 0, len(iam.AgentSessionGrant()))
	for _, granted := range iam.AgentSessionGrant() {
		grant = append(grant, permission.Permission(granted))
	}
	roles := &fakeRoleServicer{perms: map[string][]permission.Permission{
		iam.AgentSessionRoleID(agentSessionTenant): grant,
	}}
	return middleware.New(rbac.New(roles),
		middleware.WithAuthenticator(qrdbauth.NewMTLSAuthenticator()),
		middleware.WithUserService(&fakeUserServicer{users: users}),
	)
}

// agentSessionCallContext authenticates the session's certificate exactly as the
// MCP transport's HTTP context func does, returning the context every tool
// handler would be invoked with plus the identity qrdb resolved.
func agentSessionCallContext(
	t *testing.T,
	am *middleware.AuthMiddleware,
	peer *x509.Certificate,
) (context.Context, *qrdbauth.Identity) {
	t.Helper()

	ctx := qrdbauth.WithTLSState(t.Context(), &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{peer},
		VerifiedChains:   [][]*x509.Certificate{{peer}},
	})
	authCtx, identity, err := am.Authenticate(ctx)
	if err != nil {
		t.Fatalf("the session certificate must authenticate: %v", err)
	}
	return authCtx, identity
}

// TestAgentSession_EnrolledCertificateHoldsExactlyItsFloor proves the fix end to
// end through go-qrdb's own pipeline, and proves its bound in the same pass: the
// session is admitted on the four tools its floor names and REFUSED on every
// other permissioned tool in the real table.
func TestAgentSession_EnrolledCertificateHoldsExactlyItsFloor(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{
		agentSessionTenant + "/" + agentSessionCN: enrolledAgentSessionUser(t, cert),
	})
	registry := agentSessionToolRegistry(t)
	ctx, identity := agentSessionCallContext(t, am, cert)

	// The principal every authorization decision and every audit record names is
	// the SESSION's, resolved from the certificate subject, in the tenant the
	// SPIFFE SAN binds.
	if identity.Subject != agentSessionCN {
		t.Fatalf("identity subject = %q, want the session CN %q", identity.Subject, agentSessionCN)
	}
	if identity.TenantID != agentSessionTenant {
		t.Fatalf("identity tenant = %q, want %q", identity.TenantID, agentSessionTenant)
	}
	wantRole := iam.AgentSessionRoleID(agentSessionTenant)
	if len(identity.Roles) != 1 || identity.Roles[0] != wantRole {
		t.Fatalf("identity roles = %v, want exactly [%s]", identity.Roles, wantRole)
	}

	// Admitted on exactly the floor.
	allowed := map[string]bool{
		"quicbaas_orchestrator_inference": true,
		qrdbGetTool:                       true,
		qrdbPutTool:                       true,
		qrdbListTool:                      true,
	}
	for tool := range allowed {
		if _, covered := registry.Lookup(tool); !covered {
			t.Fatalf("%s is not in the tool registry; the floor names a tool nothing enforces", tool)
		}
		if err := am.AuthorizeMCP(ctx, registry, tool, nil); err != nil {
			t.Errorf("%s must be admitted for an enrolled session: %v", tool, err)
		}
	}

	// Refused on everything else the registry covers. This is the "and nothing
	// more" half, asserted exhaustively over the production table rather than
	// against a sample.
	refused, admitted := 0, 0
	for tool := range registry.Entries() {
		if allowed[tool] {
			continue
		}
		if err := am.AuthorizeMCP(ctx, registry, tool, nil); err == nil {
			admitted++
			t.Errorf("%s is OUTSIDE the session floor and must be refused", tool)
		} else {
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("no tool outside the floor was exercised; this test would prove nothing")
	}
	if admitted != 0 {
		t.Fatalf("%d tools outside the floor were admitted", admitted)
	}
	t.Logf("session admitted on %d tools, refused on %d", len(allowed), refused)
}

// TestAgentSession_UnenrolledCertificateIsRefused reproduces the live defect and
// is the neutralization guard for the enrollment: with no record for the
// certificate's subject the principal holds no roles, and the default-deny MCP
// gate refuses everything — including the tools the floor names. Remove the
// enrollment and this is what every spawned agent gets.
func TestAgentSession_UnenrolledCertificateIsRefused(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{})
	registry := agentSessionToolRegistry(t)
	ctx, identity := agentSessionCallContext(t, am, cert)

	// It authenticates. That was never the problem.
	if identity.Subject != agentSessionCN {
		t.Fatalf("identity subject = %q, want %q", identity.Subject, agentSessionCN)
	}
	if len(identity.Roles) != 0 {
		t.Fatalf("an unenrolled certificate must hold no roles, got %v", identity.Roles)
	}

	for _, tool := range []string{"quicbaas_orchestrator_inference", qrdbGetTool, qrdbPutTool, qrdbListTool} {
		if err := am.AuthorizeMCP(ctx, registry, tool, nil); err == nil {
			t.Errorf("%s was admitted for an unenrolled certificate", tool)
		}
	}
}

// TestAgentSession_RevokedCertificateCannotAct proves the principal cannot act
// after the session closes. Revocation deletes the record, so the very same
// certificate — still valid, still trusted, still presented — resolves to no
// roles and is refused everything.
func TestAgentSession_RevokedCertificateCannotAct(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)

	records := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: records,
		RoleStorage: records,
	})
	enrolled, err := store.EnrollAgentSessionIdentity(t.Context(), agentSessionTenant, cert)
	if err != nil {
		t.Fatalf("EnrollAgentSessionIdentity: %v", err)
	}
	core, err := qrdbadapter.FromIAMUser(enrolled)
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}

	// The live store the middleware reads through, so revocation is visible to
	// it the way it is in production.
	users := map[string]*user.User{agentSessionTenant + "/" + agentSessionCN: &core}
	am := agentSessionMiddleware(users)
	registry := agentSessionToolRegistry(t)

	ctx, _ := agentSessionCallContext(t, am, cert)
	if err := am.AuthorizeMCP(ctx, registry, qrdbGetTool, nil); err != nil {
		t.Fatalf("precondition: an open session must be admitted on %s: %v", qrdbGetTool, err)
	}

	if err := store.RevokeAgentSessionIdentity(t.Context(), agentSessionTenant, agentSessionCN); err != nil {
		t.Fatalf("RevokeAgentSessionIdentity: %v", err)
	}
	if _, err := records.GetUserByUsername(t.Context(), agentSessionTenant, agentSessionCN); err == nil {
		t.Fatal("the session principal survived revocation")
	}
	delete(users, agentSessionTenant+"/"+agentSessionCN)

	closedCtx, closedIdentity := agentSessionCallContext(t, am, cert)
	if len(closedIdentity.Roles) != 0 {
		t.Fatalf("a closed session must hold no roles, got %v", closedIdentity.Roles)
	}
	for _, tool := range []string{"quicbaas_orchestrator_inference", qrdbGetTool, qrdbPutTool, qrdbListTool} {
		if err := am.AuthorizeMCP(closedCtx, registry, tool, nil); err == nil {
			t.Errorf("%s was admitted after the session closed", tool)
		}
	}
}

// TestAgentSession_IsNotADaemon pins the separation at the authorization gate
// rather than only in the grant: a session certificate is refused the daemon's
// control-plane verbs, and the two enrollments converge different roles, so
// neither is a route to the other's authority.
func TestAgentSession_IsNotADaemon(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{
		agentSessionTenant + "/" + agentSessionCN: enrolledAgentSessionUser(t, cert),
	})

	registry := middleware.NewMCPToolRegistry()
	daemonOnly := map[string]permission.Permission{
		"daemon_heartbeat":      permission.Permission(iam.FleetScopeControlHeartbeat),
		"daemon_hook":           permission.Permission(iam.FleetScopeControlHook),
		"daemon_materialize":    permission.Permission(iam.FleetScopeAccountMaterialize),
		"daemon_account_read":   permission.Permission(iam.FleetScopeAccountRead),
		"daemon_account_list":   permission.Permission(iam.FleetScopeAccountList),
		"daemon_runtime_spawn":  permission.Permission("agent:runtime:spawn"),
		"daemon_runtime_prompt": permission.Permission("agent:runtime:prompt"),
	}
	for tool, perm := range daemonOnly {
		registry.Register(tool, perm)
	}

	ctx, _ := agentSessionCallContext(t, am, cert)
	for tool := range daemonOnly {
		if err := am.AuthorizeMCP(ctx, registry, tool, nil); err == nil {
			t.Errorf("a session was admitted on the daemon-only verb behind %s", tool)
		}
	}
}
