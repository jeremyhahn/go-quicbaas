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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbmiddleware "github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	qrdbrbac "github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	qrdbpermission "github.com/jeremyhahn/go-qrdb/pkg/permission"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const (
	heartbeatPattern = "/api/v1/fleet/{nodeID}/heartbeat"
	agentsPattern    = "/api/v1/fleet"
)

// registerResponse is the subset of the register endpoint's answer this
// scenario reads.
type registerResponse struct {
	AgentID      string   `json:"agent_id"`
	ClientID     string   `json:"client_id"`
	MTLSClientID string   `json:"mtls_client_id"`
	Scopes       []string `json:"scopes"`
}

// servedBackends is what a daemon started with --backends acp advertises: the
// backends it SERVES, not the transport kind that constructed them. The
// enroller derives it from Runtime.Backends, the same set the ready log and
// the heartbeat report (go-quicbaas#681).
var servedBackends = []string{"claude", "codex", "opencode"}

// register drives POST /api/v1/fleet/enroll with the daemon's client
// certificate, exactly as the daemon's enroller does.
func (f *enrollmentFixture) register(t *testing.T, cert *x509.Certificate) *registerResponse {
	t.Helper()
	return f.registerAdvertising(t, cert, servedBackends)
}

// registerAdvertising is register with the advertised backend list under the
// caller's control, so a scenario can enroll a daemon that serves something
// else — or one that still speaks the old transport vocabulary. It requires
// the registration to succeed.
func (f *enrollmentFixture) registerAdvertising(t *testing.T, cert *x509.Certificate, backends []string) *registerResponse {
	t.Helper()

	w := f.registerRaw(t, cert, backends)
	require.Equal(t, http.StatusCreated, w.Code, "register body: %s", w.Body.String())

	var resp registerResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

// registerRaw drives one registration and hands back the recorder undecided,
// for the scenarios whose subject is the REFUSAL.
func (f *enrollmentFixture) registerRaw(t *testing.T, cert *x509.Certificate, backends []string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"tenant_id": enrollTenant,
		"hostname":  "agent-04",
		"bind_addr": "0.0.0.0:9092",
		"metadata":  map[string]string{"backends": strings.Join(backends, ",")},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/enroll", strings.NewReader(string(body)))
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	w := httptest.NewRecorder()
	f.server.HandleFleetEnroll(w, req)
	return w
}

// authorizedHeartbeat mounts the heartbeat route behind go-qrdb's REAL
// authentication and authorization middleware — the mTLS authenticator, the
// RBAC authorizer, and the store-resolved role enrichment reading the records
// registration wrote — so a heartbeat is admitted here only if it would be
// admitted on the node.
func (f *enrollmentFixture) authorizedHeartbeat(t *testing.T) http.Handler {
	t.Helper()

	authMiddleware := qrdbmiddleware.New(
		qrdbrbac.New(&enrolledRoles{records: f.identity, tenant: enrollTenant}),
		qrdbmiddleware.WithAuthenticator(qrdbauth.NewMTLSAuthenticator()),
		qrdbmiddleware.WithUserService(&enrolledUsers{records: f.identity}),
	)

	routes := qrdbmiddleware.NewRESTRouteRegistry()
	routes.Register(http.MethodPost, heartbeatPattern,
		qrdbpermission.Permission(iam.FleetScopeControlHeartbeat))

	router := chi.NewRouter()
	router.Post(heartbeatPattern, f.server.HandleFleetNodeHeartbeat)

	return authMiddleware.RESTMiddleware(routes, router)
}

// heartbeat sends one heartbeat the way the daemon sends it: the client
// certificate on the connection AND the bearer its enrollment minted.
func (f *enrollmentFixture) heartbeat(t *testing.T, cert *x509.Certificate, agentID, bearer string) *httptest.ResponseRecorder {
	t.Helper()

	body := `{"status":"online","bind_addr":"0.0.0.0:9092","backends":["claude","codex","opencode"]}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/fleet/"+agentID+"/heartbeat?tenant_id="+enrollTenant,
		strings.NewReader(body))
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	f.authorizedHeartbeat(t).ServeHTTP(w, req)
	return w
}

// listAgents drives GET /api/v1/fleet for one tenant and returns
// the decoded count and the agent ids.
func (f *enrollmentFixture) listAgents(t *testing.T, tenantID string) (int, []string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, agentsPattern+"?tenant_id="+tenantID, nil)
	w := httptest.NewRecorder()
	f.server.HandleListFleetNodes(w, req)
	require.Equal(t, http.StatusOK, w.Code, "list body: %s", w.Body.String())

	var decoded struct {
		Count  int `json:"count"`
		Agents []struct {
			ID       string   `json:"id"`
			Backends []string `json:"backends"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))

	ids := make([]string, 0, len(decoded.Agents))
	for _, agent := range decoded.Agents {
		ids = append(ids, agent.ID)
	}
	return decoded.Count, ids
}

// listedBackends returns the "backends" the agents listing rendered for one
// agent, which is what an operator reading the registry sees.
func (f *enrollmentFixture) listedBackends(t *testing.T, tenantID, agentID string) []string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, agentsPattern+"?tenant_id="+tenantID, nil)
	w := httptest.NewRecorder()
	f.server.HandleListFleetNodes(w, req)
	require.Equal(t, http.StatusOK, w.Code, "list body: %s", w.Body.String())

	var decoded struct {
		Agents []struct {
			ID       string   `json:"id"`
			Backends []string `json:"backends"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded))

	for _, agent := range decoded.Agents {
		if agent.ID == agentID {
			return agent.Backends
		}
	}
	t.Fatalf("agent %s is not in the listing", agentID)
	return nil
}

// dispatchQuery issues the registry read the dispatcher issues for a worker
// session routed on one backend.
//
// THIS IS THE DISPATCH PATH FOR THE SDLC RUNNER, not an approximation of it.
// A module runner spawns its own session and never carries an agent id: the
// spawn resolves a backend by NAME through the remote backend resolver, whose
// candidate list is exactly this call (pkg/server/lifecycle_remote_resolver.go
// resolve). When it comes back empty the dispatcher falls through to the
// central's local backend table and the worker runs in-process with no
// container, which the sdlc runner refuses.
func (f *enrollmentFixture) dispatchQuery(t *testing.T, tenantID, backend string) []string {
	t.Helper()

	ids := make([]string, 0, 2)
	for _, status := range []orchestration.FleetNodeStatus{
		orchestration.FleetNodeStatusRunning,
		orchestration.FleetNodeStatusIdle,
		orchestration.FleetNodeStatusRegistered,
	} {
		agents, err := f.registry.ListFleetNodes(context.Background(), tenantID, &orchestration.FleetNodeFilter{
			Backend: backend,
			Status:  status,
		})
		require.NoError(t, err)
		for _, agent := range agents {
			ids = append(ids, agent.ID)
		}
	}
	return ids
}

// TestE2E_AgentEnrollment_RegisterHeartbeatDispatch is the scenario the fleet
// runs and the one that was never tested: enroll over mTLS, heartbeat holding
// BOTH the client certificate and the enrollment's bearer, and be visible to
// the tenant and to dispatch. Every stage below failed on the burn-in node
// (go-quicbaas#663) — the heartbeat with 403 because the certificate principal
// held no grant, and the listing with an empty result because the registration
// landed in another tenant's namespace.
func TestE2E_AgentEnrollment_RegisterHeartbeatDispatch(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)

	// 1. Enroll.
	enrolled := f.register(t, cert)
	require.Equal(t, iam.FleetNodeAppName(enrollTenant, enrollAgent), enrolled.AgentID)
	require.ElementsMatch(t, iam.FleetRuntimeGrant(), enrolled.Scopes)

	// 2. Heartbeat presenting the certificate AND the bearer, through go-qrdb's
	//    real authentication and authorization.
	w := f.heartbeat(t, cert, enrolled.AgentID, "enrollment-bearer")
	require.Equal(t, http.StatusOK, w.Code, "heartbeat body: %s", w.Body.String())

	// 3. The agent is listed under its own tenant, and only there.
	count, ids := f.listAgents(t, enrollTenant)
	require.Equal(t, 1, count, "the tenant cannot see the agent it enrolled")
	require.Equal(t, []string{enrolled.AgentID}, ids)

	systemCount, _ := f.listAgents(t, "system")
	require.Zero(t, systemCount, "a tenant's agent must not appear in the system tenant")

	// 4. Dispatch's own read: the task's tenant as a parameter, from a loop
	//    context that names no tenant of its own.
	agents, err := f.registry.ListFleetNodes(context.Background(), enrollTenant, &orchestration.FleetNodeFilter{})
	require.NoError(t, err)
	require.Len(t, agents, 1, "dispatch cannot route work to an agent it cannot list")
	require.Equal(t, "claude", agents[0].Backend, "the primary is a backend, not the transport kind")
	require.Equal(t, servedBackends, agents[0].Capabilities)

	// 5. An operator reading the registry sees what dispatch sees.
	require.Equal(t, servedBackends, f.listedBackends(t, enrollTenant, enrolled.AgentID))

	// 6. THE STAGE THE BURN-IN NEVER REACHED: a worker session routed on each
	//    served backend selects this agent. go-truststrap#53 was routed on
	//    "claude" and selected nobody, so its job spawned in-process and the
	//    sdlc runner refused the session for having no container.
	for _, backend := range servedBackends {
		require.Equal(t, []string{enrolled.AgentID}, f.dispatchQuery(t, enrollTenant, backend),
			"a task routed on %q must select the daemon that serves it", backend)
	}

	// 7. And the fall-through stays deliberate: a backend this fleet does not
	//    serve selects nobody, which is what sends the spawn to the central's
	//    local backend table.
	require.Empty(t, f.dispatchQuery(t, enrollTenant, "ollama"),
		"no daemon here serves ollama")
	require.Empty(t, f.dispatchQuery(t, enrollTenant, "acp"),
		"the transport kind is not a backend a task can route to")
}

// TestE2E_AgentEnrollment_TransportVocabularyReachesNobody is the control that
// makes the scenario above evidence rather than assertion: a daemon that
// registers its TRANSPORT name enrolls and lists exactly as happily, and is
// invisible to every task routed on a backend it serves. That is the live
// failure, reproduced end to end.
//
// It is the control for the register payload (go-quicbaas#681) and, in its
// second half, the evidence for the correction (go-quicbaas#699): the daemon's
// FIRST heartbeat carries the served list, and the record converges on it
// without an operator deleting anything.
func TestE2E_AgentEnrollment_TransportVocabularyReachesNobody(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)

	enrolled := f.registerAdvertising(t, cert, []string{"acp"})

	count, ids := f.listAgents(t, enrollTenant)
	require.Equal(t, 1, count, "the daemon enrolls and lists no matter what it advertises")
	require.Equal(t, []string{enrolled.AgentID}, ids)

	for _, backend := range servedBackends {
		require.Empty(t, f.dispatchQuery(t, enrollTenant, backend),
			"a daemon advertising its transport is unreachable by a task routed on %q", backend)
	}

	// The heartbeat body already carried the served list and the handler
	// discarded it, so the record stayed wrong forever.
	w := f.heartbeat(t, cert, enrolled.AgentID, "enrollment-bearer")
	require.Equal(t, http.StatusOK, w.Code, "heartbeat body: %s", w.Body.String())

	require.Equal(t, servedBackends, f.listedBackends(t, enrollTenant, enrolled.AgentID),
		"one heartbeat must correct a record registered under the transport vocabulary")
	for _, backend := range servedBackends {
		require.Equal(t, []string{enrolled.AgentID}, f.dispatchQuery(t, enrollTenant, backend),
			"after reconciliation a task routed on %q must select the daemon", backend)
	}
	require.Empty(t, f.dispatchQuery(t, enrollTenant, "acp"),
		"the transport name it no longer serves must select nobody")
}

// TestE2E_AgentEnrollment_RestartThenReRegisterRefreshesTheRecord is the live
// burn-in sequence end to end (go-quicbaas#646).
//
// The daemon enrolled under the pre-#681 transport vocabulary on one address.
// The registry is durable, so that record survived the restart; the daemon
// discarded its cached credentials and re-enrolled from a new bind reporting
// the backends it serves, the guarded create answered "agent already exists",
// and the record kept BOTH stale columns. The remote resolver's
// ListFleetNodes(Backend:"claude") found nothing, the spawn fell to the central's
// in-process backend, and the sdlc runner refused it for having no container.
//
// No heartbeat is sent here on purpose: the refresh must land at registration,
// because dispatch does not wait for a heartbeat.
func TestE2E_AgentEnrollment_RestartThenReRegisterRefreshesTheRecord(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)

	first := f.registerAdvertising(t, cert, []string{"acp"})
	for _, backend := range servedBackends {
		require.Empty(t, f.dispatchQuery(t, enrollTenant, backend))
	}

	// Restart: fresh credentials, current truth about what it serves.
	second := f.register(t, cert)
	require.Equal(t, first.AgentID, second.AgentID, "the identity is derived, not minted")

	count, _ := f.listAgents(t, enrollTenant)
	require.Equal(t, 1, count, "a refresh must not duplicate the fleet row")

	require.Equal(t, servedBackends, f.listedBackends(t, enrollTenant, second.AgentID),
		"the record must name what the daemon reported on THIS registration")
	for _, backend := range servedBackends {
		require.Equal(t, []string{second.AgentID}, f.dispatchQuery(t, enrollTenant, backend),
			"a task routed on %q must select the re-registered daemon without waiting for a heartbeat", backend)
	}
	require.Empty(t, f.dispatchQuery(t, enrollTenant, "acp"))

	// And it is still a fully working member of the fleet.
	w := f.heartbeat(t, cert, second.AgentID, "enrollment-bearer")
	require.Equal(t, http.StatusOK, w.Code, "heartbeat body: %s", w.Body.String())
}

// TestE2E_AgentEnrollment_ReRegisteringUnderAnotherPrincipalIsRefused walks
// the identity-takeover path to its end at the endpoint.
//
// The agent id is a SANITISED derivation of the certificate CommonName
// (iam.SanitizeAppName folds space, slash, colon and backslash to "-"), so a
// second certificate CAN address the first one's record. Two things refuse it,
// and the scenario pins both layers:
//
//   - upstream, the identity enrollment #674 performs will not accept a
//     CommonName carrying any of those four characters as a qrdb subject, so
//     today the colliding certificate never reaches the registrar;
//   - underneath, the registry refuses to refresh a record whose principal is
//     not the one presenting (TestRefreshAgent_RefusesADifferentPrincipal),
//     which is what keeps a future relaxation of that charset from silently
//     becoming a takeover.
//
// What must hold either way, and is what this asserts end to end: the
// registration does not succeed and the owner's record is untouched.
func TestE2E_AgentEnrollment_ReRegisteringUnderAnotherPrincipalIsRefused(t *testing.T) {
	f := newEnrollmentFixture(t)

	owner := agentCertificate(t, enrollTenant, enrollAgent)
	enrolled := f.register(t, owner)

	// A CommonName that sanitises to the same agent id as the owner's.
	intruderCert := agentCertificate(t, enrollTenant, strings.ReplaceAll(enrollAgent, "-", " "))
	require.Equal(t, enrolled.AgentID, iam.FleetNodeAppName(enrollTenant, intruderCert.Subject.CommonName),
		"the scenario only means anything if the two subjects collide on one agent id")

	w := f.registerRaw(t, intruderCert, []string{"ollama"})
	require.NotEqual(t, http.StatusCreated, w.Code,
		"a certificate that is not the record's principal must not enroll into it: %s", w.Body.String())

	require.Equal(t, servedBackends, f.listedBackends(t, enrollTenant, enrolled.AgentID),
		"a refused registration must leave the owner's record exactly as it was")
	for _, backend := range servedBackends {
		require.Equal(t, []string{enrolled.AgentID}, f.dispatchQuery(t, enrollTenant, backend))
	}
	require.Empty(t, f.dispatchQuery(t, enrollTenant, "ollama"))
}

// TestE2E_AgentEnrollment_HeartbeatWithoutEnrollmentIsRefused is the control:
// the same heartbeat, from the same certificate, with no enrollment behind it.
// It reproduces the live 403 exactly, which is what makes the passing case
// above evidence rather than assertion.
func TestE2E_AgentEnrollment_HeartbeatWithoutEnrollmentIsRefused(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)

	w := f.heartbeat(t, cert, iam.FleetNodeAppName(enrollTenant, enrollAgent), "enrollment-bearer")
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "cannot agent_heartbeat on orchestrator")
}

// TestE2E_AgentEnrollment_ReRegistrationConverges covers the daemon's restart
// loop: registration is refused nothing and duplicates nothing, and the agent
// stays exactly as visible afterwards.
func TestE2E_AgentEnrollment_ReRegistrationConverges(t *testing.T) {
	f := newEnrollmentFixture(t)
	cert := agentCertificate(t, enrollTenant, enrollAgent)

	first := f.register(t, cert)
	second := f.register(t, cert)
	require.Equal(t, first.AgentID, second.AgentID)
	require.Equal(t, first.MTLSClientID, second.MTLSClientID)

	count, _ := f.listAgents(t, enrollTenant)
	require.Equal(t, 1, count, "re-registration duplicated the fleet row")

	w := f.heartbeat(t, cert, second.AgentID, "enrollment-bearer")
	require.Equal(t, http.StatusOK, w.Code, "heartbeat body: %s", w.Body.String())
}
