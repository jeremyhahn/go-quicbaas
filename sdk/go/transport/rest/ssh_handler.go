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
	"context"
	"net/http"
	"net/url"
	"strings"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// sshRESTBasePath is the canonical REST mount for the SSH surface. It
// mirrors the routes registered in pkg/api/rest/server.go.
const sshRESTBasePath = "/api/v1/ssh"

// sshRESTHandler maps the SDK SSH verbs onto the REST routes.
type sshRESTHandler struct {
	transport *Transport
}

// newSSHRESTHandler creates a new SSH REST handler.
func newSSHRESTHandler(t *Transport) *sshRESTHandler {
	return &sshRESTHandler{transport: t}
}

// isSSHService reports whether service names the SSH service.
func isSSHService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "SSH"
}

// sshMethodHandler handles one SDK SSH method.
type sshMethodHandler func(ctx context.Context, req interface{}) (interface{}, error)

// handleRequest routes SSH requests using map-based O(1) dispatch.
func (h *sshRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handlers := map[string]sshMethodHandler{
		sshclient.MethodGenerateKey:         h.handleGenerateKey,
		sshclient.MethodImportKey:           h.handleImportKey,
		sshclient.MethodGetKey:              h.handleGetKey,
		sshclient.MethodListKeys:            h.handleListKeys,
		sshclient.MethodDeleteKey:           h.handleDeleteKey,
		sshclient.MethodExportPublicKey:     h.handleExportPublicKey,
		sshclient.MethodSignDigest:          h.handleSignDigest,
		sshclient.MethodAddKnownHost:        h.handleAddKnownHost,
		sshclient.MethodListKnownHosts:      h.handleListKnownHosts,
		sshclient.MethodRemoveKnownHost:     h.handleRemoveKnownHost,
		sshclient.MethodAddAuthorizedKey:    h.handleAddAuthorizedKey,
		sshclient.MethodListAuthorizedKeys:  h.handleListAuthorizedKeys,
		sshclient.MethodRemoveAuthorizedKey: h.handleRemoveAuthorizedKey,
		sshclient.MethodEmitAuthorizedKey:   h.handleEmitAuthorizedKey,
	}
	handler, ok := handlers[method]
	if !ok {
		return nil, errors.NewInvalidInput("ssh."+method, "unknown ssh method")
	}
	return handler(ctx, req)
}

// sshKeyPath builds the /keys/{scope}/{owner}/{name} path.
func sshKeyPath(scope, owner, name, suffix string) string {
	return sshRESTBasePath + "/keys/" +
		url.PathEscape(scope) + "/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + suffix
}

// sshScopeOwnerQuery builds the ?scope=&owner= query string.
func sshScopeOwnerQuery(scope, owner string) string {
	q := url.Values{}
	q.Set("scope", scope)
	q.Set("owner", owner)
	return "?" + q.Encode()
}

// =============================================================================
// Keys
// =============================================================================

func (h *sshRESTHandler) handleGenerateKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.GenerateRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.GenerateKey", "invalid request type")
	}
	var out sshclient.Key
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		sshRESTBasePath+"/keys", sdkReq, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleImportKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ImportKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.ImportKey", "invalid request type")
	}
	var out sshclient.Key
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		sshRESTBasePath+"/keys/import", sdkReq, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleGetKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.GetKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.GetKey", "invalid request type")
	}
	var out sshclient.Key
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		sshKeyPath(sdkReq.Scope, sdkReq.Owner, sdkReq.Name, ""), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleListKeys(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ListKeysRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.ListKeys", "invalid request type")
	}
	var out sshclient.ListKeysResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		sshRESTBasePath+"/keys"+sshScopeOwnerQuery(sdkReq.Scope, sdkReq.Owner), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleDeleteKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.DeleteKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.DeleteKey", "invalid request type")
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete,
		sshKeyPath(sdkReq.Scope, sdkReq.Owner, sdkReq.Name, ""), nil, nil); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (h *sshRESTHandler) handleExportPublicKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ExportRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.ExportPublicKey", "invalid request type")
	}
	var out sshclient.PublicKey
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		sshKeyPath(sdkReq.Scope, sdkReq.Owner, sdkReq.Name, "/public"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleSignDigest(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.SignRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.SignDigest", "invalid request type")
	}
	body := struct {
		Digest []byte `json:"digest"`
	}{Digest: sdkReq.Digest}
	var out sshclient.SignResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		sshKeyPath(sdkReq.Scope, sdkReq.Owner, sdkReq.Name, "/sign"), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// =============================================================================
// Known hosts
// =============================================================================

func (h *sshRESTHandler) handleAddKnownHost(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.AddKnownHostRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.AddKnownHost", "invalid request type")
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		sshRESTBasePath+"/known-hosts", sdkReq, nil); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (h *sshRESTHandler) handleListKnownHosts(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ListKnownHostsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.ListKnownHosts", "invalid request type")
	}
	var out sshclient.ListKnownHostsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		sshRESTBasePath+"/known-hosts"+sshScopeOwnerQuery(sdkReq.Scope, sdkReq.Owner), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleRemoveKnownHost(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.RemoveKnownHostRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.RemoveKnownHost", "invalid request type")
	}
	path := sshRESTBasePath + "/known-hosts/" + url.PathEscape(sdkReq.Host) +
		sshScopeOwnerQuery(sdkReq.Scope, sdkReq.Owner)
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

// =============================================================================
// Authorized keys
// =============================================================================

func (h *sshRESTHandler) handleAddAuthorizedKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.AddAuthorizedKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.AddAuthorizedKey", "invalid request type")
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		sshRESTBasePath+"/authorized/keys", sdkReq, nil); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (h *sshRESTHandler) handleListAuthorizedKeys(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.ListAuthorizedKeysRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.ListAuthorizedKeys", "invalid request type")
	}
	var out sshclient.ListAuthorizedKeysResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet,
		sshRESTBasePath+"/authorized/keys"+sshScopeOwnerQuery(sdkReq.Scope, sdkReq.Owner), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (h *sshRESTHandler) handleRemoveAuthorizedKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.RemoveAuthorizedKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.RemoveAuthorizedKey", "invalid request type")
	}
	path := sshRESTBasePath + "/authorized/keys/" +
		url.PathEscape(sdkReq.Scope) + "/" + url.PathEscape(sdkReq.Owner) + "/" + url.PathEscape(sdkReq.Name)
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (h *sshRESTHandler) handleEmitAuthorizedKey(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*sshclient.EmitRequest)
	if !ok {
		return nil, errors.NewInvalidInput("ssh.EmitAuthorizedKey", "invalid request type")
	}
	var out sshclient.EmitResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		sshRESTBasePath+"/authorized", sdkReq, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
