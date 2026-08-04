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
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Bootstrap REST handlers — /api/v1/bootstrap surface.
//
// The bootstrap surface drives first-touch CA discovery for clients that have
// never spoken to the cluster before. It supports three trust-anchor methods
// (DANE/TLSA, Noise PSK, SPKI pin) plus an "auto" entry point that lets the
// daemon pick the strongest method that satisfies the request.
//
// Every handler delegates to the service-layer BootstrapServicer, which is a
// pure composition over the canonical go-xkms trust-bootstrap library — the
// REST layer never performs DANE/Noise/SPKI verification itself. The handlers
// follow the same readiness lifecycle as barrier/ceremony: the route is always
// mounted (callers get a 5xx, never a 404); a nil servicer yields 503; a wired
// servicer dispatches the fetch and serialises the resulting CA bundle.
// =============================================================================

// bootstrapServiceMethod is the shape every concrete BootstrapServicer mode
// dispatcher shares; the four handlers differ only in which method they bind.
type bootstrapServiceMethod func(ctx context.Context, req *service.BootstrapRequest) (*service.BootstrapBundleResponse, error)

// bootstrapMethodSelector picks one mode dispatcher off a (non-nil) servicer.
// It is evaluated only after the readiness guard so it never dereferences a
// nil bootstrap facade.
type bootstrapMethodSelector func(svc service.BootstrapServicer) bootstrapServiceMethod

// requireBootstrapService writes a 503 Service Unavailable response when the
// bootstrap facade is unwired. Returns true when the service is present so
// that callers can continue handling the request.
func (s *Server) requireBootstrapService(w http.ResponseWriter, r *http.Request) bool {
	if s.bootstrapSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrBootstrapServiceUnavailable)
		return false
	}
	return true
}

// dispatchBootstrap runs the readiness guard, decodes the optional request
// body, invokes the selected servicer method, and serialises the resulting CA
// bundle. A missing or empty body is valid: it yields a zero-value request,
// which the servicer rejects with a typed invalid-input error when the chosen
// mode needs trust material.
func (s *Server) dispatchBootstrap(w http.ResponseWriter, r *http.Request, pick bootstrapMethodSelector) {
	if !s.requireBootstrapService(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.BootstrapRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	resp, err := pick(s.bootstrapSvc)(r.Context(), &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBootstrapAuto handles POST /api/v1/bootstrap/auto.
//
// The "auto" mode delegates trust-anchor selection to the daemon: it tries
// every configured strategy (direct plus any supplied DANE/Noise/SPKI
// material) in priority order and returns the first verified bundle.
func (s *Server) HandleBootstrapAuto(w http.ResponseWriter, r *http.Request) {
	s.dispatchBootstrap(w, r, func(svc service.BootstrapServicer) bootstrapServiceMethod { return svc.Auto })
}

// HandleBootstrapDane handles POST /api/v1/bootstrap/dane.
//
// DANE/TLSA bootstrap verifies the server certificate against DNSSEC-validated
// TLSA records before fetching the CA bundle over HTTPS.
func (s *Server) HandleBootstrapDane(w http.ResponseWriter, r *http.Request) {
	s.dispatchBootstrap(w, r, func(svc service.BootstrapServicer) bootstrapServiceMethod { return svc.Dane })
}

// HandleBootstrapNoise handles POST /api/v1/bootstrap/noise.
//
// Noise bootstrap establishes trust via a Noise_NK handshake pinned by the
// server's static public key, fetching the CA bundle in-band.
func (s *Server) HandleBootstrapNoise(w http.ResponseWriter, r *http.Request) {
	s.dispatchBootstrap(w, r, func(svc service.BootstrapServicer) bootstrapServiceMethod { return svc.Noise })
}

// HandleBootstrapSpki handles POST /api/v1/bootstrap/spki.
//
// SPKI-pin bootstrap verifies the server certificate against a pre-shared
// SHA-256 hash of its SubjectPublicKeyInfo, fetching the CA bundle over HTTPS.
func (s *Server) HandleBootstrapSpki(w http.ResponseWriter, r *http.Request) {
	s.dispatchBootstrap(w, r, func(svc service.BootstrapServicer) bootstrapServiceMethod { return svc.Spki })
}
