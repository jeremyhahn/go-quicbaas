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

// dek_handler.go provides typed REST routing for the quicbaas.DEK service.
//
// The generic dispatch path produced endpoints with literal {path} placeholders
// and returned an opaque map. This handler substitutes the DEK path from the
// typed SDK request and decodes responses directly into the service.*Response
// types the SDK DEK client expects — the server DEK handlers already serialize
// those service types verbatim, so no field-by-field re-mapping is required.

package rest

import (
	"context"
	"net/http"
	"net/url"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// dekServicePrefix is the gRPC-style service identifier the SDK uses to route
// DEK requests to this handler.
const dekServicePrefix = "quicbaas.DEK"

// isDEKService reports whether the supplied service name resolves to the DEK
// service.
func isDEKService(service string) bool {
	return trimRESTServicePrefix(service) == "DEK"
}

// dekRESTHandler delegates DEK service calls to the quicbaasd REST endpoints.
type dekRESTHandler struct {
	transport *Transport
}

// newDEKRESTHandler constructs a handler bound to t.
func newDEKRESTHandler(t *Transport) *dekRESTHandler {
	return &dekRESTHandler{transport: t}
}

// handleRequest dispatches the DEK service method to the matching REST
// endpoint.
func (h *dekRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "CreateDEK":
		return h.handleCreateDEK(ctx, req)
	case "GetDEK":
		return h.handleGetDEK(ctx, req)
	case "ListDEKs":
		return h.handleListDEKs(ctx, req)
	case "RotateDEK":
		return h.handleRotateDEK(ctx, req)
	case "DeleteDEK":
		return h.handleDeleteDEK(ctx, req)
	case "GetDEKStatus":
		return h.handleGetDEKStatus(ctx, req)
	case "SetDefaultDEK":
		return h.handleSetDefaultDEK(ctx, req)
	case "SetDEKSafety":
		return h.handleSetDEKSafety(ctx, req)
	case "ClearDEKSafety":
		return h.handleClearDEKSafety(ctx, req)
	case "StartReencryption":
		return h.handleStartReencryption(ctx, req)
	case "GetReencryptionProgress":
		return h.handleGetReencryptionProgress(ctx, req)
	default:
		return h.transport.requestWithRetry(ctx, dekServicePrefix, method, req)
	}
}

// dekPathFromRequest reads the exported Path field from a DEK request struct.
func dekPathFromRequest(req interface{}, op string) (string, error) {
	path, ok := extractStringField(req, "Path")
	if !ok || path == "" {
		return "", errors.NewInvalidInput(op, "path is required")
	}
	return path, nil
}

func (h *dekRESTHandler) handleCreateDEK(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dekclient.CreateDEKRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dek.CreateDEK", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.dek.CreateDEK", "path is required")
	}
	body := &service.CreateDEKRequest{
		Path:           r.Path,
		Algorithm:      service.KeyAlgorithm(r.Algorithm),
		Description:    r.Description,
		MaxOperations:  r.MaxOperations,
		MaxBytes:       r.MaxBytes,
		WarningPercent: r.WarningPercent,
	}
	var resp service.CreateDEKResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/kms/deks", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleGetDEK(ctx context.Context, req interface{}) (interface{}, error) {
	path, err := dekPathFromRequest(req, "rest.dek.GetDEK")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(path)
	var resp service.GetDEKResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleListDEKs(ctx context.Context, req interface{}) (interface{}, error) {
	endpoint := "/api/v1/kms/deks"
	if r, ok := req.(*dekclient.ListDEKsRequest); ok && r != nil {
		q := url.Values{}
		if r.Prefix != "" {
			q.Set("prefix", r.Prefix)
		}
		if r.Recursive {
			q.Set("recursive", "true")
		}
		if encoded := q.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
	}
	var resp service.ListDEKsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleRotateDEK(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dekclient.RotateDEKRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dek.RotateDEK", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.dek.RotateDEK", "path is required")
	}
	body := &service.RotateDEKRequest{
		Path:       r.Path,
		Background: r.Background,
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(r.Path) + "/rotate"
	var resp service.RotateDEKResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleDeleteDEK(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dekclient.DeleteDEKRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dek.DeleteDEK", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.dek.DeleteDEK", "path is required")
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(r.Path)
	q := url.Values{}
	if r.Force {
		q.Set("force", "true")
	}
	if r.MigrateTo != "" {
		q.Set("migrate_to", r.MigrateTo)
	}
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var resp service.DeleteDEKResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleGetDEKStatus(ctx context.Context, req interface{}) (interface{}, error) {
	path, err := dekPathFromRequest(req, "rest.dek.GetDEKStatus")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(path) + "/status"
	var resp service.GetDEKStatusResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleSetDefaultDEK(ctx context.Context, req interface{}) (interface{}, error) {
	path, err := dekPathFromRequest(req, "rest.dek.SetDefaultDEK")
	if err != nil {
		return nil, err
	}
	body := &service.SetDefaultDEKRequest{Path: path}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(path) + "/set-default"
	var resp service.SetDefaultDEKResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleSetDEKSafety(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*dekclient.SetDEKSafetyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dek.SetDEKSafety", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.dek.SetDEKSafety", "path is required")
	}
	body := &service.SetDEKSafetyRequest{
		Path:           r.Path,
		MaxOperations:  r.MaxOperations,
		MaxBytes:       r.MaxBytes,
		WarningPercent: r.WarningPercent,
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(r.Path) + "/safety"
	var resp service.SetDEKSafetyResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleClearDEKSafety(ctx context.Context, req interface{}) (interface{}, error) {
	path, err := dekPathFromRequest(req, "rest.dek.ClearDEKSafety")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(path) + "/safety"
	var resp service.ClearDEKSafetyResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleStartReencryption(ctx context.Context, req interface{}) (interface{}, error) {
	// The SDK DEK client already converts its public request into a
	// *service.StartReencryptionRequest before dispatching, so accept that
	// type directly.
	r, ok := req.(*service.StartReencryptionRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dek.StartReencryption", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.dek.StartReencryption", "path is required")
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(r.Path) + "/reencrypt"
	var resp service.StartReencryptionResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *dekRESTHandler) handleGetReencryptionProgress(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetReencryptionProgressRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.dek.GetReencryptionProgress", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.dek.GetReencryptionProgress", "path is required")
	}
	endpoint := "/api/v1/kms/deks/" + dekEscapePath(r.Path) + "/reencrypt"
	q := url.Values{}
	if r.OperationID != "" {
		q.Set("operation_id", r.OperationID)
	}
	if r.Verbose {
		q.Set("verbose", "true")
	}
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var resp service.GetReencryptionProgressResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// dekEscapePath escapes a DEK path for inclusion in a URL while preserving the
// hierarchical "/" separators the server's wildcard route depends on.
func dekEscapePath(path string) string {
	segments := splitPathSegments(path)
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return joinPathSegments(segments)
}
