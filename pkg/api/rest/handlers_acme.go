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
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrACMEServiceUnavailable indicates the ACME service is not configured on
// this server (no CA / qrdb datastore / REST base URL).
var ErrACMEServiceUnavailable = errors.New("acme service not available")

// =============================================================================
// ACME Route Registration
// =============================================================================
//
// QuicBaaS composes the go-xkms ACME (RFC 8555) server. Two route groups are
// mounted:
//
//  1. The standard RFC 8555 directory / nonce / account / order / finalize /
//     authz / challenge / cert / revoke / key-change endpoints under /acme/*.
//     These delegate verbatim to the xkms RestServicer — the single canonical
//     RFC 8555 implementation in the stack — so any conformant ACME client
//     (certbot, lego, the go-xkms ACME client) can drive issuance.
//
//  2. The QuicBaaS server-control and broker-as-CA client verbs under
//     /api/v1/acme/* (register-challenge, issue, new-order, finalize, renew)
//     that back Security().ACME(). These delegate to the service-layer
//     ACMEServicer. In the broker-as-CA model the client verbs are in-process
//     service calls, not JWS-signed exchanges.

// registerACMEDirectoryRoutes mounts the raw RFC 8555 endpoints at the router
// root (/acme/*). The handlers are resolved from the composed xkms
// RestServicer at request time so the routes are inert when ACME is not
// configured (they respond 503).
func registerACMEDirectoryRoutes(s *Server, root chi.Router) {
	root.Route("/acme", func(r chi.Router) {
		r.Get("/directory", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.DirectoryHandler }))
		r.Head("/directory", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.DirectoryHandler }))
		r.Get("/new-nonce", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.NewNonceHandler }))
		r.Head("/new-nonce", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.NewNonceHandler }))
		r.Post("/new-account", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.NewAccountHandler }))
		r.Post("/account/{id}", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.AccountHandler }))
		r.Post("/new-order", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.NewOrderHandler }))
		r.Post("/order/{id}", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.OrderHandler }))
		r.Post("/order/{id}/finalize", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.OrderFinalizeHandler }))
		r.Post("/orders/{id}", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.OrdersListHandler }))
		r.Post("/authz/{id}", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.AuthorizationHandler }))
		r.Post("/challenge/{id}", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.ChallengeHandler }))
		r.Post("/cert/{id}", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.CertificateHandler }))
		r.Post("/revoke-cert", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.RevokeCertHandler }))
		r.Post("/key-change", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.KeyChangeHandler }))
		r.Get("/ca-bundle", s.acmeRFCHandler(func(rs acmeRest) http.HandlerFunc { return rs.CABundleHandler }))
	})
}

// acmeRest is the subset of the xkms RestServicer the RFC 8555 routes use,
// expressed as bound http.HandlerFunc selectors.
type acmeRest = interface {
	AccountHandler(w http.ResponseWriter, r *http.Request)
	AuthorizationHandler(w http.ResponseWriter, r *http.Request)
	CertificateHandler(w http.ResponseWriter, r *http.Request)
	ChallengeHandler(w http.ResponseWriter, r *http.Request)
	DirectoryHandler(w http.ResponseWriter, r *http.Request)
	NewAccountHandler(w http.ResponseWriter, r *http.Request)
	NewNonceHandler(w http.ResponseWriter, r *http.Request)
	NewOrderHandler(w http.ResponseWriter, r *http.Request)
	OrderHandler(w http.ResponseWriter, r *http.Request)
	OrdersListHandler(w http.ResponseWriter, r *http.Request)
	OrderFinalizeHandler(w http.ResponseWriter, r *http.Request)
	RevokeCertHandler(w http.ResponseWriter, r *http.Request)
	KeyChangeHandler(w http.ResponseWriter, r *http.Request)
	CABundleHandler(w http.ResponseWriter, r *http.Request)
}

// acmeRFCHandler resolves the composed xkms RestServicer at request time and
// invokes the selected RFC 8555 handler. When ACME is not configured it
// responds 503 so the route is present but inert.
func (s *Server) acmeRFCHandler(sel func(acmeRest) http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acmeSvc := s.service.ACME()
		if acmeSvc == nil {
			RespondError(w, r, http.StatusServiceUnavailable, ErrACMEServiceUnavailable)
			return
		}
		rs := acmeSvc.RestServicer()
		if rs == nil {
			RespondError(w, r, http.StatusServiceUnavailable, ErrACMEServiceUnavailable)
			return
		}
		sel(rs)(w, r)
	}
}

// =============================================================================
// Control-verb handlers (Security().ACME().Server())
// =============================================================================

// acmeReadJSON resolves the ACME service, reads the request body, and decodes
// it into v. It writes the appropriate error response and returns false when
// the service is unavailable or the body is malformed.
func (s *Server) acmeReadJSON(w http.ResponseWriter, r *http.Request, v interface{}) (service.ACMEServicer, bool) {
	acmeSvc := s.service.ACME()
	if acmeSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrACMEServiceUnavailable)
		return nil, false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return nil, false
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, v); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return nil, false
	}
	return acmeSvc, true
}

// HandleACMERegisterChallenge handles POST /api/v1/acme/register-challenge —
// enables a challenge verifier on the composed ACME server.
func (s *Server) HandleACMERegisterChallenge(w http.ResponseWriter, r *http.Request) {
	var req acmeclient.RegisterChallengeRequest
	acmeSvc, ok := s.acmeReadJSON(w, r, &req)
	if !ok {
		return
	}

	svcResp, err := acmeSvc.RegisterChallenge(r.Context(), &service.ACMERegisterChallengeRequest{Name: req.Name})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &acmeclient.RegisterChallengeResponse{Registered: svcResp.Registered})
}

// HandleACMEIssue handles POST /api/v1/acme/issue — performs a server-side
// issuance from the supplied CSR using the named challenge.
func (s *Server) HandleACMEIssue(w http.ResponseWriter, r *http.Request) {
	var req acmeclient.IssueRequest
	acmeSvc, ok := s.acmeReadJSON(w, r, &req)
	if !ok {
		return
	}

	svcResp, err := acmeSvc.Issue(r.Context(), &service.ACMEIssueRequest{
		CSRPEM:         req.CSRPEM,
		ChallengeType:  req.ChallengeType,
		ChallengeValue: req.ChallengeValue,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &acmeclient.IssueResponse{
		CertificatePEM: svcResp.CertificatePEM,
		ChainPEM:       svcResp.ChainPEM,
		SerialNumber:   svcResp.SerialNumber,
	})
}

// HandleACMENewOrder handles POST /api/v1/acme/new-order — creates a
// lightweight in-process broker-as-CA order descriptor.
func (s *Server) HandleACMENewOrder(w http.ResponseWriter, r *http.Request) {
	var req acmeclient.NewOrderRequest
	acmeSvc, ok := s.acmeReadJSON(w, r, &req)
	if !ok {
		return
	}

	svcResp, err := acmeSvc.NewOrder(r.Context(), &service.ACMENewOrderRequest{
		Identifiers:   toServiceIdentifiers(req.Identifiers),
		ChallengeType: req.ChallengeType,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &acmeclient.NewOrderResponse{
		OrderID:     svcResp.OrderID,
		Status:      svcResp.Status,
		Identifiers: fromServiceIdentifiers(svcResp.Identifiers),
		Finalize:    svcResp.Finalize,
		URL:         svcResp.URL,
	})
}

// HandleACMEFinalizeOrder handles POST /api/v1/acme/finalize — signs the
// supplied CSR with the composed CA and returns the issued chain.
func (s *Server) HandleACMEFinalizeOrder(w http.ResponseWriter, r *http.Request) {
	var req acmeclient.FinalizeOrderRequest
	acmeSvc, ok := s.acmeReadJSON(w, r, &req)
	if !ok {
		return
	}

	svcResp, err := acmeSvc.FinalizeOrder(r.Context(), &service.ACMEFinalizeOrderRequest{
		OrderURL: req.OrderURL,
		CSRPEM:   req.CSRPEM,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &acmeclient.FinalizeOrderResponse{
		CertificatePEM: svcResp.CertificatePEM,
		ChainPEM:       svcResp.ChainPEM,
		SerialNumber:   svcResp.SerialNumber,
	})
}

// HandleACMERenew handles POST /api/v1/acme/renew — resolves (and optionally
// revokes) an existing certificate by serial. Renewal in the broker-as-CA
// model requires a fresh CSR via new-order/finalize; the service returns
// ErrACMERenewNeedsCSR which surfaces as a 400 with the resolved cert omitted.
func (s *Server) HandleACMERenew(w http.ResponseWriter, r *http.Request) {
	var req acmeclient.RenewRequest
	acmeSvc, ok := s.acmeReadJSON(w, r, &req)
	if !ok {
		return
	}

	svcResp, err := acmeSvc.Renew(r.Context(), &service.ACMERenewRequest{
		CertURL: req.CertURL,
		Revoke:  req.Revoke,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &acmeclient.RenewResponse{
		CertificatePEM: svcResp.CertificatePEM,
		SerialNumber:   svcResp.SerialNumber,
		Revoked:        svcResp.Revoked,
		Renewable:      svcResp.Renewable,
	})
}

// toServiceIdentifiers maps wire identifiers to service identifiers.
func toServiceIdentifiers(in []acmeclient.OrderIdentifier) []service.ACMEOrderIdentifier {
	out := make([]service.ACMEOrderIdentifier, len(in))
	for i, id := range in {
		out[i] = service.ACMEOrderIdentifier{Type: id.Type, Value: id.Value}
	}
	return out
}

// fromServiceIdentifiers maps service identifiers back to wire identifiers.
func fromServiceIdentifiers(in []service.ACMEOrderIdentifier) []acmeclient.OrderIdentifier {
	out := make([]acmeclient.OrderIdentifier, len(in))
	for i, id := range in {
		out[i] = acmeclient.OrderIdentifier{Type: id.Type, Value: id.Value}
	}
	return out
}
