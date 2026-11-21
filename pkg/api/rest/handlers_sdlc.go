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
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// SDLC module REST surface
// =============================================================================
//
//   GET    /api/v1/sdlc/projects/{projectID}/binding   sdlc:read
//   PUT    /api/v1/sdlc/projects/{projectID}/binding   sdlc:bind
//   DELETE /api/v1/sdlc/projects/{projectID}/binding   sdlc:unbind
//   POST   /api/v1/sdlc/projects/{projectID}/onboarding      sdlc:bind
//   POST   /api/v1/sdlc/repos/{repo}/onboarding              sdlc:bind
//   GET    /api/v1/sdlc/projects/{projectID}/conformance     sdlc:read
//   GET    /api/v1/sdlc/projects/{projectID}/open-work       sdlc:read
//   GET    /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer   sdlc:read
//   PUT    /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer   sdlc:bind
//   DELETE /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer   sdlc:bind
//   POST   /api/v1/sdlc/projects/{projectID}/issues          sdlc:issue_create
//   GET    /api/v1/sdlc/projects/{projectID}/collaborators   sdlc:read
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

// sdlcTenant decides the tenant an SDLC request operates in: the caller's
// own; naming another is refused for every caller. A request with no
// tenant bound is refused rather than trusted to name its own.
func sdlcTenant(r *http.Request, requested string) (string, bool) {
	tenantID, err := tenantctx.Target(r.Context(), "rest.SDLC", requested)
	return tenantID, err == nil
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
	if respondReferenceError(w, r, err) {
		return
	}
	var (
		notRegistered   *sdlc.ForgeNotRegisteredError
		strategy        *sdlc.StrategyNotRegisteredError
		projectNotFound *service.ProjectNotFoundError
	)
	switch {
	case errors.Is(err, sdlc.ErrNotFound), isOrchNotFoundError(err), errors.As(err, &projectNotFound):
		RespondError(w, r, http.StatusNotFound, err)
	case errors.Is(err, sdlc.ErrAlreadyExists), errors.Is(err, sdlc.ErrVersionConflict), errors.Is(err, sdlc.ErrWebhookRouteConflict),
		errors.Is(err, orchestration.ErrTaskInvalidState), errors.Is(err, orchestration.ErrTaskSourceAlreadyAdopted),
		errors.Is(err, sdlc.ErrAdoptionRefused):
		// ErrTaskInvalidState covers the attention inbox resolving an item
		// that is no longer blocked — another caller readmitted or failed it
		// first, exactly the race a 409 exists to report.
		RespondError(w, r, http.StatusConflict, err)
	case errors.Is(err, sdlc.ErrInvalidInput), errors.As(err, &notRegistered), errors.As(err, &strategy),
		errors.Is(err, orchestration.ErrWorkflowValidation), errors.Is(err, orchestration.ErrInvalidInput):
		RespondError(w, r, http.StatusBadRequest, err)
	case errors.Is(err, sdlc.ErrUnavailable):
		RespondError(w, r, http.StatusServiceUnavailable, err)
	case errors.Is(err, sdlc.ErrAdoptionIncomplete):
		// The task exists and runs; a forge write after it did not land. The
		// error names the task, and the forge is the party that failed.
		RespondError(w, r, http.StatusBadGateway, err)
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
	// The body is the binding itself, so the request's intent rides in the
	// query string.
	opts := service.BindOptions{}
	if raw := r.URL.Query().Get("paused"); raw != "" {
		paused, err := strconv.ParseBool(raw)
		if err != nil {
			RespondBadRequest(w, r, "paused must be true or false")
			return
		}
		opts.LeavePaused = paused
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
	bound, err := svc.Bind(r.Context(), tenantID, &binding, opts)
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

// HandleOnboardSDLCRepo onboards the repository in the path like the bound
// project the body names. Every step checks before it acts, so the same call
// twice reports every step unchanged the second time.
func (s *Server) HandleOnboardSDLCRepo(w http.ResponseWriter, r *http.Request) {
	svc := s.sdlcService(w, r)
	if svc == nil {
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
	var req sdlc.RepoOnboardRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	repo := chi.URLParam(r, "repo")
	if req.Repo == "" {
		req.Repo = repo
	}
	if req.Repo != repo {
		RespondBadRequest(w, r, "repo in the body must match the path")
		return
	}
	report, err := svc.OnboardRepo(r.Context(), tenantID, req)
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

// HandleGetSDLCOpenWork serves GET /api/v1/sdlc/projects/{projectID}/open-work.
//
// READ-ONLY. It answers what the factory has in flight on a bound project's
// forge: the open pull requests the project's agent opened, with their CI and
// review state, and the open issues carrying a label the module writes.
func (s *Server) HandleGetSDLCOpenWork(w http.ResponseWriter, r *http.Request) {
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
	work, err := svc.OpenWork(r.Context(), tenantID, projectID)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, work)
}

// =============================================================================
// The per-pull-request reviewer override
// =============================================================================

// SDLCReviewerOverrideRequest is the body of
// PUT /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer.
type SDLCReviewerOverrideRequest struct {
	// Profile names one of the binding's reviewer profiles.
	Profile string `json:"profile"`
}

// sdlcReviewerTarget resolves the module, the tenant, the project and the pull
// request every reviewer-override route acts on, or answers the refusal.
func (s *Server) sdlcReviewerTarget(w http.ResponseWriter, r *http.Request) (*service.SDLCService, string, string, int, bool) {
	svc := s.sdlcService(w, r)
	if svc == nil {
		return nil, "", "", 0, false
	}
	projectID := chi.URLParam(r, "projectID")
	if projectID == "" {
		RespondBadRequest(w, r, "project ID is required")
		return nil, "", "", 0, false
	}
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || number <= 0 {
		RespondBadRequest(w, r, "the pull request number must be a positive integer")
		return nil, "", "", 0, false
	}
	tenantID, ok := sdlcRequestTenant(w, r)
	if !ok {
		return nil, "", "", 0, false
	}
	return svc, tenantID, projectID, number, true
}

// HandleGetSDLCReviewer serves
// GET /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer: which
// reviewer profile reviews the pull request, and whether an operator chose it.
func (s *Server) HandleGetSDLCReviewer(w http.ResponseWriter, r *http.Request) {
	svc, tenantID, projectID, number, ok := s.sdlcReviewerTarget(w, r)
	if !ok {
		return
	}
	selection, err := svc.ReviewerSelection(r.Context(), tenantID, projectID, number)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, selection)
}

// HandleSetSDLCReviewer serves
// PUT /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer: an operator
// choosing which profile reviews one pull request.
func (s *Server) HandleSetSDLCReviewer(w http.ResponseWriter, r *http.Request) {
	svc, tenantID, projectID, number, ok := s.sdlcReviewerTarget(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()
	var req SDLCReviewerOverrideRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	selection, err := svc.SetReviewerOverride(r.Context(), tenantID, projectID, number, req.Profile)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, selection)
}

// HandleClearSDLCReviewer serves
// DELETE /api/v1/sdlc/projects/{projectID}/pulls/{number}/reviewer: the
// binding's default reviews the pull request again. It answers the selection
// that now holds rather than 204, so a caller renders the result without a
// second read.
func (s *Server) HandleClearSDLCReviewer(w http.ResponseWriter, r *http.Request) {
	svc, tenantID, projectID, number, ok := s.sdlcReviewerTarget(w, r)
	if !ok {
		return
	}
	selection, err := svc.ClearReviewerOverride(r.Context(), tenantID, projectID, number)
	if err != nil {
		respondSDLCError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, selection)
}
