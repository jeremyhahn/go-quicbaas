//go:build rest

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
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The attestation Verifier over REST (and QUIC):
//
//	PUT    /api/v1/attestation/endorsements/{name}        PutEndorsement
//	GET    /api/v1/attestation/endorsements[?kind=]       ListEndorsements
//	DELETE /api/v1/attestation/endorsements/{name}        DeleteEndorsement
//	PUT    /api/v1/attestation/reference-values/{name}    PutReferenceValues
//	GET    /api/v1/attestation/reference-values/{name}    GetReferenceValues
//	GET    /api/v1/attestation/reference-values           ListReferenceValues
//	DELETE /api/v1/attestation/reference-values/{name}    DeleteReferenceValues
//	POST   /api/v1/attestation/appraise-quote             AppraiseQuote
//
// Bodies are the service-layer types; byte fields travel base64-encoded, as
// encoding/json writes them.

// attestationEndorsementBody is the PUT endorsement body; the name is the
// path parameter.
type attestationEndorsementBody struct {
	Kind           service.EndorsementKind `json:"kind"`
	CertificatePEM []byte                  `json:"certificate_pem,omitempty"`
	PublicKeyPEM   []byte                  `json:"public_key_pem,omitempty"`
}

// attestationReferenceValuesBody is the PUT reference-values body.
type attestationReferenceValuesBody struct {
	SignedCoRIM []byte `json:"signed_corim"`
}

// attestationVerifierService returns the attestation service, answering 503
// itself when the server has none.
func (s *Server) attestationVerifierService(w http.ResponseWriter, r *http.Request) (service.AttestationService, bool) {
	svc := s.service.Attestation()
	if svc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrAttestationServiceUnavailable)
		return nil, false
	}
	return svc, true
}

// HandleAttestationPutEndorsement handles PUT /api/v1/attestation/endorsements/{name}.
func (s *Server) HandleAttestationPutEndorsement(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	var body attestationEndorsementBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	e, err := svc.PutEndorsement(r.Context(), &service.PutEndorsementRequest{
		Name:           chi.URLParam(r, "name"),
		Kind:           body.Kind,
		CertificatePEM: body.CertificatePEM,
		PublicKeyPEM:   body.PublicKeyPEM,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, e)
}

// HandleAttestationListEndorsements handles GET /api/v1/attestation/endorsements.
func (s *Server) HandleAttestationListEndorsements(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	resp, err := svc.ListEndorsements(r.Context(), &service.ListEndorsementsRequest{Kind: service.EndorsementKind(r.URL.Query().Get("kind"))})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleAttestationDeleteEndorsement handles DELETE /api/v1/attestation/endorsements/{name}.
func (s *Server) HandleAttestationDeleteEndorsement(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteEndorsement(r.Context(), &service.DeleteEndorsementRequest{Name: chi.URLParam(r, "name")}); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondNoContent(w, r)
}

// HandleAttestationPutReferenceValues handles PUT /api/v1/attestation/reference-values/{name}.
func (s *Server) HandleAttestationPutReferenceValues(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	var body attestationReferenceValuesBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	rv, err := svc.PutReferenceValues(r.Context(), &service.PutReferenceValuesRequest{Name: chi.URLParam(r, "name"), SignedCoRIM: body.SignedCoRIM})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, rv)
}

// HandleAttestationGetReferenceValues handles GET /api/v1/attestation/reference-values/{name}.
func (s *Server) HandleAttestationGetReferenceValues(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	rv, err := svc.GetReferenceValues(r.Context(), &service.GetReferenceValuesRequest{Name: chi.URLParam(r, "name")})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, rv)
}

// HandleAttestationListReferenceValues handles GET /api/v1/attestation/reference-values.
func (s *Server) HandleAttestationListReferenceValues(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	resp, err := svc.ListReferenceValues(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleAttestationDeleteReferenceValues handles DELETE /api/v1/attestation/reference-values/{name}.
func (s *Server) HandleAttestationDeleteReferenceValues(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteReferenceValues(r.Context(), &service.DeleteReferenceValuesRequest{Name: chi.URLParam(r, "name")}); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondNoContent(w, r)
}

// HandleAttestationAppraiseQuote handles POST /api/v1/attestation/appraise-quote.
func (s *Server) HandleAttestationAppraiseQuote(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.attestationVerifierService(w, r)
	if !ok {
		return
	}
	var req service.AppraiseQuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	result, err := svc.AppraiseQuote(r.Context(), &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, result)
}
