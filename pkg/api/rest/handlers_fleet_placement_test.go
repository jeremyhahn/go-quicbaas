//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

type mockQuicBaaSWithFleetPlacement struct {
	*testutil.MockQuicBaaS
	fleetPlacement service.FleetPlacementService
}

func (m *mockQuicBaaSWithFleetPlacement) FleetPlacement() service.FleetPlacementService {
	return m.fleetPlacement
}

// setupFleetPlacementServer serves a fleet placement service over in-memory
// stores: acme has hosts h1 (gpu) and h2, and one live session on h2.
func setupFleetPlacementServer(t *testing.T) (*Server, orchestration.AgentStore) {
	t.Helper()
	ctx := context.Background()
	registry := orchestration.NewInMemoryFleetRegistry()
	for _, node := range []*orchestration.FleetNode{
		{ID: "n1", TenantID: "acme", Name: "n1", Host: "h1", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning, Capabilities: []string{"gpu"}},
		{ID: "n2", TenantID: "acme", Name: "n2", Host: "h2", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning},
	} {
		require.NoError(t, registry.RegisterFleetNode(ctx, node))
	}
	sessions := orchestration.NewInMemoryAgentStore()
	require.NoError(t, sessions.PutSession(ctx, &orchestration.SessionRecord{
		SessionID: "s1", TenantID: "acme", Backend: "bot", HostAffinity: "h2", Status: orchestration.SessionStateActive,
	}))
	specs, err := orchestration.NewPlacementStore(orchestration.NewMemoryTaskKV())
	require.NoError(t, err)
	inventory, err := orchestration.NewFleetInventory(registry)
	require.NoError(t, err)
	work, err := orchestration.NewSessionWorkSource(sessions, func(*orchestration.SessionRecord) string { return "agent" })
	require.NoError(t, err)
	drift, err := orchestration.NewDriftReporter(specs, inventory, work)
	require.NoError(t, err)

	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	server.service = &mockQuicBaaSWithFleetPlacement{
		MockQuicBaaS:   testutil.NewMockQuicBaaS(),
		fleetPlacement: service.NewFleetPlacementService(specs, drift),
	}
	return server, sessions
}

// callFleetPlacement calls h as acme's caller, with the {name} route
// parameter set when name is not empty.
func callFleetPlacement(t *testing.T, h http.HandlerFunc, method, target, name string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))
	if name != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", name)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestFleetPlacementREST_DeclareGetListDriftWithdraw(t *testing.T) {
	s, _ := setupFleetPlacementServer(t)

	w := callFleetPlacement(t, s.HandleDeclareFleetPlacementSpec, http.MethodPut, "/api/v1/fleet/placements/agents", "agents",
		map[string]any{"role": "agent", "hosts": []string{"h1"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored orchestration.PlacementSpec
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &stored))
	assert.Equal(t, "agents", stored.Name)
	assert.Equal(t, "acme", stored.TenantID)
	assert.Equal(t, uint64(1), stored.Generation)

	w = callFleetPlacement(t, s.HandleGetFleetPlacementSpec, http.MethodGet, "/api/v1/fleet/placements/agents", "agents", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = callFleetPlacement(t, s.HandleListFleetPlacementSpecs, http.MethodGet, "/api/v1/fleet/placements", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list ListFleetPlacementSpecsRESTResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Equal(t, 1, list.Count)

	w = callFleetPlacement(t, s.HandleGetFleetPlacementDrift, http.MethodGet, "/api/v1/fleet/placements/agents/drift", "agents", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var report orchestration.DriftReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	assert.Equal(t, 1, report.ObservedWork)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, orchestration.DriftHostNotAdmitted, report.Findings[0].Kind)
	assert.Equal(t, "h2", report.Findings[0].Host)

	w = callFleetPlacement(t, s.HandleWithdrawFleetPlacementSpec, http.MethodDelete, "/api/v1/fleet/placements/agents", "agents", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = callFleetPlacement(t, s.HandleListFleetPlacementSpecs, http.MethodGet, "/api/v1/fleet/placements", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"specs":[],"count":0}`, w.Body.String())
}

func TestFleetPlacementREST_Errors(t *testing.T) {
	s, sessions := setupFleetPlacementServer(t)

	cases := []struct {
		name   string
		h      http.HandlerFunc
		method string
		target string
		param  string
		body   any
		want   int
	}{
		{"get absent", s.HandleGetFleetPlacementSpec, http.MethodGet, "/x", "absent", nil, http.StatusNotFound},
		{"withdraw absent", s.HandleWithdrawFleetPlacementSpec, http.MethodDelete, "/x", "absent", nil, http.StatusNotFound},
		{"drift absent", s.HandleGetFleetPlacementDrift, http.MethodGet, "/x", "absent", nil, http.StatusNotFound},
		{"invalid spec", s.HandleDeclareFleetPlacementSpec, http.MethodPut, "/x", "bad", map[string]any{"role": "agent", "hosts": []string{"h1"}, "host_count": 1}, http.StatusBadRequest},
		{"name differs from path", s.HandleDeclareFleetPlacementSpec, http.MethodPut, "/x", "one", map[string]any{"name": "two", "role": "agent", "hosts": []string{"h1"}}, http.StatusBadRequest},
		{"another tenant", s.HandleListFleetPlacementSpecs, http.MethodGet, "/x?tenant_id=other", "", nil, http.StatusForbidden},
	}
	for _, tc := range cases {
		w := callFleetPlacement(t, tc.h, tc.method, tc.target, tc.param, tc.body)
		assert.Equalf(t, tc.want, w.Code, "%s: %s", tc.name, w.Body.String())
	}

	// A non-JSON body is a bad request.
	req := httptest.NewRequest(http.MethodPut, "/x", bytes.NewReader([]byte("{")))
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))
	w := httptest.NewRecorder()
	s.HandleDeclareFleetPlacementSpec(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// A declare over an existing spec without its generation is a conflict.
	spec := map[string]any{"role": "agent", "hosts": []string{"h1"}}
	w = callFleetPlacement(t, s.HandleDeclareFleetPlacementSpec, http.MethodPut, "/x", "agents", spec)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = callFleetPlacement(t, s.HandleDeclareFleetPlacementSpec, http.MethodPut, "/x", "agents", spec)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	// A live session whose host is unrecorded cannot be named: the report is
	// refused with 412 rather than answered without it.
	require.NoError(t, sessions.PutSession(context.Background(), &orchestration.SessionRecord{
		SessionID: "s2", TenantID: "acme", Backend: "bot", Status: orchestration.SessionStateActive,
	}))
	w = callFleetPlacement(t, s.HandleGetFleetPlacementDrift, http.MethodGet, "/x", "agents", nil)
	assert.Equal(t, http.StatusPreconditionFailed, w.Code, w.Body.String())
}

func TestFleetPlacementREST_UnavailableWithoutTheService(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	server.service = testutil.NewMockQuicBaaS()
	unbound, _, cleanup2 := setupMockServer(t)
	t.Cleanup(cleanup2)
	unbound.service = &mockQuicBaaSWithFleetPlacement{
		MockQuicBaaS:   testutil.NewMockQuicBaaS(),
		fleetPlacement: service.NewFleetPlacementService(nil, nil),
	}
	for label, s := range map[string]*Server{"no provider": server, "unbound": unbound} {
		handlers := map[string]http.HandlerFunc{
			"list":     s.HandleListFleetPlacementSpecs,
			"get":      s.HandleGetFleetPlacementSpec,
			"declare":  s.HandleDeclareFleetPlacementSpec,
			"withdraw": s.HandleWithdrawFleetPlacementSpec,
			"drift":    s.HandleGetFleetPlacementDrift,
		}
		for name, h := range handlers {
			w := callFleetPlacement(t, h, http.MethodPut, "/x", "x", map[string]any{"role": "agent", "hosts": []string{"h1"}})
			assert.Equalf(t, http.StatusServiceUnavailable, w.Code, "%s %s: %s", label, name, w.Body.String())
		}
	}
}

// setupFleetPlacementConsentServer returns a server whose fleet placement
// service has a consent ledger bound, and the ledger.
func setupFleetPlacementConsentServer(t *testing.T) (*Server, orchestration.ConsentLedger) {
	t.Helper()
	s, _ := setupFleetPlacementServer(t)
	ledger, err := orchestration.NewConsentLedger(orchestration.NewMemoryTaskKV())
	require.NoError(t, err)
	binder, ok := s.service.(*mockQuicBaaSWithFleetPlacement).fleetPlacement.(service.FleetPlacementConsentBinder)
	require.True(t, ok)
	binder.BindFleetPlacementConsent(ledger)
	return s, ledger
}

func raiseFleetPlacementConsentREST(t *testing.T, ledger orchestration.ConsentLedger, workID string) *orchestration.PlacementConsentRequest {
	t.Helper()
	req, err := ledger.Raise(orchestration.ContextWithTenant(context.Background(), "acme"), "acme", orchestration.ConsentMove{
		SpecName: "agents", SpecGeneration: 1, Class: "agent", WorkID: workID, SourceHost: "h1",
	})
	require.NoError(t, err)
	return req
}

// callFleetPlacementConsent calls a consent handler in tenant acme as
// subject (none when empty), with id as the {id} path parameter.
func callFleetPlacementConsent(t *testing.T, h http.HandlerFunc, method, target, subject, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, isRaw := body.([]byte)
	if body != nil && !isRaw {
		var err error
		raw, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(raw))
	ctx := tenantctx.WithTenant(req.Context(), "acme")
	if subject != "" {
		ctx = auth.SubjectIntoContext(ctx, interfaces.Subject{ID: subject, TenantID: "acme", Authenticated: true})
	}
	if id != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	w := httptest.NewRecorder()
	h(w, req.WithContext(ctx))
	return w
}

func TestFleetPlacementREST_ConsentListAndAnswer(t *testing.T) {
	s, ledger := setupFleetPlacementConsentServer(t)
	first := raiseFleetPlacementConsentREST(t, ledger, "s1")
	raiseFleetPlacementConsentREST(t, ledger, "s2")

	w := callFleetPlacementConsent(t, s.HandleListFleetPlacementConsents, http.MethodGet, "/x?state=pending", "alice", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed ListFleetPlacementConsentsRESTResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	assert.Equal(t, 2, listed.Count)
	require.Len(t, listed.Requests, 2)

	w = callFleetPlacementConsent(t, s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x?tenant_id=acme", "alice", first.ID,
		map[string]any{"state": "granted", "reason": "window", "by": "mallory"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var answered orchestration.PlacementConsentRequest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered))
	assert.Equal(t, orchestration.ConsentGranted, answered.State)
	assert.Equal(t, "alice", answered.By, "the answer is the subject's, never the body's")
	assert.Equal(t, "window", answered.Reason)

	w = callFleetPlacementConsent(t, s.HandleListFleetPlacementConsents, http.MethodGet, "/x?state=granted&tenant_id=acme", "alice", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
	require.Equal(t, 1, listed.Count)
	assert.Equal(t, first.ID, listed.Requests[0].ID)
	assert.Equal(t, "alice", listed.Requests[0].By)

	w = callFleetPlacementConsent(t, s.HandleListFleetPlacementConsents, http.MethodGet, "/x?state=refused", "alice", "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"requests":[],"count":0}`, w.Body.String())
}

func TestFleetPlacementREST_ConsentErrors(t *testing.T) {
	s, ledger := setupFleetPlacementConsentServer(t)
	req := raiseFleetPlacementConsentREST(t, ledger, "s1")
	grant := map[string]any{"state": "granted"}

	cases := []struct {
		name    string
		h       http.HandlerFunc
		method  string
		target  string
		subject string
		id      string
		body    any
		want    int
	}{
		{"list bogus state", s.HandleListFleetPlacementConsents, http.MethodGet, "/x?state=bogus", "alice", "", nil, http.StatusBadRequest},
		{"answer maybe", s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "alice", req.ID, map[string]any{"state": "maybe"}, http.StatusBadRequest},
		{"answer with no subject", s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "", req.ID, grant, http.StatusForbidden},
		{"answer in another tenant", s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x?tenant_id=other", "alice", req.ID, grant, http.StatusForbidden},
		{"list in another tenant", s.HandleListFleetPlacementConsents, http.MethodGet, "/x?tenant_id=other", "alice", "", nil, http.StatusForbidden},
		{"answer unknown", s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "alice", "unknown", grant, http.StatusNotFound},
		{"answer", s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "alice", req.ID, grant, http.StatusOK},
		{"second answer", s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "alice", req.ID, map[string]any{"state": "refused"}, http.StatusConflict},
	}
	for _, tc := range cases {
		w := callFleetPlacementConsent(t, tc.h, tc.method, tc.target, tc.subject, tc.id, tc.body)
		assert.Equalf(t, tc.want, w.Code, "%s: %s", tc.name, w.Body.String())
	}

	// A non-JSON body is a bad request.
	w := callFleetPlacementConsent(t, s.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "alice", req.ID, []byte("{"))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	noProvider, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	noProvider.service = testutil.NewMockQuicBaaS()
	unbound, _ := setupFleetPlacementServer(t)
	for label, srv := range map[string]*Server{"no provider": noProvider, "no ledger": unbound} {
		w := callFleetPlacementConsent(t, srv.HandleListFleetPlacementConsents, http.MethodGet, "/x", "alice", "", nil)
		assert.Equalf(t, http.StatusServiceUnavailable, w.Code, "%s list: %s", label, w.Body.String())
		w = callFleetPlacementConsent(t, srv.HandleAnswerFleetPlacementConsent, http.MethodPost, "/x", "alice", "x", grant)
		assert.Equalf(t, http.StatusServiceUnavailable, w.Code, "%s answer: %s", label, w.Body.String())
	}
}
