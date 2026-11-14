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
	stderrors "errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Fleet placement specs (SPEC row declarative-placement, go-quicbaas#1681):
// where a class of work may run across the fleet's hosts, and the read-only
// report of where it runs instead. Every route takes an optional tenant_id
// query parameter; empty means the caller's tenant, and naming another is
// refused by the service. Authorization is the route permission table's:
// placement:read for get, list and drift, placement:manage for declare and
// withdraw.

// ErrFleetPlacementUnavailable is returned when the service facade serves no
// fleet placement.
var ErrFleetPlacementUnavailable = stderrors.New("fleet placement service not available")

// ListFleetPlacementSpecsRESTResponse is the body of GET
// /api/v1/fleet/placements.
type ListFleetPlacementSpecsRESTResponse struct {
	Specs []*orchestration.PlacementSpec `json:"specs"`
	Count int                            `json:"count"`
}

// WithdrawFleetPlacementSpecRESTResponse is the body of DELETE
// /api/v1/fleet/placements/{name}.
type WithdrawFleetPlacementSpecRESTResponse struct {
	Name      string `json:"name"`
	Withdrawn bool   `json:"withdrawn"`
}

func (s *Server) fleetPlacementSvc(w http.ResponseWriter, r *http.Request) (service.FleetPlacementService, bool) {
	provider, ok := s.service.(service.FleetPlacementProvider)
	if !ok || provider.FleetPlacement() == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrFleetPlacementUnavailable)
		return nil, false
	}
	return provider.FleetPlacement(), true
}

// HandleListFleetPlacementSpecs handles GET /api/v1/fleet/placements.
func (s *Server) HandleListFleetPlacementSpecs(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.fleetPlacementSvc(w, r)
	if !ok {
		return
	}
	specs, err := svc.ListPlacementSpecs(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, ListFleetPlacementSpecsRESTResponse{Specs: specs, Count: len(specs)})
}

// HandleGetFleetPlacementSpec handles GET /api/v1/fleet/placements/{name}.
func (s *Server) HandleGetFleetPlacementSpec(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.fleetPlacementSvc(w, r)
	if !ok {
		return
	}
	spec, err := svc.GetPlacementSpec(r.Context(), r.URL.Query().Get("tenant_id"), chi.URLParam(r, "name"))
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, spec)
}

// HandleDeclareFleetPlacementSpec handles PUT /api/v1/fleet/placements/{name}.
// The body is the spec; its name, when given, must be the path's. The
// generation it carries is the one the caller read (zero to create): a stale
// one is a conflict.
func (s *Server) HandleDeclareFleetPlacementSpec(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.fleetPlacementSvc(w, r)
	if !ok {
		return
	}
	var spec orchestration.PlacementSpec
	if !decodePlacementBody(w, r, &spec) {
		return
	}
	name := chi.URLParam(r, "name")
	if spec.Name == "" {
		spec.Name = name
	} else if spec.Name != name {
		RespondBadRequest(w, r, "the spec's name must be the path's")
		return
	}
	stored, err := svc.DeclarePlacementSpec(r.Context(), r.URL.Query().Get("tenant_id"), &spec)
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, stored)
}

// HandleWithdrawFleetPlacementSpec handles DELETE
// /api/v1/fleet/placements/{name}.
func (s *Server) HandleWithdrawFleetPlacementSpec(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.fleetPlacementSvc(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if err := svc.WithdrawPlacementSpec(r.Context(), r.URL.Query().Get("tenant_id"), name); err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, WithdrawFleetPlacementSpecRESTResponse{Name: name, Withdrawn: true})
}

// HandleGetFleetPlacementDrift handles GET
// /api/v1/fleet/placements/{name}/drift: the read-only report of the spec's
// role work against the hosts it admits. A report that cannot be complete is
// refused with 412 naming what has to be fixed first.
func (s *Server) HandleGetFleetPlacementDrift(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.fleetPlacementSvc(w, r)
	if !ok {
		return
	}
	report, err := svc.GetPlacementDrift(r.Context(), r.URL.Query().Get("tenant_id"), chi.URLParam(r, "name"))
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}
