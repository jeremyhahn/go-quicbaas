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
	path := sdlcBindingPath(sdkReq.ProjectID) + tenantQuery(sdkReq.TenantID)
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

// handleListForges reads the build's registered forge adapters. The route
// takes no tenant: the adapter set is the build's, not a tenant's.
func (h *sdlcRESTHandler) handleListForges(ctx context.Context, _ interface{}) (interface{}, error) {
	var resp sdlc.ListForgesResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/sdlc/forges", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
