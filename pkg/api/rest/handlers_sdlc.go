//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// SDLC module REST surface
// =============================================================================
//
//   GET    /api/v1/sdlc/projects/{projectID}/binding   sdlc:read
//   PUT    /api/v1/sdlc/projects/{projectID}/binding   sdlc:bind
//   DELETE /api/v1/sdlc/projects/{projectID}/binding   sdlc:unbind
//   POST   /api/v1/sdlc/projects/{projectID}/onboarding      sdlc:bind
//   GET    /api/v1/sdlc/projects/{projectID}/conformance     sdlc:read
//   GET    /api/v1/sdlc/tasks/{taskID}/delivery        sdlc:read
//   POST   /api/v1/sdlc/webhook/{projectID}            public (HMAC)
//
// Tenancy is the caller's own: the tenant bound to the request by the
// authenticator wins, and a tenant_id query naming another tenant is
// refused. Everything else about a task — its status, attempts, blocked
// reason — is the orchestrator surface's.

// ErrSDLCServiceUnavailable reports the module not running on this node.
var ErrSDLCServiceUnavailable = errors.New("the sdlc module is not running on this node")

// ErrSDLCTenantMismatch reports a tenant_id that is not the caller's own.
var ErrSDLCTenantMismatch = errors.New("tenant_id must be the caller's own tenant")

// sdlcTenant decides the tenant an SDLC request operates in.
func sdlcTenant(r *http.Request, requested string) (string, bool) {
	bound := iam.TenantFromContext(r.Context())
	switch {
	case bound == "" && requested == "":
		return "", false
	case bound == "":
		return requested, true
	case requested != "" && requested != bound:
		return "", false
	default:
		return bound, true
	}
}

// sdlcService returns the binding service or answers 503.
func (s *Server) sdlcService(w http.ResponseWriter, r *http.Request) *service.SDLCService {
	svc := s.service.SDLC()
	if svc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrSDLCServiceUnavailable)
		return nil
	}
	return svc
}

// respondSDLCError maps the module's typed errors onto statuses.
func respondSDLCError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		notRegistered   *sdlc.ForgeNotRegisteredError
		strategy        *sdlc.StrategyNotRegisteredError
		projectNotFound *service.ProjectNotFoundError
	)
	switch {
	case errors.Is(err, sdlc.ErrNotFound), isOrchNotFoundError(err), errors.As(err, &projectNotFound):
		RespondError(w, r, http.StatusNotFound, err)
	case errors.Is(err, sdlc.ErrAlreadyExists), errors.Is(err, sdlc.ErrVersionConflict), errors.Is(err, sdlc.ErrWebhookRouteConflict):
		RespondError(w, r, http.StatusConflict, err)
	case errors.Is(err, sdlc.ErrInvalidInput), errors.As(err, &notRegistered), errors.As(err, &strategy),
		errors.Is(err, orchestration.ErrWorkflowValidation):
		RespondError(w, r, http.StatusBadRequest, err)
	default:
		RespondError(w, r, http.StatusInternalServerError, err)
	}
}

// sdlcRequestTenant resolves the tenant for a request or answers 403.
func sdlcRequestTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID, ok := sdlcTenant(r, r.URL.Query().Get("tenant_id"))
	if !ok {
		RespondError(w, r, http.StatusForbidden, ErrSDLCTenantMismatch)
		return "", false
	}
	return tenantID, true
}

// SDLCForgesResponse lists the forge platforms this build can talk to.
type SDLCForgesResponse struct {
	// Forges are the registered platform types, sorted so two reads
	// serialize identically.
	Forges []string `json:"forges"`

	// Count is len(Forges).
	Count int `json:"count"`
}

// HandleListSDLCForges serves GET /api/v1/sdlc/forges — the forge adapters
// this build actually carries.
//
// It exists so a caller can show an unavailable platform DISABLED with the
// reason rather than hide it: a project may only bind to a forge some adapter
// serves (NewForgeBackend is fail-closed), and a selector that silently omits
// github says nothing about why. This is BUILD capability, not tenant data,
// so no tenant is derived; the module-not-running answer stays the 503 the
// neighbouring handlers give, distinct from a 200 listing nothing.
func (s *Server) HandleListSDLCForges(w http.ResponseWriter, r *http.Request) {
	if svc := s.sdlcService(w, r); svc == nil {
		return
	}
	registered := sdlc.RegisteredForgeTypes()
	forges := make([]string, 0, len(registered))
	for _, forgeType := range registered {
		forges = append(forges, string(forgeType))
	}
	sort.Strings(forges)
	RespondJSON(w, r, http.StatusOK, SDLCForgesResponse{Forges: forges, Count: len(forges)})
}

// HandleGetSDLCBinding serves GET /api/v1/sdlc/projects/{projectID}/binding.
func (s *Server) HandleGetSDLCBinding(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	binding, err := svc.Binding(r.Context(), tenantID, projectID)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, binding)
}

// HandleBindSDLCProject serves PUT /api/v1/sdlc/projects/{projectID}/binding.
func (s *Server) HandleBindSDLCProject(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()
	var binding sdlc.ProjectBinding
	if err := json.Unmarshal(body, &binding); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	if binding.ProjectID == "" {
		binding.ProjectID = projectID
	}
	if binding.ProjectID != projectID {
		RespondBadRequest(w, r, "project_id in the body must match the path")
		return
	}
	bound, err := svc.Bind(r.Context(), tenantID, &binding)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, bound)
}

// HandleUnbindSDLCProject serves DELETE /api/v1/sdlc/projects/{projectID}/binding.
func (s *Server) HandleUnbindSDLCProject(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	if err := svc.Unbind(r.Context(), tenantID, projectID); err != nil {
		respondSDLCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetSDLCDelivery serves GET /api/v1/sdlc/tasks/{taskID}/delivery.
func (s *Server) HandleGetSDLCDelivery(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		RespondBadRequest(w, r, "task ID is required")
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	delivery, err := svc.Delivery(r.Context(), tenantID, taskID)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, delivery)
}

// HandleOnboardSDLCProject serves
// POST /api/v1/sdlc/projects/{projectID}/onboarding.
//
// A POST rather than a flag on the conformance GET, and the separation is the
// design rather than REST pedantry: an operator asking what state a
// repository is in must be able to ask WITHOUT changing it. Repair is a
// deliberate second call under a write permission.
func (s *Server) HandleOnboardSDLCProject(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	report, err := svc.Onboard(r.Context(), tenantID, projectID)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}

// HandleGetSDLCConformance serves
// GET /api/v1/sdlc/projects/{projectID}/conformance.
//
// READ-ONLY. It answers "is this project correctly onboarded, and if not,
// what is wrong", naming each failure class distinctly with its subject and
// its remedy. A non-conformant project is still a 200: the report IS the
// answer, and a status code cannot carry which of seven things went wrong.
func (s *Server) HandleGetSDLCConformance(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return
	}
	report, err := svc.Conformance(r.Context(), tenantID, projectID)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, report)
}
