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

// security_handler_attestation_verifier.go wires the attestation Verifier's
// verbs to quicbaasd's /api/v1/attestation/{endorsements,reference-values,
// appraise-quote} routes. The server's JSON bodies are the service-layer
// types, whose tags match the leaf client/attestation types.

package rest

import (
	"context"
	"net/http"
	"net/url"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func invalidVerifierREST(method string) error {
	return errors.NewInvalidInput("rest.security.Attestation."+method, "invalid request type")
}

// attestationPutEndorsement calls PUT /api/v1/attestation/endorsements/{name}.
func (h *securityRESTHandler) attestationPutEndorsement(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.PutEndorsementRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("PutEndorsement")
	}
	body := map[string]interface{}{"kind": r.Kind}
	if len(r.CertificatePEM) > 0 {
		body["certificate_pem"] = r.CertificatePEM
	}
	if len(r.PublicKeyPEM) > 0 {
		body["public_key_pem"] = r.PublicKeyPEM
	}
	var resp attestationclient.Endorsement
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, "/api/v1/attestation/endorsements/"+url.PathEscape(r.Name), body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationListEndorsements calls GET /api/v1/attestation/endorsements.
func (h *securityRESTHandler) attestationListEndorsements(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.ListEndorsementsRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("ListEndorsements")
	}
	path := "/api/v1/attestation/endorsements"
	if r.Kind != "" {
		path += "?kind=" + url.QueryEscape(r.Kind)
	}
	var resp attestationclient.ListEndorsementsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationDeleteEndorsement calls DELETE /api/v1/attestation/endorsements/{name}.
func (h *securityRESTHandler) attestationDeleteEndorsement(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("DeleteEndorsement")
	}
	return nil, h.transport.doJSONRequest(ctx, http.MethodDelete, "/api/v1/attestation/endorsements/"+url.PathEscape(r.Name), nil, nil)
}

// attestationPutReferenceValues calls PUT /api/v1/attestation/reference-values/{name}.
func (h *securityRESTHandler) attestationPutReferenceValues(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.PutReferenceValuesRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("PutReferenceValues")
	}
	var resp attestationclient.ReferenceValues
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, "/api/v1/attestation/reference-values/"+url.PathEscape(r.Name),
		map[string]interface{}{"signed_corim": r.SignedCoRIM}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationGetReferenceValues calls GET /api/v1/attestation/reference-values/{name}.
func (h *securityRESTHandler) attestationGetReferenceValues(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("GetReferenceValues")
	}
	var resp attestationclient.ReferenceValues
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/attestation/reference-values/"+url.PathEscape(r.Name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationListReferenceValues calls GET /api/v1/attestation/reference-values.
func (h *securityRESTHandler) attestationListReferenceValues(ctx context.Context, _ interface{}) (interface{}, error) {
	var resp attestationclient.ListReferenceValuesResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/attestation/reference-values", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// attestationDeleteReferenceValues calls DELETE /api/v1/attestation/reference-values/{name}.
func (h *securityRESTHandler) attestationDeleteReferenceValues(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.NameRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("DeleteReferenceValues")
	}
	return nil, h.transport.doJSONRequest(ctx, http.MethodDelete, "/api/v1/attestation/reference-values/"+url.PathEscape(r.Name), nil, nil)
}

// attestationAppraiseQuote calls POST /api/v1/attestation/appraise-quote.
func (h *securityRESTHandler) attestationAppraiseQuote(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*attestationclient.AppraiseQuoteRequest)
	if !ok || r == nil {
		return nil, invalidVerifierREST("AppraiseQuote")
	}
	var resp attestationclient.AttestationResult
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/attestation/appraise-quote", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
