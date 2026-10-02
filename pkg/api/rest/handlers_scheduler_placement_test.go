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

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// mockQuicBaaSWithPlacement serves a real placement engine over an
// in-memory store, so the handlers are exercised end to end.
type mockQuicBaaSWithPlacement struct {
	*testutil.MockQuicBaaS
	placement service.PlacementService
}

func (m *mockQuicBaaSWithPlacement) Placement() service.PlacementService { return m.placement }

func setupPlacementServer(t *testing.T, svc service.PlacementService) *Server {
	t.Helper()
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	server.service = &mockQuicBaaSWithPlacement{MockQuicBaaS: testutil.NewMockQuicBaaS(), placement: svc}
	return server
}

// callPlacement invokes a handler with an optional {id} URL parameter and
// JSON body, returning the recorder.
func callPlacement(t *testing.T, h http.HandlerFunc, method, target, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	// The tenant go-qrdb's AuthMiddleware binds for an authenticated caller.
	req := httptest.NewRequest(method, target, reader)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))
	if id != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestPlacementREST_EndToEnd(t *testing.T) {
	s := setupPlacementServer(t, service.NewPlacementService(placement.New(placementtest.New())))

	w := callPlacement(t, s.HandleRegisterPlacementNode, http.MethodPost, "/api/v1/scheduler/nodes", "",
		service.RegisterPlacementNodeRequest{Facts: placement.NodeFacts{NodeID: "n1", Capacity: map[string]int64{"cpu": 4}}, TTLSeconds: 20})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var node placement.NodeInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &node))
	assert.Equal(t, int64(20), node.TTLSeconds)

	w = callPlacement(t, s.HandleRenewPlacementNode, http.MethodPost, "/api/v1/scheduler/nodes/n1/renew", "n1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = callPlacement(t, s.HandlePlaceWorkload, http.MethodPost, "/api/v1/scheduler/placements", "",
		placement.PlacementRequest{WorkloadID: "w1", Group: "web", Resources: map[string]int64{"cpu": 3}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res placement.PlaceResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, "n1", res.Placement.NodeID)

	// What the server recorded: the node now carries the allocation.
	w = callPlacement(t, s.HandleGetPlacementNode, http.MethodGet, "/api/v1/scheduler/nodes/n1", "n1", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &node))
	assert.Equal(t, int64(3), node.Allocated["cpu"])

	w = callPlacement(t, s.HandleListPlacementNodes, http.MethodGet, "/api/v1/scheduler/nodes", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var nodes ListPlacementNodesRESTResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &nodes))
	assert.Len(t, nodes.Nodes, 1)

	w = callPlacement(t, s.HandleGetPlacement, http.MethodGet, "/api/v1/scheduler/placements/w1", "w1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = callPlacement(t, s.HandleListPlacements, http.MethodGet, "/api/v1/scheduler/placements?group=web&node_id=n1", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list ListPlacementsRESTResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list.Placements, 1)

	w = callPlacement(t, s.HandleListPlacements, http.MethodGet, "/api/v1/scheduler/placements?group=db", "", nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Empty(t, list.Placements, "the group filter reached the service")

	w = callPlacement(t, s.HandleCordonPlacementNode, http.MethodPost, "/api/v1/scheduler/nodes/n1/cordon", "n1", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &node))
	assert.True(t, node.Status.Unschedulable)

	w = callPlacement(t, s.HandleUncordonPlacementNode, http.MethodPost, "/api/v1/scheduler/nodes/n1/uncordon", "n1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = callPlacement(t, s.HandleDrainPlacementNode, http.MethodPost, "/api/v1/scheduler/nodes/n1/drain", "n1", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var drained DrainPlacementNodeRESTResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &drained))
	require.Len(t, drained.Placements, 1)

	w = callPlacement(t, s.HandleReleasePlacement, http.MethodDelete, "/api/v1/scheduler/placements/w1", "w1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = callPlacement(t, s.HandleDeregisterPlacementNode, http.MethodDelete, "/api/v1/scheduler/nodes/n1", "n1", nil)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestPlacementREST_Errors(t *testing.T) {
	s := setupPlacementServer(t, service.NewPlacementService(placement.New(placementtest.New())))

	w := callPlacement(t, s.HandleGetPlacementNode, http.MethodGet, "/api/v1/scheduler/nodes/x", "x", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// No feasible node is the answer to the request, so its reasons are
	// echoed rather than sanitised.
	w = callPlacement(t, s.HandlePlaceWorkload, http.MethodPost, "/api/v1/scheduler/placements", "", placement.PlacementRequest{WorkloadID: "w"})
	assert.Equal(t, http.StatusPreconditionFailed, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "no feasible node")

	w = callPlacement(t, s.HandlePlaceWorkload, http.MethodPost, "/api/v1/scheduler/placements", "", placement.PlacementRequest{})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/placements", bytes.NewReader([]byte("{nope")))
	rec := httptest.NewRecorder()
	s.HandlePlaceWorkload(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/nodes", bytes.NewReader([]byte("[")))
	rec = httptest.NewRecorder()
	s.HandleRegisterPlacementNode(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	for name, h := range map[string]http.HandlerFunc{
		"renew":      s.HandleRenewPlacementNode,
		"deregister": s.HandleDeregisterPlacementNode,
		"drain":      s.HandleDrainPlacementNode,
		"release":    s.HandleReleasePlacement,
		"get":        s.HandleGetPlacement,
	} {
		w := callPlacement(t, h, http.MethodPost, "/x", "missing", nil)
		assert.Equalf(t, http.StatusNotFound, w.Code, "%s: %s", name, w.Body.String())
	}
}

func TestPlacementREST_UnavailableWithoutAnEngine(t *testing.T) {
	s := setupPlacementServer(t, nil)
	handlers := map[string]http.HandlerFunc{
		"register":   s.HandleRegisterPlacementNode,
		"list nodes": s.HandleListPlacementNodes,
		"get node":   s.HandleGetPlacementNode,
		"renew":      s.HandleRenewPlacementNode,
		"deregister": s.HandleDeregisterPlacementNode,
		"drain":      s.HandleDrainPlacementNode,
		"place":      s.HandlePlaceWorkload,
		"list":       s.HandleListPlacements,
		"get":        s.HandleGetPlacement,
		"release":    s.HandleReleasePlacement,
	}
	for name, h := range handlers {
		w := callPlacement(t, h, http.MethodPost, "/x", "id", map[string]string{})
		assert.Equalf(t, http.StatusServiceUnavailable, w.Code, "%s", name)
	}
}

// contendedPlacement is a PlacementService whose Place always loses its
// guarded write.
type contendedPlacement struct {
	service.PlacementService
}

func (contendedPlacement) Place(context.Context, *placement.PlacementRequest) (*placement.PlaceResult, error) {
	return nil, &errors.QBError{Code: errors.ErrAborted, Op: "PlacementService.Place", Err: &placement.ContendedError{Op: "place", Attempts: 16}}
}

func TestPlacementREST_ContentionIsAConflict(t *testing.T) {
	s := setupPlacementServer(t, contendedPlacement{})
	w := callPlacement(t, s.HandlePlaceWorkload, http.MethodPost, "/api/v1/scheduler/placements", "", placement.PlacementRequest{WorkloadID: "w"})
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "contended")
}
