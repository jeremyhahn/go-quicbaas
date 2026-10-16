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
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// The prompt surface: the eight verbs over a tenant's prompt sets, under
// /api/v1/prompts/{set}[/{name}]. A prompt is addressed by the tenant_id,
// scope (tenant | project | system) and project_id query parameters; the
// tenant is the caller's own, and the system scope is read-only.

// ErrPromptTenantMismatch reports a tenant_id that is not the caller's own.
var ErrPromptTenantMismatch = errors.New("tenant_id must be the caller's own tenant")

// promptFaultStatus is the HTTP status of each prompt refusal.
var promptFaultStatus = map[service.PromptFault]int{
	service.PromptFaultInvalid:     http.StatusBadRequest,
	service.PromptFaultNotFound:    http.StatusNotFound,
	service.PromptFaultConflict:    http.StatusConflict,
	service.PromptFaultUnavailable: http.StatusServiceUnavailable,
}

// PutPromptBody is the body of PUT /api/v1/prompts/{set}/{name}.
type PutPromptBody struct {
	Content         string `json:"content"`
	Note            string `json:"note"`
	ExpectedVersion int64  `json:"expected_version"`
}

// RollbackPromptBody is the body of POST /api/v1/prompts/{set}/{name}/rollback.
type RollbackPromptBody struct {
	ToVersion       int64  `json:"to_version"`
	Note            string `json:"note"`
	ExpectedVersion int64  `json:"expected_version"`
}

// promptService returns the prompt service, answering 503 when the prompt
// store is not mounted on this node.
func (s *Server) promptService(w http.ResponseWriter, r *http.Request) *service.PromptService {
	svc := s.service.Prompts()
	if svc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, service.ErrPromptsUnavailable)
		return nil
	}
	return svc
}

// promptAddress reads the prompt a request names: the set and name from the
// path, the scope from the query, and the caller's own tenant. A tenant_id
// naming another tenant is answered 403.
func promptAddress(w http.ResponseWriter, r *http.Request) (service.PromptAddress, bool) {
	query := r.URL.Query()
	tenantID, err := tenantctx.Target(r.Context(), "rest.Prompts", query.Get("tenant_id"))
	if err != nil {
		RespondError(w, r, http.StatusForbidden, ErrPromptTenantMismatch)
		return service.PromptAddress{}, false
	}
	return service.PromptAddress{
		TenantID:  tenantID,
		Scope:     query.Get("scope"),
		ProjectID: query.Get("project_id"),
		Set:       chi.URLParam(r, "set"),
		Name:      chi.URLParam(r, "name"),
	}, true
}

// promptTarget resolves the service and the address of a prompt request.
func (s *Server) promptTarget(w http.ResponseWriter, r *http.Request) (*service.PromptService, service.PromptAddress, bool) {
	svc := s.promptService(w, r)
	if svc == nil {
		return nil, service.PromptAddress{}, false
	}
	addr, ok := promptAddress(w, r)
	if !ok {
		return nil, service.PromptAddress{}, false
	}
	return svc, addr, true
}

// respondPromptError answers a prompt service error with the status of its
// fault; an error the service does not classify is the server's.
func respondPromptError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	if fault, ok := service.ClassifyPromptError(err); ok {
		status = promptFaultStatus[fault]
	}
	RespondError(w, r, status, err)
}

// respondPrompt answers a verb's result, or its error.
func respondPrompt(w http.ResponseWriter, r *http.Request, result any, err error) {
	if err != nil {
		respondPromptError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, result)
}

// decodePromptBody reads a JSON request body into out, answering 400 when
// it cannot.
func decodePromptBody(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	if err := json.Unmarshal(body, out); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return false
	}
	return true
}

// promptQueryInt reads an optional integer query parameter, answering 400
// when it is present and not an integer.
func promptQueryInt(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, name+" must be an integer")
		return 0, false
	}
	return value, true
}

// HandleListPrompts handles GET /api/v1/prompts/{set}: every prompt of the
// set at the addressed scope, with what is overridden there and what each
// prompt resolves to.
func (s *Server) HandleListPrompts(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	got, err := svc.List(r.Context(), &service.ListPromptsRequest{
		TenantID: addr.TenantID, Scope: addr.Scope, ProjectID: addr.ProjectID, Set: addr.Set,
	})
	respondPrompt(w, r, got, err)
}

// HandleGetPrompt handles GET /api/v1/prompts/{set}/{name}: the record
// overridden at the addressed scope, or with ?version=N one of its
// revisions. Nothing overridden there is 404.
func (s *Server) HandleGetPrompt(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	version, ok := promptQueryInt(w, r, "version")
	if !ok {
		return
	}
	got, err := svc.Get(r.Context(), &service.GetPromptRequest{PromptAddress: addr, Version: version})
	respondPrompt(w, r, got, err)
}

// HandleGetEffectivePrompt handles GET /api/v1/prompts/{set}/{name}/effective:
// the body a job at the addressed scope runs with, its origin and the chain.
func (s *Server) HandleGetEffectivePrompt(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	got, err := svc.Effective(r.Context(), &addr)
	respondPrompt(w, r, got, err)
}

// HandleDiffPrompt handles GET /api/v1/prompts/{set}/{name}/diff: the
// effective body at the addressed scope against its parent's.
func (s *Server) HandleDiffPrompt(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	got, err := svc.Diff(r.Context(), &addr)
	respondPrompt(w, r, got, err)
}

// HandleGetPromptHistory handles GET /api/v1/prompts/{set}/{name}/history:
// the record's revision log, identifiers only.
func (s *Server) HandleGetPromptHistory(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	got, err := svc.History(r.Context(), &addr)
	respondPrompt(w, r, got, err)
}

// HandlePutPrompt handles PUT /api/v1/prompts/{set}/{name}: an edit at the
// addressed scope, guarded by expected_version and explained by a note.
func (s *Server) HandlePutPrompt(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	var body PutPromptBody
	if !decodePromptBody(w, r, &body) {
		return
	}
	got, err := svc.Put(r.Context(), &service.PutPromptRequest{
		PromptAddress: addr, Content: body.Content, Note: body.Note, ExpectedVersion: body.ExpectedVersion,
	})
	respondPrompt(w, r, got, err)
}

// HandleRollbackPrompt handles POST /api/v1/prompts/{set}/{name}/rollback:
// a new revision carrying an earlier one's body.
func (s *Server) HandleRollbackPrompt(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	var body RollbackPromptBody
	if !decodePromptBody(w, r, &body) {
		return
	}
	got, err := svc.Rollback(r.Context(), &service.RollbackPromptRequest{
		PromptAddress: addr, ToVersion: body.ToVersion, Note: body.Note, ExpectedVersion: body.ExpectedVersion,
	})
	respondPrompt(w, r, got, err)
}

// HandleResetPrompt handles DELETE /api/v1/prompts/{set}/{name}: the record
// follows its parent again. A DELETE carries no body, so expected_version
// and note are query parameters.
func (s *Server) HandleResetPrompt(w http.ResponseWriter, r *http.Request) {
	svc, addr, ok := s.promptTarget(w, r)
	if !ok {
		return
	}
	expected, ok := promptQueryInt(w, r, "expected_version")
	if !ok {
		return
	}
	got, err := svc.Reset(r.Context(), &service.ResetPromptRequest{
		PromptAddress: addr, Note: r.URL.Query().Get("note"), ExpectedVersion: expected,
	})
	respondPrompt(w, r, got, err)
}
