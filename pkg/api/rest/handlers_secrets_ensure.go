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
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandleEnsureSecret handles POST /api/v1/secrets/{path...}/ensure —
// Vault-style generate-on-first-access. First call produces a fresh
// random value per the requested SecretSpec (length + encoding) and
// stores it; subsequent calls return the same stable bytes. A spec
// mismatch against a previously stored secret is a hard error; no
// silent rotation occurs.
//
// Request body:
//
//	{
//	  "length":    <int>,       // required, > 0
//	  "encoding":  "<string>",  // required: base64|hex|numeric|raw
//	  "tenant_id": "<string>"   // optional
//	}
//
// Response body is service.EnsureSecretResponse (JSON).
func (s *Server) HandleEnsureSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := chi.URLParam(r, "*")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}
	// Strip the trailing "/ensure" suffix routed to this handler.
	path = strings.TrimSuffix(path, "/ensure")
	if path == "" {
		RespondBadRequest(w, r, "secret path is required")
		return
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Length   int    `json:"length"`
		Encoding string `json:"encoding"`
		TenantID string `json:"tenant_id,omitempty"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Pre-validate the spec at the REST boundary so the caller gets a
	// clean 400 instead of a 500 for obviously-malformed input. The
	// service layer will re-validate as a defense-in-depth check.
	spec := &secrets.SecretSpec{Length: reqBody.Length, Encoding: reqBody.Encoding}
	if err := spec.Validate(); err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	req := &service.EnsureSecretRequest{
		SecretID: path,
		Length:   reqBody.Length,
		Encoding: reqBody.Encoding,
		TenantID: reqBody.TenantID,
	}

	secretsService := s.getSecretsService(w, r)
	if secretsService == nil {
		return
	}

	resp, err := secretsService.EnsureSecret(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
