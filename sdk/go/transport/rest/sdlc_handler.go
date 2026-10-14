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
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// sdlcRESTHandler routes the quicbaas.SDLC service onto the module's REST
// surface: the binding under a project and the delivery under a task.
type sdlcRESTHandler struct {
	transport *Transport
}

func newSDLCRESTHandler(t *Transport) *sdlcRESTHandler {
	return &sdlcRESTHandler{transport: t}
}

// SupportsSDLC declares the SDLC service on this transport.
func (t *Transport) SupportsSDLC() bool { return true }

var _ sdlc.SDLCCapableTransport = (*Transport)(nil)

// isSDLCService reports whether the service name is the SDLC service.
func isSDLCService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "SDLC"
}

type sdlcMethodHandler func(ctx context.Context, req interface{}) (interface{}, error)

// sdlcMethods dispatches by method name.
var sdlcMethods = map[string]func(*sdlcRESTHandler) sdlcMethodHandler{
	"Bind":        func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleBind },
	"Unbind":      func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleUnbind },
	"Binding":     func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleBinding },
	"Delivery":    func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleDelivery },
	"ListForges":  func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleListForges },
	"Onboard":     func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleOnboard },
	"Conformance": func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleConformance },
	"OpenWork":    func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleOpenWork },

	"Reviewer":           func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleReviewer },
	"SetReviewer":        func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleSetReviewer },
	"AdoptIssue":         func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleAdoptIssue },
	"ClearReviewer":      func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleClearReviewer },
	"WorkerImageStatus":  func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleWorkerImageStatus },
	"RefreshWorkerImage": func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleRefreshWorkerImage },
	"FileIssue":          func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleFileIssue },
	"Collaborators":      func(h *sdlcRESTHandler) sdlcMethodHandler { return h.handleCollaborators },
}

func (h *sdlcRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	bind, ok := sdlcMethods[method]
	if !ok {
		return nil, errors.NewInvalidInput("sdlc."+method, "unknown method")
	}
	return bind(h)(ctx, req)
}

func sdlcBindingPath(projectID string) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/binding"
}

func sdlcOnboardingPath(projectID string) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/onboarding"
}

func sdlcConformancePath(projectID string) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/conformance"
}

func sdlcOpenWorkPath(projectID string) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/open-work"
}

func sdlcReviewerPath(projectID string, pullRequest int) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/pulls/" + strconv.Itoa(pullRequest) + "/reviewer"
}

func sdlcIssuesPath(projectID string) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/issues"
}

func sdlcCollaboratorsPath(projectID string) string {
	return "/api/v1/sdlc/projects/" + url.PathEscape(projectID) + "/collaborators"
}

func sdlcDeliveryPath(taskID string) string {
	return "/api/v1/sdlc/tasks/" + url.PathEscape(taskID) + "/delivery"
}

func tenantQuery(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	return "?tenant_id=" + url.QueryEscape(tenantID)
}

func (h *sdlcRESTHandler) handleBind(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.BindRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Bind", "invalid request type")
	}
	var resp sdlc.ProjectBinding
	query := url.Values{}
	if sdkReq.TenantID != "" {
		query.Set("tenant_id", sdkReq.TenantID)
	}
	if sdkReq.Paused {
		query.Set("paused", "true")
	}
	path := sdlcBindingPath(sdkReq.ProjectID)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, path, &sdkReq.Binding, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *sdlcRESTHandler) handleUnbind(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.UnbindRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Unbind", "invalid request type")
	}
	path := sdlcBindingPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return nil, err
	}
	return &sdlc.UnbindResponse{Unbound: true}, nil
}

func (h *sdlcRESTHandler) handleBinding(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.GetBindingRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Binding", "invalid request type")
	}
	var resp sdlc.ProjectBinding
	path := sdlcBindingPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *sdlcRESTHandler) handleDelivery(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.GetDeliveryRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Delivery", "invalid request type")
	}
	var resp sdlc.Delivery
	path := sdlcDeliveryPath(sdkReq.TaskID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleOnboard provisions the project's repository. A POST, because it
// writes to the forge; the read is a different route entirely.
func (h *sdlcRESTHandler) handleOnboard(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.OnboardRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Onboard", "invalid request type")
	}
	var resp sdlc.OnboardingReport
	path := sdlcOnboardingPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleConformance reads the project's onboarding conformance and changes
// nothing.
func (h *sdlcRESTHandler) handleConformance(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.ConformanceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Conformance", "invalid request type")
	}
	var resp sdlc.ConformanceReport
	path := sdlcConformancePath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleOpenWork reads a bound project's open work and changes nothing.
func (h *sdlcRESTHandler) handleOpenWork(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.OpenWorkRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.OpenWork", "invalid request type")
	}
	var resp sdlc.OpenWork
	path := sdlcOpenWorkPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleReviewer reads which reviewer profile reviews one pull request.
func (h *sdlcRESTHandler) handleReviewer(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.ReviewerRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Reviewer", "invalid request type")
	}
	var resp sdlc.ReviewerSelection
	path := sdlcReviewerPath(sdkReq.ProjectID, sdkReq.PullRequest) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSetReviewer records an operator's reviewer choice for one pull
// request. The body carries the profile and nothing else: the path already
// names the project and the pull request.
func (h *sdlcRESTHandler) handleSetReviewer(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.SetReviewerRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.SetReviewer", "invalid request type")
	}
	var resp sdlc.ReviewerSelection
	path := sdlcReviewerPath(sdkReq.ProjectID, sdkReq.PullRequest) + tenantQuery(sdkReq.TenantID)
	body := struct {
		Profile string `json:"profile"`
	}{Profile: sdkReq.Profile}
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, path, &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleClearReviewer removes one pull request's reviewer override.
func (h *sdlcRESTHandler) handleClearReviewer(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.ReviewerRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.ClearReviewer", "invalid request type")
	}
	var resp sdlc.ReviewerSelection
	path := sdlcReviewerPath(sdkReq.ProjectID, sdkReq.PullRequest) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListForges reads the build's registered forge adapters. The route
// takes no tenant: the adapter set is the build's, not a tenant's.
func (h *sdlcRESTHandler) handleListForges(ctx context.Context, _ interface{}) (interface{}, error) {
	var resp sdlc.ListForgesResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/sdlc/forges", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// sdlcWorkerImagePath is the worker image refresh's route. It takes no tenant:
// the refresh is a host verb over every tenant's bindings.
const sdlcWorkerImagePath = "/api/v1/sdlc/worker-image"

// handleWorkerImageStatus reads the refresh loop's state and its last pass.
func (h *sdlcRESTHandler) handleWorkerImageStatus(ctx context.Context, _ interface{}) (interface{}, error) {
	var resp sdlc.WorkerImageStatus
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, sdlcWorkerImagePath, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleRefreshWorkerImage queues a pass. The server answers 202 with the
// status the request left behind.
func (h *sdlcRESTHandler) handleRefreshWorkerImage(ctx context.Context, _ interface{}) (interface{}, error) {
	var resp sdlc.WorkerImageStatus
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, sdlcWorkerImagePath+"/refresh", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleFileIssue files an issue on the bound repository. The body carries
// the filing and nothing else: the path already names the project.
func (h *sdlcRESTHandler) handleFileIssue(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.FileIssueRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.FileIssue", "invalid request type")
	}
	body := struct {
		Title  string   `json:"title"`
		Body   string   `json:"body,omitempty"`
		Labels []string `json:"labels,omitempty"`
		Parent int      `json:"parent,omitempty"`
	}{Title: sdkReq.Title, Body: sdkReq.Body, Labels: sdkReq.Labels, Parent: sdkReq.Parent}
	var resp sdlc.ForgeIssue
	path := sdlcIssuesPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, path, &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleAdoptIssue adopts one issue: POST .../projects/{id}/adopt. The path
// names the project; the body carries the rest.
func (h *sdlcRESTHandler) handleAdoptIssue(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.AdoptIssueRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.AdoptIssue", "invalid request type")
	}
	var resp sdlc.Adoption
	path := "/api/v1/sdlc/projects/" + url.PathEscape(sdkReq.ProjectID) + "/adopt" + tenantQuery(sdkReq.TenantID)
	body := struct {
		Issue       int    `json:"issue"`
		PullRequest int    `json:"pull_request,omitempty"`
		Ruling      string `json:"ruling,omitempty"`
	}{Issue: sdkReq.Issue, PullRequest: sdkReq.PullRequest, Ruling: sdkReq.Ruling}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, path, &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleCollaborators reads the accounts that may rule on the bound
// repository.
func (h *sdlcRESTHandler) handleCollaborators(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sdlc.CollaboratorsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("sdlc.Collaborators", "invalid request type")
	}
	var resp sdlc.CollaboratorsResponse
	path := sdlcCollaboratorsPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
