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
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The placement routes live under /api/v1/scheduler beside the job routes:
// placement is part of the scheduler surface (Async().Scheduler()). The JSON
// bodies are the pkg/scheduler/placement types verbatim, so the SDK decodes
// them into its own mirror types field for field.

// ErrPlacementServiceUnavailable indicates this node has no placement engine.
var ErrPlacementServiceUnavailable = stderrors.New("scheduler placement service not available")

// RenewPlacementNodeRESTResponse is returned by POST .../nodes/{id}/renew.
type RenewPlacementNodeRESTResponse struct {
	NodeID  string `json:"node_id"`
	Renewed bool   `json:"renewed"`
}

// DeregisterPlacementNodeRESTResponse is returned by DELETE .../nodes/{id}.
type DeregisterPlacementNodeRESTResponse struct {
	NodeID       string `json:"node_id"`
	Deregistered bool   `json:"deregistered"`
}

// ListPlacementNodesRESTResponse is returned by GET .../nodes.
type ListPlacementNodesRESTResponse struct {
	Nodes []*placement.NodeInfo `json:"nodes"`
}

// DrainPlacementNodeRESTResponse is returned by POST .../nodes/{id}/drain.
type DrainPlacementNodeRESTResponse struct {
	NodeID     string                 `json:"node_id"`
	Placements []*placement.Placement `json:"placements"`
}

// ReleasePlacementRESTResponse is returned by DELETE .../placements/{id}.
type ReleasePlacementRESTResponse struct {
	WorkloadID string `json:"workload_id"`
	Released   bool   `json:"released"`
}

// ListPlacementsRESTResponse is returned by GET .../placements.
type ListPlacementsRESTResponse struct {
	Placements []*placement.Placement `json:"placements"`
}

// placementSvc returns the placement service, answering 503 when absent.
func (s *Server) placementSvc(w http.ResponseWriter, r *http.Request) (service.PlacementService, bool) {
	svc := s.service.Placement()
	if svc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrPlacementServiceUnavailable)
		return nil, false
	}
	return svc, true
}

// requestDescribingStatus maps the placement failures that describe the
// REQUEST against the current pool — no node fits, the workload must be
// released first, the guarded write lost to concurrent writers — to their
// HTTP status. Their text is echoed to the caller rather than sanitised as a
// server fault: it is the answer they asked for. A lost write is 409, not the
// 500 qrdb's code table gives Aborted, because retrying it is correct.
var requestDescribingStatus = map[errors.ErrorCode]int{
	errors.ErrPreconditionFailed: http.StatusPreconditionFailed,
	errors.ErrAborted:            http.StatusConflict,
}

// respondPlacementError writes a placement failure.
func (s *Server) respondPlacementError(w http.ResponseWriter, r *http.Request, err error) {
	var qbErr *errors.QBError
	if stderrors.As(err, &qbErr) {
		if status, ok := requestDescribingStatus[qbErr.Code]; ok {
			RespondError(w, r, status, qbErr)
			return
		}
	}
	s.handleServiceError(w, r, err)
}

// decodePlacementBody decodes a JSON request body.
func decodePlacementBody(w http.ResponseWriter, r *http.Request, into any) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	if err := json.Unmarshal(body, into); err != nil {
		RespondBadRequest(w, r, "invalid JSON request body")
		return false
	}
	return true
}

// HandleRegisterPlacementNode handles POST /api/v1/scheduler/nodes.
func (s *Server) HandleRegisterPlacementNode(w http.ResponseWriter, r *http.Request) {
	var req service.RegisterPlacementNodeRequest
	if !decodePlacementBody(w, r, &req) {
		return
	}
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	info, err := svc.RegisterNode(r.Context(), &req)
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, info)
}

// HandleListPlacementNodes handles GET /api/v1/scheduler/nodes.
func (s *Server) HandleListPlacementNodes(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	nodes, err := svc.ListNodes(r.Context())
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &ListPlacementNodesRESTResponse{Nodes: nodes})
}

// placementNodeOp is a node-keyed placement call returning the node.
type placementNodeOp func(svc service.PlacementService, r *http.Request, nodeID string) (*placement.NodeInfo, error)

// servePlacementNodeOp runs a node-keyed call and writes the node.
func (s *Server) servePlacementNodeOp(w http.ResponseWriter, r *http.Request, op placementNodeOp) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	info, err := op(svc, r, chi.URLParam(r, "id"))
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, info)
}

// HandleGetPlacementNode handles GET /api/v1/scheduler/nodes/{id}.
func (s *Server) HandleGetPlacementNode(w http.ResponseWriter, r *http.Request) {
	s.servePlacementNodeOp(w, r, func(svc service.PlacementService, r *http.Request, id string) (*placement.NodeInfo, error) {
		return svc.GetNode(r.Context(), id)
	})
}

// HandleCordonPlacementNode handles POST /api/v1/scheduler/nodes/{id}/cordon.
func (s *Server) HandleCordonPlacementNode(w http.ResponseWriter, r *http.Request) {
	s.servePlacementNodeOp(w, r, func(svc service.PlacementService, r *http.Request, id string) (*placement.NodeInfo, error) {
		return svc.CordonNode(r.Context(), id)
	})
}

// HandleUncordonPlacementNode handles POST /api/v1/scheduler/nodes/{id}/uncordon.
func (s *Server) HandleUncordonPlacementNode(w http.ResponseWriter, r *http.Request) {
	s.servePlacementNodeOp(w, r, func(svc service.PlacementService, r *http.Request, id string) (*placement.NodeInfo, error) {
		return svc.UncordonNode(r.Context(), id)
	})
}

// HandleRenewPlacementNode handles POST /api/v1/scheduler/nodes/{id}/renew.
func (s *Server) HandleRenewPlacementNode(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := svc.RenewNode(r.Context(), id); err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &RenewPlacementNodeRESTResponse{NodeID: id, Renewed: true})
}

// HandleDeregisterPlacementNode handles DELETE /api/v1/scheduler/nodes/{id}.
func (s *Server) HandleDeregisterPlacementNode(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := svc.DeregisterNode(r.Context(), id); err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &DeregisterPlacementNodeRESTResponse{NodeID: id, Deregistered: true})
}

// HandleDrainPlacementNode handles POST /api/v1/scheduler/nodes/{id}/drain.
func (s *Server) HandleDrainPlacementNode(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	moved, err := svc.DrainNode(r.Context(), id)
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &DrainPlacementNodeRESTResponse{NodeID: id, Placements: moved})
}

// HandlePlaceWorkload handles POST /api/v1/scheduler/placements.
func (s *Server) HandlePlaceWorkload(w http.ResponseWriter, r *http.Request) {
	var req placement.PlacementRequest
	if !decodePlacementBody(w, r, &req) {
		return
	}
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	res, err := svc.Place(r.Context(), &req)
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, res)
}

// HandleListPlacements handles GET /api/v1/scheduler/placements, filtered by
// the optional node_id and group query parameters.
func (s *Server) HandleListPlacements(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	out, err := svc.ListPlacements(r.Context(), &placement.PlacementFilter{NodeID: q.Get("node_id"), Group: q.Get("group")})
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &ListPlacementsRESTResponse{Placements: out})
}

// HandleGetPlacement handles GET /api/v1/scheduler/placements/{id}.
func (s *Server) HandleGetPlacement(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	pl, err := svc.GetPlacement(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, pl)
}

// HandleReleasePlacement handles DELETE /api/v1/scheduler/placements/{id}.
func (s *Server) HandleReleasePlacement(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.placementSvc(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if err := svc.Release(r.Context(), id); err != nil {
		s.respondPlacementError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &ReleasePlacementRESTResponse{WorkloadID: id, Released: true})
}
