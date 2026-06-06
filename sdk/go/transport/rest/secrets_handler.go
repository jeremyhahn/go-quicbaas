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

// secrets_handler.go provides typed REST routing for the quicbaas.Secrets
// service. The generic dispatch path mapped Secrets methods to endpoint
// templates with a literal {path} placeholder and returned an opaque map; this
// handler substitutes the concrete path, marshals the correct wire body, and
// returns the response types the SDK Secrets client expects.
//
// Methods without a precise server route mapping (GetVersion, SetRotationPolicy,
// GetRotationPolicy) fall through to the generic dispatch path.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// secretsServicePrefix is the gRPC-style service identifier the SDK uses to
// route Secrets requests to this handler.
const secretsServicePrefix = "quicbaas.Secrets"

// isSecretsService reports whether the supplied service name resolves to the
// Secrets service.
func isSecretsService(service string) bool {
	return trimRESTServicePrefix(service) == "Secrets"
}

// secretsRESTHandler delegates Secrets service calls to the quicbaasd REST
// endpoints.
type secretsRESTHandler struct {
	transport *Transport
}

// newSecretsRESTHandler constructs a handler bound to t.
func newSecretsRESTHandler(t *Transport) *secretsRESTHandler {
	return &secretsRESTHandler{transport: t}
}

// handleRequest dispatches the Secrets service method to the matching REST
// endpoint.
func (h *secretsRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "Store":
		return h.handleStore(ctx, req)
	case "Ensure":
		return h.handleEnsure(ctx, req)
	case "Get":
		return h.handleGet(ctx, req)
	case "List":
		return h.handleList(ctx, req)
	case "Delete":
		return h.handleDelete(ctx, req)
	case "ListVersions":
		return h.handleListVersions(ctx, req)
	case "Rotate":
		return h.handleRotate(ctx, req)
	case "Rollback":
		return h.handleRollback(ctx, req)
	case "UpdateStatus":
		return h.handleUpdateStatus(ctx, req)
	default:
		return h.transport.requestWithRetry(ctx, secretsServicePrefix, method, req)
	}
}

// secretsStoreWireRequest mirrors the server's store/rotate request body.
type secretsStoreWireRequest struct {
	Data     json.RawMessage   `json:"data"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// secretsRollbackWireRequest mirrors the server's rollback request body.
type secretsRollbackWireRequest struct {
	Version uint64 `json:"version"`
}

func (h *secretsRESTHandler) handleStore(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*secretsclient.StoreSecretRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.secrets.Store", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.secrets.Store", "path is required")
	}
	data, err := json.Marshal(r.Data)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.secrets.Store",
			"failed to encode secret data")
	}
	body := &secretsStoreWireRequest{Data: data, Metadata: r.Metadata}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(r.Path)
	var resp service.StoreSecretResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &secretsclient.StoreSecretResponse{Version: int(resp.Version)}, nil
}

func (h *secretsRESTHandler) handleEnsure(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*secretsclient.EnsureSecretRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.secrets.Ensure", "request is required")
	}
	if r.Path == "" {
		return nil, errors.NewInvalidInput("rest.secrets.Ensure", "path is required")
	}
	body := &service.EnsureSecretRequest{
		SecretID: r.Path,
		Length:   r.Length,
		Encoding: r.Encoding,
		TenantID: r.TenantID,
	}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(r.Path) + "/ensure"
	var resp service.EnsureSecretResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &secretsclient.EnsureSecretResponse{
		Path:      resp.SecretID,
		Data:      resp.Data,
		Generated: resp.Generated,
		Version:   int(resp.Version),
	}, nil
}

func (h *secretsRESTHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	path, version, ok := secretsGetParams(req)
	if !ok || path == "" {
		return nil, errors.NewInvalidInput("rest.secrets.Get", "path is required")
	}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(path)
	if version > 0 {
		endpoint += "?version=" + strconv.FormatUint(version, 10)
	}
	var resp service.GetSecretResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	secret := &secretsclient.Secret{
		Path:      resp.SecretID,
		Version:   int(resp.Version),
		Metadata:  resp.Metadata,
		CreatedAt: resp.CreatedAt,
	}
	if len(resp.Data) > 0 {
		decoded := map[string]interface{}{}
		if err := json.Unmarshal(resp.Data, &decoded); err == nil {
			secret.Data = decoded
		} else {
			secret.Data = map[string]interface{}{"value": string(resp.Data)}
		}
	}
	return secret, nil
}

func (h *secretsRESTHandler) handleList(ctx context.Context, req interface{}) (interface{}, error) {
	endpoint := "/api/v1/secrets"
	q := url.Values{}
	if prefix, ok := extractStringField(req, "Prefix"); ok && prefix != "" {
		q.Set("prefix", prefix)
	}
	if encoded := q.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var resp service.ListSecretsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(resp.Secrets))
	for _, s := range resp.Secrets {
		if s == nil {
			continue
		}
		if s.Name != "" {
			paths = append(paths, s.Name)
		} else {
			paths = append(paths, s.SecretID)
		}
	}
	return &secretsclient.ListSecretsResponse{Paths: paths}, nil
}

func (h *secretsRESTHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	path, ok := extractStringField(req, "Path")
	if !ok || path == "" {
		return nil, errors.NewInvalidInput("rest.secrets.Delete", "path is required")
	}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(path)
	var resp service.DeleteSecretResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	// The SDK Delete only needs a non-error result; returning nil signals
	// success without requiring the unexported deleteSecretResponse type.
	return nil, nil
}

func (h *secretsRESTHandler) handleListVersions(ctx context.Context, req interface{}) (interface{}, error) {
	secretID, ok := extractStringField(req, "SecretID")
	if !ok || secretID == "" {
		return nil, errors.NewInvalidInput("rest.secrets.ListVersions", "secret ID is required")
	}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(secretID) + "?versions=true"
	var resp service.ListSecretVersionsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *secretsRESTHandler) handleRotate(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*secretsclient.RotateSecretRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.secrets.Rotate", "request is required")
	}
	if r.SecretID == "" {
		return nil, errors.NewInvalidInput("rest.secrets.Rotate", "secret ID is required")
	}
	if len(r.NewData) == 0 {
		return nil, errors.NewInvalidInput("rest.secrets.Rotate", "new data is required")
	}
	data, err := json.Marshal(r.NewData)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.secrets.Rotate",
			"failed to encode secret data")
	}
	body := &secretsStoreWireRequest{Data: data, Metadata: r.Metadata}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(r.SecretID) + "?action=rotate"
	var resp service.RotateSecretResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *secretsRESTHandler) handleRollback(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*secretsclient.RollbackSecretRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.secrets.Rollback", "request is required")
	}
	if r.SecretID == "" {
		return nil, errors.NewInvalidInput("rest.secrets.Rollback", "secret ID is required")
	}
	if r.TargetVersion <= 0 {
		return nil, errors.NewInvalidInput("rest.secrets.Rollback", "target version must be positive")
	}
	body := &secretsRollbackWireRequest{Version: uint64(r.TargetVersion)}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(r.SecretID) + "?action=rollback"
	var resp service.RollbackSecretResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *secretsRESTHandler) handleUpdateStatus(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*secretsclient.UpdateSecretStatusRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.secrets.UpdateStatus", "request is required")
	}
	if r.SecretID == "" {
		return nil, errors.NewInvalidInput("rest.secrets.UpdateStatus", "secret ID is required")
	}
	suffix := "/enable"
	if r.Status == secretsclient.SecretStatusDisabled {
		suffix = "/disable"
	}
	endpoint := "/api/v1/secrets/" + secretsEscapePath(r.SecretID) + suffix
	var resp service.UpdateSecretStatusResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// secretsGetParams reads the Path and Version fields from the internal
// getSecretRequest the SDK constructs for Get.
func secretsGetParams(req interface{}) (string, uint64, bool) {
	path, ok := extractStringField(req, "Path")
	if !ok {
		return "", 0, false
	}
	version, _ := extractIntField(req, "Version")
	if version < 0 {
		version = 0
	}
	return path, uint64(version), true
}

// secretsEscapePath escapes a hierarchical secret path while preserving the "/"
// separators the server's wildcard route depends on.
func secretsEscapePath(path string) string {
	segments := splitPathSegments(path)
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return joinPathSegments(segments)
}
