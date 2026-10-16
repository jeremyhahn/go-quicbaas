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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsPrompts declares the Prompts service on the REST transport, and so
// on the QUIC transport that embeds it.
func (t *Transport) SupportsPrompts() bool { return true }

var _ prompt.PromptsCapableTransport = (*Transport)(nil)

// promptRESTHandler serves the Prompts service over /api/v1/prompts.
type promptRESTHandler struct {
	transport *Transport
}

func newPromptRESTHandler(t *Transport) *promptRESTHandler {
	return &promptRESTHandler{transport: t}
}

// isPromptsService reports whether the service name is the Prompts service.
func isPromptsService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Prompts"
}

type promptMethodHandler func(h *promptRESTHandler, ctx context.Context, req interface{}) (interface{}, error)

// promptMethods dispatches by method name.
var promptMethods = map[string]promptMethodHandler{
	"List":      (*promptRESTHandler).handleList,
	"Get":       (*promptRESTHandler).handleGet,
	"Effective": (*promptRESTHandler).handleEffective,
	"Diff":      (*promptRESTHandler).handleDiff,
	"History":   (*promptRESTHandler).handleHistory,
	"Put":       (*promptRESTHandler).handlePut,
	"Rollback":  (*promptRESTHandler).handleRollback,
	"Reset":     (*promptRESTHandler).handleReset,
}

func (h *promptRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handler, ok := promptMethods[method]
	if !ok {
		return nil, errors.NewInvalidInput("prompt."+method, "unknown method")
	}
	return handler(h, ctx, req)
}

// promptRequest asserts the SDK request type of a verb.
func promptRequest[T any](method string, req interface{}) (*T, error) {
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("prompt."+method, "invalid request type")
	}
	return r, nil
}

// promptQuery is the scope part of a prompt address as query parameters.
func promptQuery(tenantID, scope, projectID string) url.Values {
	query := url.Values{}
	for key, value := range map[string]string{"tenant_id": tenantID, "scope": scope, "project_id": projectID} {
		if value != "" {
			query.Set(key, value)
		}
	}
	return query
}

// promptPath is a prompt's endpoint, plus a sub-resource when suffix is set,
// with the query appended.
func promptPath(addr *prompt.Address, suffix string, query url.Values) string {
	path := "/api/v1/prompts/" + url.PathEscape(addr.Set) + "/" + url.PathEscape(addr.Name) + suffix
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path
}

// addressPath is a prompt's endpoint addressed at addr's scope.
func addressPath(addr *prompt.Address, suffix string) string {
	return promptPath(addr, suffix, promptQuery(addr.TenantID, addr.Scope, addr.ProjectID))
}

// send makes one request and decodes the answer into out.
func (h *promptRESTHandler) send(ctx context.Context, httpMethod, path string, body interface{}, out interface{}) (interface{}, error) {
	if err := h.transport.doJSONRequest(ctx, httpMethod, path, body, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *promptRESTHandler) handleList(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := promptRequest[prompt.ListRequest]("List", req)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/prompts/" + url.PathEscape(r.Set)
	if encoded := promptQuery(r.TenantID, r.Scope, r.ProjectID).Encode(); encoded != "" {
		path += "?" + encoded
	}
	return h.send(ctx, http.MethodGet, path, nil, &prompt.ListResponse{})
}

func (h *promptRESTHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := promptRequest[prompt.GetRequest]("Get", req)
	if err != nil {
		return nil, err
	}
	query := promptQuery(r.TenantID, r.Scope, r.ProjectID)
	if r.Version != 0 {
		query.Set("version", strconv.FormatInt(r.Version, 10))
	}
	return h.send(ctx, http.MethodGet, promptPath(&r.Address, "", query), nil, &prompt.Record{})
}

func (h *promptRESTHandler) handleEffective(ctx context.Context, req interface{}) (interface{}, error) {
	addr, err := promptRequest[prompt.Address]("Effective", req)
	if err != nil {
		return nil, err
	}
	return h.send(ctx, http.MethodGet, addressPath(addr, "/effective"), nil, &prompt.EffectivePrompt{})
}

func (h *promptRESTHandler) handleDiff(ctx context.Context, req interface{}) (interface{}, error) {
	addr, err := promptRequest[prompt.Address]("Diff", req)
	if err != nil {
		return nil, err
	}
	return h.send(ctx, http.MethodGet, addressPath(addr, "/diff"), nil, &prompt.Diff{})
}

func (h *promptRESTHandler) handleHistory(ctx context.Context, req interface{}) (interface{}, error) {
	addr, err := promptRequest[prompt.Address]("History", req)
	if err != nil {
		return nil, err
	}
	return h.send(ctx, http.MethodGet, addressPath(addr, "/history"), nil, &prompt.History{})
}

func (h *promptRESTHandler) handlePut(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := promptRequest[prompt.PutRequest]("Put", req)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{"content": r.Content, "note": r.Note, "expected_version": r.ExpectedVersion}
	return h.send(ctx, http.MethodPut, addressPath(&r.Address, ""), body, &prompt.Record{})
}

func (h *promptRESTHandler) handleRollback(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := promptRequest[prompt.RollbackRequest]("Rollback", req)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{"to_version": r.ToVersion, "note": r.Note, "expected_version": r.ExpectedVersion}
	return h.send(ctx, http.MethodPost, addressPath(&r.Address, "/rollback"), body, &prompt.Record{})
}

func (h *promptRESTHandler) handleReset(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := promptRequest[prompt.ResetRequest]("Reset", req)
	if err != nil {
		return nil, err
	}
	query := promptQuery(r.TenantID, r.Scope, r.ProjectID)
	query.Set("expected_version", strconv.FormatInt(r.ExpectedVersion, 10))
	if r.Note != "" {
		query.Set("note", r.Note)
	}
	return h.send(ctx, http.MethodDelete, promptPath(&r.Address, "", query), nil, &prompt.Record{})
}
